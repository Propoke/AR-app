package session

import (
	"errors"
	"net/http"

	"github.com/propoke/ar-app/backend/internal/auth"
	"github.com/propoke/ar-app/backend/internal/httputil"
)

// Handlers exposes session/token operations over HTTP.
type Handlers struct {
	svc *Service
}

// NewHandlers constructs session HTTP handlers.
func NewHandlers(svc *Service) *Handlers {
	return &Handlers{svc: svc}
}

// Mint issues a new connection token for the authenticated agent. The PIN is
// included in the response and must be relayed to the end-user out of band.
func (h *Handlers) Mint(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		httputil.WriteError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if p.Role == "viewer" {
		httputil.WriteError(w, http.StatusForbidden, "viewers cannot start sessions")
		return
	}
	tok, err := h.svc.Mint(r.Context(), p.OrgID, p.UserID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "could not mint token")
		return
	}
	// Include ICE servers + the agent's signaling join token so it can connect.
	httputil.WriteJSON(w, http.StatusCreated, map[string]any{
		"session_id":      tok.SessionID,
		"connect_id":      tok.ConnectID,
		"pin":             tok.PIN,
		"expires_at":      tok.ExpiresAt,
		"room":            tok.SessionID.String(),
		"signaling_token": tok.SignalingToken,
		"ice_servers":     h.svc.ICEServers(tok.SessionID),
	})
}

type redeemRequest struct {
	ConnectID string `json:"connect_id"`
	PIN       string `json:"pin"`
}

// Redeem validates a connect ID/PIN from the phone and returns join info. This
// endpoint is intentionally unauthenticated — the token itself is the credential.
func (h *Handlers) Redeem(w http.ResponseWriter, r *http.Request) {
	var req redeemRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ConnectID == "" || req.PIN == "" {
		httputil.WriteError(w, http.StatusBadRequest, "connect_id and pin are required")
		return
	}
	info, err := h.svc.Redeem(r.Context(), req.ConnectID, req.PIN)
	if err != nil {
		switch {
		case errors.Is(err, ErrTooManyAttempts):
			httputil.WriteError(w, http.StatusTooManyRequests, "too many attempts")
		case errors.Is(err, ErrInvalidToken):
			httputil.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		default:
			httputil.WriteError(w, http.StatusInternalServerError, "redeem failed")
		}
		return
	}
	httputil.WriteJSON(w, http.StatusOK, info)
}
