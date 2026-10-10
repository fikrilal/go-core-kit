package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"testing"
	"time"
)

// Vector 1: AWS's documented key-derivation example (Signature Version 4
// signing process). It pins the HMAC chain, including the raw signing key.
func TestDeriveSigningKeyMatchesAWSVector(t *testing.T) {
	at, err := time.Parse(shortDateFormat, "20120215")
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	key := deriveSigningKey("wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", at, "us-east-1", "iam")
	const want = "f4780e2d9f65fa895f9c67b32ce1baf0b0d8a43505a000a1a9e090d414db404d"
	if got := hex.EncodeToString(key); got != want {
		t.Fatalf("signing key = %s, want the documented %s", got, want)
	}
}

// Vector 2: AWS's documented presigned-URL canonical request. The canonical
// text is the spec; the test also recomputes the signature from that text with
// an inline HMAC chain, so assembly errors (joining, ordering, escaping) fail.
func TestCanonicalRequestMatchesDocumentedText(t *testing.T) {
	at, err := time.Parse(dateFormat, "20130524T000000Z")
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	const scope = "20130524/us-east-1/s3/aws4_request"
	query := sortedQuery(url.Values{
		"X-Amz-Algorithm":     {signatureAlgorithm},
		"X-Amz-Credential":    {"AKIAIOSFODNN7EXAMPLE/" + scope},
		"X-Amz-Date":          {at.Format(dateFormat)},
		"X-Amz-Expires":       {"86400"},
		"X-Amz-SignedHeaders": {"host"},
	})
	request := signRequest{ //nolint:gosec // AWS's published documentation example credentials
		method:        "GET",
		host:          "examplebucket.s3.amazonaws.com",
		path:          "/test.txt",
		query:         query,
		signedHeaders: []string{"host"},
		headerValues:  map[string]string{"host": "examplebucket.s3.amazonaws.com"},
		payloadHash:   unsignedPayload,
		at:            at,
		region:        "us-east-1",
		accessKeyID:   "AKIAIOSFODNN7EXAMPLE",
		secretKey:     "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
	}
	const wantCanonical = "GET\n/test.txt\n" +
		"X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request" +
		"&X-Amz-Date=20130524T000000Z&X-Amz-Expires=86400&X-Amz-SignedHeaders=host\n" +
		"host:examplebucket.s3.amazonaws.com\n\n" +
		"host\n" +
		"UNSIGNED-PAYLOAD"
	if got := canonicalRequest(request); got != wantCanonical {
		t.Fatalf("canonical request =\n%s\nwant the documented\n%s", got, wantCanonical)
	}

	canonicalHash := sha256.Sum256([]byte(wantCanonical))
	stringToSign := signatureAlgorithm + "\n" + at.Format(dateFormat) + "\n" + scope + "\n" + hex.EncodeToString(canonicalHash[:])
	wantSignature := hex.EncodeToString(hmacSHA256(deriveSigningKey(request.secretKey, at, "us-east-1", "s3"), stringToSign))
	signature, credential := sign(request)
	if signature != wantSignature {
		t.Fatalf("signature = %s, want the value assembled from the documented text %s", signature, wantSignature)
	}
	if credential != scope {
		t.Errorf("credential scope = %q, want %q", credential, scope)
	}
}
