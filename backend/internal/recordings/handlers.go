package recordings

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/propoke/ar-app/backend/internal/auth"
	"github.com/propoke/ar-app/backend/internal/httputil"
)

// Handlers exposes recording operations over HTTP.
type Handlers struct {
	svc *Service
}

// NewHandlers constructs recording HTTP handlers.
func NewHandlers(svc *Service) *Handlers {
	return &Handlers{svc: svc}
}

type createUploadRequest struct {
	ContentType string `json:"content_type"`
}

// CreateUpload issues a presigned upload URL for a session recording.
// POST /v1/sessions/{id}/recordings
func (h *Handlers) CreateUpload(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		httputil.WriteError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	sessionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	var req createUploadRequest
	_ = httputil.DecodeJSON(r, &req) // body is optional; content_type defaults

	up, err := h.svc.CreateUpload(r.Context(), p.OrgID, sessionID, req.ContentType)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, up)
}

type completeRequest struct {
	SizeBytes int64 `json:"size_bytes"`
}

// Complete finalizes a recording after upload.
// POST /v1/recordings/{id}/complete
func (h *Handlers) Complete(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		httputil.WriteError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	recordingID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid recording id")
		return
	}
	var req completeRequest
	_ = httputil.DecodeJSON(r, &req)

	if err := h.svc.Complete(r.Context(), p.OrgID, recordingID, req.SizeBytes); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "available"})
}

// List returns the recordings for a session.
// GET /v1/sessions/{id}/recordings
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		httputil.WriteError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	sessionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	list, err := h.svc.List(r.Context(), p.OrgID, sessionID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "could not list recordings")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"recordings": list})
}
