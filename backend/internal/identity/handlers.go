package identity

import (
	"errors"
	"net/http"
	"time"

	"github.com/propoke/ar-app/backend/internal/auth"
	"github.com/propoke/ar-app/backend/internal/httputil"
)

// Handlers exposes identity operations over HTTP.
type Handlers struct {
	svc    *Service
	issuer *auth.Issuer
}

// NewHandlers constructs identity HTTP handlers.
func NewHandlers(svc *Service, issuer *auth.Issuer) *Handlers {
	return &Handlers{svc: svc, issuer: issuer}
}

type registerRequest struct {
	OrgName     string `json:"org_name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type tokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	User         User      `json:"user"`
}

// Register creates a new organization and its first admin user.
func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateCredentials(req.Email, req.Password); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.OrgName == "" {
		httputil.WriteError(w, http.StatusBadRequest, "org_name is required")
		return
	}

	u, err := h.svc.RegisterOrg(r.Context(), req.OrgName, req.Email, req.Password, req.DisplayName)
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			httputil.WriteError(w, http.StatusConflict, err.Error())
			return
		}
		httputil.WriteError(w, http.StatusInternalServerError, "could not register")
		return
	}
	h.respondWithTokens(w, http.StatusCreated, u)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login authenticates a user and returns a fresh token pair.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := h.svc.Authenticate(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			httputil.WriteError(w, http.StatusUnauthorized, "invalid email or password")
		case errors.Is(err, ErrDisabled):
			httputil.WriteError(w, http.StatusForbidden, "account disabled")
		default:
			httputil.WriteError(w, http.StatusInternalServerError, "login failed")
		}
		return
	}
	h.respondWithTokens(w, http.StatusOK, u)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh exchanges a valid refresh token for a new access token pair.
func (h *Handlers) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	claims, err := h.issuer.Parse(req.RefreshToken)
	if err != nil || claims.Kind != auth.KindRefresh {
		httputil.WriteError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	// Re-load the user so role changes and disablement take effect on refresh.
	orgID, err1 := parseUUID(claims.OrgID)
	userID, err2 := parseUUID(claims.Subject)
	if err1 != nil || err2 != nil {
		httputil.WriteError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	u, err := h.svc.GetUser(r.Context(), orgID, userID)
	if err != nil {
		httputil.WriteError(w, http.StatusUnauthorized, "user no longer exists")
		return
	}
	h.respondWithTokens(w, http.StatusOK, u)
}

// Me returns the authenticated user's profile.
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		httputil.WriteError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	u, err := h.svc.GetUser(r.Context(), p.OrgID, p.UserID)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "user not found")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, u)
}

func (h *Handlers) respondWithTokens(w http.ResponseWriter, status int, u User) {
	access, exp, err := h.issuer.Issue(auth.KindAccess, u.ID, u.OrgID, u.Role)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	refresh, _, err := h.issuer.Issue(auth.KindRefresh, u.ID, u.OrgID, u.Role)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	httputil.WriteJSON(w, status, tokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    exp,
		User:         u,
	})
}
