package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type ctxKey int

const principalKey ctxKey = iota

// Principal is the authenticated caller attached to a request context.
type Principal struct {
	UserID uuid.UUID
	OrgID  uuid.UUID
	Role   string
}

// Middleware authenticates requests using a Bearer access token. On success it
// stores a Principal in the request context; otherwise it responds 401.
func (i *Issuer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		if raw == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		claims, err := i.Parse(raw)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		if claims.Kind != KindAccess {
			http.Error(w, "wrong token kind", http.StatusUnauthorized)
			return
		}
		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			http.Error(w, "invalid subject", http.StatusUnauthorized)
			return
		}
		orgID, err := uuid.Parse(claims.OrgID)
		if err != nil {
			http.Error(w, "invalid org", http.StatusUnauthorized)
			return
		}
		p := Principal{UserID: userID, OrgID: orgID, Role: claims.Role}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
	})
}

// FromContext returns the authenticated Principal, if any.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}
