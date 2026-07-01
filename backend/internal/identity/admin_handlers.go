package identity

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/propoke/ar-app/backend/internal/auth"
	"github.com/propoke/ar-app/backend/internal/httputil"
)

// requireAdmin returns the caller's principal if they are an org admin, else it
// writes an error response and returns ok=false.
func requireAdmin(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		httputil.WriteError(w, http.StatusUnauthorized, "unauthenticated")
		return auth.Principal{}, false
	}
	if p.Role != "admin" {
		httputil.WriteError(w, http.StatusForbidden, "admin role required")
		return auth.Principal{}, false
	}
	return p, true
}

type createUserRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

// CreateUser adds a user to the admin's organization. POST /v1/users
func (h *Handlers) CreateUser(w http.ResponseWriter, r *http.Request) {
	p, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	var req createUserRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateCredentials(req.Email, req.Password); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Role != "" && req.Role != "admin" && req.Role != "agent" && req.Role != "viewer" {
		httputil.WriteError(w, http.StatusBadRequest, "role must be admin, agent, or viewer")
		return
	}

	u, err := h.svc.CreateUser(r.Context(), p.OrgID, req.Email, req.Password, req.DisplayName, req.Role)
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			httputil.WriteError(w, http.StatusConflict, err.Error())
			return
		}
		httputil.WriteError(w, http.StatusInternalServerError, "could not create user")
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, u)
}

// ListUsers returns the users in the admin's organization. GET /v1/users
func (h *Handlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	p, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	users, err := h.svc.ListUsers(r.Context(), p.OrgID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "could not list users")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"users": users})
}

// SetUserDisabled enables/disables a user. POST /v1/users/{id}/disable|enable
func (h *Handlers) SetUserDisabled(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := requireAdmin(w, r)
		if !ok {
			return
		}
		userID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "invalid user id")
			return
		}
		if userID == p.UserID && disabled {
			httputil.WriteError(w, http.StatusBadRequest, "cannot disable your own account")
			return
		}
		if err := h.svc.SetDisabled(r.Context(), p.OrgID, userID, disabled); err != nil {
			httputil.WriteError(w, http.StatusNotFound, err.Error())
			return
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]bool{"disabled": disabled})
	}
}
