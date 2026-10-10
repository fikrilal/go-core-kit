// Package storage signs S3-compatible object-storage requests: presigned
// PUT/GET URLs for direct client upload/read, and signed HEAD/DELETE for the
// server. SigV4 is implemented here with the standard library only.
package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	signatureAlgorithm = "AWS4-HMAC-SHA256"
	signingService     = "s3"
	unsignedPayload    = "UNSIGNED-PAYLOAD"
	emptyPayloadHash   = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	dateFormat         = "20060102T150405Z"
	shortDateFormat    = "20060102"
)

// signRequest carries the pieces of a SigV4 signature.
type signRequest struct {
	method        string
	host          string
	path          string
	query         url.Values
	signedHeaders []string
	headerValues  map[string]string
	payloadHash   string
	at            time.Time
	region        string
	accessKeyID   string
	secretKey     string
}

// sign computes the SigV4 signature and the credential scope for a request.
func sign(req signRequest) (signature, credential string) {
	scope := credentialScope(req.at, req.region)
	canonical := canonicalRequest(req)
	stringToSign := strings.Join([]string{
		signatureAlgorithm,
		req.at.UTC().Format(dateFormat),
		scope,
		hashHex(canonical),
	}, "\n")
	signingKey := deriveSigningKey(req.secretKey, req.at, req.region, signingService)
	return hex.EncodeToString(hmacSHA256(signingKey, stringToSign)), scope
}

// credentialScope renders "<date>/<region>/s3/aws4_request".
func credentialScope(at time.Time, region string) string {
	return strings.Join([]string{
		at.UTC().Format(shortDateFormat),
		region,
		signingService,
		"aws4_request",
	}, "/")
}

// canonicalRequest renders the SigV4 canonical request.
func canonicalRequest(req signRequest) string {
	headers := make([]string, 0, len(req.signedHeaders))
	for _, name := range req.signedHeaders {
		headers = append(headers, name+":"+req.headerValues[name])
	}
	return strings.Join([]string{
		req.method,
		req.path,
		req.query.Encode(),
		strings.Join(headers, "\n") + "\n",
		strings.Join(req.signedHeaders, ";"),
		req.payloadHash,
	}, "\n")
}

// deriveSigningKey runs the SigV4 key derivation chain for a service.
func deriveSigningKey(secret string, at time.Time, region, service string) []byte {
	date := hmacSHA256([]byte("AWS4"+secret), at.UTC().Format(shortDateFormat))
	regionKey := hmacSHA256(date, region)
	serviceKey := hmacSHA256(regionKey, service)
	return hmacSHA256(serviceKey, "aws4_request")
}

func hmacSHA256(key []byte, message string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}

func hashHex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// escapePath escapes every object-key segment but keeps the "/" separators.
func escapePath(key string) string {
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return "/" + strings.Join(segments, "/")
}

// sortedQuery renders the query parameters in SigV4 order.
func sortedQuery(values url.Values) url.Values {
	sorted := url.Values{}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range values[key] {
			sorted.Add(key, value)
		}
	}
	return sorted
}

// validateTTL mirrors the reference object-storage policy: 1..900 seconds.
func validateTTL(ttl time.Duration) error {
	seconds := int64(ttl / time.Second)
	if ttl%time.Second != 0 || seconds < 1 || seconds > 900 {
		return fmt.Errorf("storage: presigned URL ttl must be 1-900 seconds, got %s", ttl)
	}
	return nil
}
