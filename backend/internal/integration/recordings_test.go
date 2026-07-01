//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/propoke/ar-app/backend/internal/auth"
	"github.com/propoke/ar-app/backend/internal/recordings"
)

// fakePresigner returns a dummy URL without touching real object storage, so
// these tests exercise the recordings HTTP/DB path without needing S3/MinIO.
type fakePresigner struct{}

func (fakePresigner) PresignPut(_ context.Context, objectKey, _ string, _ time.Duration) (string, error) {
	return "https://example-bucket.test/" + objectKey, nil
}

// newRecordingsServer builds a minimal server exposing just the recordings
// routes behind real JWT auth, backed by the real Postgres store from
// newTestStoreAndSession. It mints a real 'active' session to attach
// recordings to.
func newRecordingsServer(t *testing.T) (srv *httptest.Server, issuer *auth.Issuer, orgID uuid.UUID, sessionID uuid.UUID) {
	t.Helper()
	st, sessSvc := newTestStoreAndSession(t)
	ctx := context.Background()
	orgID, agentID := insertOrgAndAgent(t, st)

	tok, err := sessSvc.Mint(ctx, orgID, agentID)
	if err != nil {
		t.Fatalf("mint session: %v", err)
	}
	if _, err := sessSvc.Redeem(ctx, tok.ConnectID, tok.PIN); err != nil {
		t.Fatalf("redeem session: %v", err)
	}

	issuer = auth.NewIssuer([]byte("itest-secret"), 15*time.Minute, time.Hour, time.Hour)
	recSvc := recordings.NewService(st.DB, fakePresigner{}, time.Hour)
	h := recordings.NewHandlers(recSvc)

	mux := http.NewServeMux()
	authed := func(hf http.HandlerFunc) http.Handler { return issuer.Middleware(http.HandlerFunc(hf)) }
	mux.Handle("POST /v1/sessions/{id}/recordings", authed(h.CreateUpload))
	mux.Handle("GET /v1/sessions/{id}/recordings", authed(h.List))
	mux.Handle("POST /v1/recordings/{id}/complete", authed(h.Complete))

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, issuer, orgID, tok.SessionID
}

func mintToken(t *testing.T, issuer *auth.Issuer, orgID uuid.UUID, role string) string {
	t.Helper()
	tok, _, err := issuer.Issue(auth.KindAccess, uuid.New(), orgID, role, 0)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

// TestRecordingsRejectsViewerWrites is the regression test for the RBAC gap:
// a viewer (read-only by design — see identity's role comments) must not be
// able to create or finalize recordings, only list them.
func TestRecordingsRejectsViewerWrites(t *testing.T) {
	srv, issuer, orgID, sessionID := newRecordingsServer(t)
	viewerToken := mintToken(t, issuer, orgID, "viewer")
	agentToken := mintToken(t, issuer, orgID, "agent")

	resp, _ := postJSON(t, srv.URL+"/v1/sessions/"+sessionID.String()+"/recordings", viewerToken, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer create upload: want 403, got %d", resp.StatusCode)
	}

	resp, _ = postJSON(t, srv.URL+"/v1/recordings/"+uuid.NewString()+"/complete", viewerToken, map[string]int64{"size_bytes": 100})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer complete: want 403, got %d", resp.StatusCode)
	}

	// A viewer CAN still list (read-only access to history).
	resp, _ = getJSON(t, srv.URL+"/v1/sessions/"+sessionID.String()+"/recordings", viewerToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer list: want 200, got %d", resp.StatusCode)
	}

	// An agent CAN create and complete.
	resp, created := postJSON(t, srv.URL+"/v1/sessions/"+sessionID.String()+"/recordings", agentToken, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("agent create upload: want 201, got %d (body=%v)", resp.StatusCode, created)
	}
}

// TestRecordingsCompleteRejectsInvalidSize covers the size_bytes bounds check:
// non-positive or absurdly large client-reported sizes must be rejected
// rather than written to the database unchecked.
func TestRecordingsCompleteRejectsInvalidSize(t *testing.T) {
	srv, issuer, orgID, sessionID := newRecordingsServer(t)
	agentToken := mintToken(t, issuer, orgID, "agent")

	resp, created := postJSON(t, srv.URL+"/v1/sessions/"+sessionID.String()+"/recordings", agentToken, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create upload: want 201, got %d", resp.StatusCode)
	}
	recordingID, _ := created["recording_id"].(string)
	if recordingID == "" {
		t.Fatalf("no recording_id in response: %v", created)
	}

	for _, size := range []int64{0, -1, recordings.MaxRecordingSizeBytes + 1} {
		resp, body := postJSON(t, srv.URL+"/v1/recordings/"+recordingID+"/complete", agentToken,
			map[string]int64{"size_bytes": size})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("size %d: want 400, got %d (body=%v)", size, resp.StatusCode, body)
		}
	}

	// A valid size succeeds.
	resp, _ = postJSON(t, srv.URL+"/v1/recordings/"+recordingID+"/complete", agentToken,
		map[string]int64{"size_bytes": 12345})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid size: want 200, got %d", resp.StatusCode)
	}
}
