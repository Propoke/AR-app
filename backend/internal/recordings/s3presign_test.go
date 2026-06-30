package recordings

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func fixedPresigner(t *testing.T) *S3Presigner {
	t.Helper()
	p, err := NewS3Presigner("minio.example.com:9000", "AKID", "SECRET", "recordings", "us-east-1", true)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	// Pin time so signatures are deterministic.
	p.now = func() time.Time { return time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC) }
	return p
}

func TestPresignPutStructure(t *testing.T) {
	p := fixedPresigner(t)
	raw, err := p.PresignPut(context.Background(), "recordings/sess/rec.webm", "video/webm", 15*time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("result is not a valid URL: %v", err)
	}
	if u.Scheme != "https" || u.Host != "minio.example.com:9000" {
		t.Fatalf("unexpected scheme/host: %s://%s", u.Scheme, u.Host)
	}
	if u.Path != "/recordings/recordings/sess/rec.webm" {
		t.Fatalf("unexpected path-style key: %s", u.Path)
	}

	q := u.Query()
	if q.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" {
		t.Fatalf("missing algorithm")
	}
	if !strings.HasPrefix(q.Get("X-Amz-Credential"), "AKID/20260630/us-east-1/s3/aws4_request") {
		t.Fatalf("bad credential scope: %s", q.Get("X-Amz-Credential"))
	}
	if q.Get("X-Amz-Expires") != "900" {
		t.Fatalf("expected 900s expiry, got %s", q.Get("X-Amz-Expires"))
	}
	if len(q.Get("X-Amz-Signature")) != 64 {
		t.Fatalf("signature should be 64 hex chars, got %q", q.Get("X-Amz-Signature"))
	}
}

func TestPresignPutIsDeterministic(t *testing.T) {
	a, _ := fixedPresigner(t).PresignPut(context.Background(), "k/o.webm", "video/webm", time.Minute)
	b, _ := fixedPresigner(t).PresignPut(context.Background(), "k/o.webm", "video/webm", time.Minute)
	if a != b {
		t.Fatal("same inputs + time must yield identical presigned URLs")
	}
}

func TestPresignPutSignatureIsSensitive(t *testing.T) {
	sig := func(p *S3Presigner, key string) string {
		raw, _ := p.PresignPut(context.Background(), key, "video/webm", time.Minute)
		u, _ := url.Parse(raw)
		return u.Query().Get("X-Amz-Signature")
	}

	base := fixedPresigner(t)
	other, _ := NewS3Presigner("minio.example.com:9000", "AKID", "DIFFERENT", "recordings", "us-east-1", true)
	other.now = base.now

	if sig(base, "a.webm") == sig(base, "b.webm") {
		t.Fatal("different object keys must produce different signatures")
	}
	if sig(base, "a.webm") == sig(other, "a.webm") {
		t.Fatal("different secret keys must produce different signatures")
	}
}

func TestNewS3PresignerValidates(t *testing.T) {
	if _, err := NewS3Presigner("", "k", "s", "b", "", true); err == nil {
		t.Fatal("expected error when endpoint is empty")
	}
}
