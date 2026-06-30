package identity

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

// validateCredentials applies minimal format rules to an email/password pair.
func validateCredentials(email, password string) error {
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return errors.New("a valid email is required")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	return nil
}

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
