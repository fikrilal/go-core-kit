package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/clock"
)

// Options configure the S3-compatible endpoint.
type Options struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
}

// PresignedPut is the upload plan a client executes directly.
type PresignedPut struct {
	Method  string
	URL     string
	Headers map[string]string
}

// PresignedGet is a short-lived read URL.
type PresignedGet struct {
	URL string
}

// HeadResult is the object metadata a HEAD returns; a nil field means the
// header was absent.
type HeadResult struct {
	Exists        bool
	ContentType   *string
	ContentLength *int64
}

// Client is the object-storage client. A client without the required options
// is disabled, mirroring the reference's configured check.
type Client struct {
	options Options
	clock   clock.Clock
	http    *http.Client
}

// New builds the client. Endpoint, region, bucket, and credentials must all be
// set for the client to be enabled.
func New(options Options, clk clock.Clock) *Client {
	return &Client{options: options, clock: clk, http: &http.Client{Timeout: 30 * time.Second}}
}

// Enabled reports whether every required option is set.
func (c *Client) Enabled() bool {
	return strings.TrimSpace(c.options.Endpoint) != "" &&
		strings.TrimSpace(c.options.Region) != "" &&
		strings.TrimSpace(c.options.Bucket) != "" &&
		strings.TrimSpace(c.options.AccessKeyID) != "" &&
		strings.TrimSpace(c.options.SecretAccessKey) != ""
}

// Bucket returns the configured bucket name.
func (c *Client) Bucket() string { return c.options.Bucket }

// PresignPut signs a direct PUT for a key; the client must send the returned
// Content-Type header.
func (c *Client) PresignPut(key, contentType string, ttl time.Duration) (PresignedPut, error) {
	if err := validateTTL(ttl); err != nil {
		return PresignedPut{}, err
	}
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return PresignedPut{}, fmt.Errorf("storage: contentType is required")
	}
	signed, err := c.presign(key, http.MethodPut, ttl, map[string]string{"content-type": contentType})
	if err != nil {
		return PresignedPut{}, err
	}
	return PresignedPut{
		Method:  http.MethodPut,
		URL:     signed,
		Headers: map[string]string{"Content-Type": contentType},
	}, nil
}

// PresignGet signs a short-lived GET for a key.
func (c *Client) PresignGet(key string, ttl time.Duration) (PresignedGet, error) {
	if err := validateTTL(ttl); err != nil {
		return PresignedGet{}, err
	}
	signed, err := c.presign(key, http.MethodGet, ttl, nil)
	if err != nil {
		return PresignedGet{}, err
	}
	return PresignedGet{URL: signed}, nil
}

// Head inspects an object; a missing object reports Exists false.
func (c *Client) Head(ctx context.Context, key string) (HeadResult, error) {
	response, err := c.do(ctx, http.MethodHead, key)
	if err != nil {
		return HeadResult{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return HeadResult{}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return HeadResult{}, fmt.Errorf("storage: head %s: unexpected status %d", key, response.StatusCode)
	}
	result := HeadResult{Exists: true}
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		result.ContentType = &contentType
	}
	if length := response.Header.Get("Content-Length"); length != "" {
		parsed, err := strconv.ParseInt(length, 10, 64)
		if err != nil {
			return HeadResult{}, fmt.Errorf("storage: head %s: parse content-length: %w", key, err)
		}
		result.ContentLength = &parsed
	}
	return result, nil
}

// Delete removes an object; a missing object is not an error.
func (c *Client) Delete(ctx context.Context, key string) error {
	response, err := c.do(ctx, http.MethodDelete, key)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound || (response.StatusCode >= 200 && response.StatusCode < 300) {
		return nil
	}
	return fmt.Errorf("storage: delete %s: unexpected status %d", key, response.StatusCode)
}

// presign builds the query-signed URL for a request.
func (c *Client) presign(key, method string, ttl time.Duration, extraHeaders map[string]string) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("storage: object storage is not configured")
	}
	target, err := c.objectURL(key)
	if err != nil {
		return "", err
	}
	at := c.clock.Now().UTC()
	scope := credentialScope(at, c.options.Region)
	query := url.Values{
		"X-Amz-Algorithm":  {signatureAlgorithm},
		"X-Amz-Credential": {c.options.AccessKeyID + "/" + scope},
		"X-Amz-Date":       {at.Format(dateFormat)},
		"X-Amz-Expires":    {strconv.FormatInt(int64(ttl/time.Second), 10)},
	}
	headers := map[string]string{"host": target.Host}
	signed := []string{"host"}
	for name, value := range extraHeaders {
		headers[name] = strings.TrimSpace(value)
		signed = append(signed, name)
	}
	sort.Strings(signed)
	query.Set("X-Amz-SignedHeaders", strings.Join(signed, ";"))
	signature, _ := sign(signRequest{
		method:        method,
		host:          target.Host,
		path:          target.EscapedPath(),
		query:         sortedQuery(query),
		signedHeaders: signed,
		headerValues:  headers,
		payloadHash:   unsignedPayload,
		at:            at,
		region:        c.options.Region,
		accessKeyID:   c.options.AccessKeyID,
		secretKey:     c.options.SecretAccessKey,
	})
	query.Set("X-Amz-Signature", signature)
	target.RawQuery = query.Encode()
	return target.String(), nil
}

// do sends a header-signed request.
func (c *Client) do(ctx context.Context, method, key string) (*http.Response, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("storage: object storage is not configured")
	}
	target, err := c.objectURL(key)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("storage: build request: %w", err)
	}
	at := c.clock.Now().UTC()
	scope := credentialScope(at, c.options.Region)
	headers := map[string]string{
		"host":                 target.Host,
		"x-amz-content-sha256": emptyPayloadHash,
		"x-amz-date":           at.Format(dateFormat),
	}
	signed := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	sort.Strings(signed)
	signature, _ := sign(signRequest{
		method:        method,
		host:          target.Host,
		path:          target.EscapedPath(),
		query:         url.Values{},
		signedHeaders: signed,
		headerValues:  headers,
		payloadHash:   emptyPayloadHash,
		at:            at,
		region:        c.options.Region,
		accessKeyID:   c.options.AccessKeyID,
		secretKey:     c.options.SecretAccessKey,
	})
	request.Header.Set("x-amz-date", headers["x-amz-date"])
	request.Header.Set("x-amz-content-sha256", emptyPayloadHash)
	request.Header.Set("Authorization", fmt.Sprintf(
		"%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		signatureAlgorithm, c.options.AccessKeyID, scope, strings.Join(signed, ";"), signature,
	))
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("storage: %s %s: %w", method, key, err)
	}
	return response, nil
}

// objectURL builds the endpoint URL for a key, path style or virtual hosted.
func (c *Client) objectURL(key string) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(c.options.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("storage: parse endpoint: %w", err)
	}
	if endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, fmt.Errorf("storage: endpoint %q needs a scheme and host", c.options.Endpoint)
	}
	escaped := escapePath(key)
	if c.options.ForcePathStyle {
		endpoint.Path = "/" + c.options.Bucket + "/" + key
		endpoint.RawPath = "/" + c.options.Bucket + escaped
	} else {
		endpoint.Host = c.options.Bucket + "." + endpoint.Host
		endpoint.Path = "/" + key
		endpoint.RawPath = escaped
	}
	endpoint.RawQuery = ""
	return endpoint, nil
}
