package recordings

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// S3Presigner generates AWS Signature V4 presigned PUT URLs for an S3-compatible
// endpoint (AWS S3, MinIO, etc.). It uses path-style addressing and signs with an
// UNSIGNED-PAYLOAD, so no request body hashing or network call is needed — the URL
// is pure HMAC, which keeps the dependency surface to the standard library and
// makes the output deterministic and unit-testable.
type S3Presigner struct {
	endpoint  string // host[:port], no scheme
	region    string
	accessKey string
	secretKey string
	bucket    string
	secure    bool
	now       func() time.Time // injectable for tests
}

// NewS3Presigner constructs a presigner. region defaults to "us-east-1" (MinIO's default).
func NewS3Presigner(endpoint, accessKey, secretKey, bucket, region string, secure bool) (*S3Presigner, error) {
	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		return nil, fmt.Errorf("s3 presigner requires endpoint, credentials, and bucket")
	}
	if region == "" {
		region = "us-east-1"
	}
	return &S3Presigner{
		endpoint:  endpoint,
		region:    region,
		accessKey: accessKey,
		secretKey: secretKey,
		bucket:    bucket,
		secure:    secure,
		now:       time.Now,
	}, nil
}

// PresignPut returns a presigned URL the client can PUT the object bytes to. The
// content type is recorded in metadata but not bound into the signature; the
// client sends it as the request Content-Type header.
func (p *S3Presigner) PresignPut(_ context.Context, objectKey, _ string, expiry time.Duration) (string, error) {
	now := p.now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	scope := strings.Join([]string{dateStamp, p.region, "s3", "aws4_request"}, "/")

	scheme := "https"
	if !p.secure {
		scheme = "http"
	}

	// Path-style canonical URI: /bucket/key with each segment URL-encoded.
	canonicalURI := "/" + p.bucket + "/" + encodePath(objectKey)

	query := url.Values{}
	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", p.accessKey+"/"+scope)
	query.Set("X-Amz-Date", amzDate)
	query.Set("X-Amz-Expires", fmt.Sprintf("%d", int(expiry.Seconds())))
	query.Set("X-Amz-SignedHeaders", "host")

	canonicalQuery := query.Encode() // url.Values.Encode sorts by key

	canonicalHeaders := "host:" + p.endpoint + "\n"
	canonicalRequest := strings.Join([]string{
		"PUT",
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hexSHA256([]byte(canonicalRequest)),
	}, "\n")

	signingKey := p.signingKey(dateStamp)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	return fmt.Sprintf("%s://%s%s?%s&X-Amz-Signature=%s",
		scheme, p.endpoint, canonicalURI, canonicalQuery, signature), nil
}

func (p *S3Presigner) signingKey(dateStamp string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+p.secretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(p.region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// encodePath URL-encodes each path segment per AWS rules (slashes preserved).
func encodePath(p string) string {
	segments := strings.Split(p, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}
