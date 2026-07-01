// Package identity handles user registration, authentication, and organization
// membership.
package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/propoke/ar-app/backend/internal/crypto"
)

var (
	// ErrEmailTaken is returned when an email already exists in the organization.
	ErrEmailTaken = errors.New("email already registered")
	// ErrInvalidCredentials is returned for a bad email/password combination.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrDisabled is returned when a user account has been disabled.
	ErrDisabled = errors.New("account disabled")
)

// User is a backend representation of an application user.
type User struct {
	ID          uuid.UUID `json:"id"`
	OrgID       uuid.UUID `json:"org_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
	// TokenVersion is used for refresh-token revocation; not serialized to clients.
	TokenVersion int `json:"-"`
}

// Service provides identity operations backed by Postgres.
type Service struct {
	db *pgxpool.Pool
}

// NewService constructs an identity Service.
func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// RegisterOrg creates a brand-new organization together with its first admin user.
// This is the self-service signup path used to bootstrap a tenant.
func (s *Service) RegisterOrg(ctx context.Context, orgName, email, password, displayName string) (User, error) {
	email = normalizeEmail(email)
	hash, err := crypto.HashPassword(password)
	if err != nil {
		return User{}, err
	}

	var u User
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var orgID uuid.UUID
		if err := tx.QueryRow(ctx,
			`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, orgName,
		).Scan(&orgID); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`INSERT INTO users (org_id, email, password_hash, display_name, role)
			 VALUES ($1, $2, $3, $4, 'admin')
			 RETURNING id, org_id, email, display_name, role, created_at, token_version`,
			orgID, email, hash, displayName,
		).Scan(&u.ID, &u.OrgID, &u.Email, &u.DisplayName, &u.Role, &u.CreatedAt, &u.TokenVersion)
	})
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrEmailTaken
		}
		return User{}, err
	}
	return u, nil
}

// CreateUser adds a user to an existing organization (admin-invited path).
func (s *Service) CreateUser(ctx context.Context, orgID uuid.UUID, email, password, displayName, role string) (User, error) {
	email = normalizeEmail(email)
	if role == "" {
		role = "agent"
	}
	hash, err := crypto.HashPassword(password)
	if err != nil {
		return User{}, err
	}
	var u User
	err = s.db.QueryRow(ctx,
		`INSERT INTO users (org_id, email, password_hash, display_name, role)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, org_id, email, display_name, role, created_at`,
		orgID, email, hash, displayName, role,
	).Scan(&u.ID, &u.OrgID, &u.Email, &u.DisplayName, &u.Role, &u.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrEmailTaken
		}
		return User{}, err
	}
	return u, nil
}

// Authenticate verifies an email/password pair and returns the user on success.
// It always performs a password hash comparison to avoid leaking, via timing,
// whether the email exists.
//
// Login takes only an email (no org selector), so email must be globally
// unique across organizations — enforced by the users_email_unique_ci index
// (migration 0004). Without that constraint, the same email could exist in two
// orgs and this query would always resolve to whichever was created first,
// silently locking the second account out of login forever. The ORDER BY /
// LIMIT here is defense-in-depth only (e.g. a database that predates the
// index): it guarantees this query never errors on multiple rows, but it does
// not fix the underlying collision — the index is what prevents it from
// occurring at all.
func (s *Service) Authenticate(ctx context.Context, email, password string) (User, error) {
	email = normalizeEmail(email)
	var (
		u        User
		hash     string
		disabled bool
	)
	err := s.db.QueryRow(ctx,
		`SELECT id, org_id, email, password_hash, display_name, role, disabled, created_at, token_version
		 FROM users WHERE lower(email) = $1
		 ORDER BY created_at LIMIT 1`,
		email,
	).Scan(&u.ID, &u.OrgID, &u.Email, &hash, &u.DisplayName, &u.Role, &disabled, &u.CreatedAt, &u.TokenVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		// Compare against a dummy hash to equalize timing, then fail.
		_ = crypto.VerifyPassword(password, dummyHash)
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if err := crypto.VerifyPassword(password, hash); err != nil {
		return User{}, ErrInvalidCredentials
	}
	if disabled {
		return User{}, ErrDisabled
	}
	return u, nil
}

// ListUsers returns all users in an organization, newest first.
func (s *Service) ListUsers(ctx context.Context, orgID uuid.UUID) ([]User, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, org_id, email, display_name, role, created_at
		 FROM users WHERE org_id = $1 ORDER BY created_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.OrgID, &u.Email, &u.DisplayName, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetDisabled enables or disables a user within an organization. Disabling also
// bumps the user's token version, revoking their outstanding refresh tokens.
func (s *Service) SetDisabled(ctx context.Context, orgID, userID uuid.UUID, disabled bool) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE users SET disabled = $1,
		    token_version = token_version + CASE WHEN $1 THEN 1 ELSE 0 END
		 WHERE id = $2 AND org_id = $3`,
		disabled, userID, orgID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("user not found")
	}
	return nil
}

// GetUser returns a user by id, scoped to an organization.
func (s *Service) GetUser(ctx context.Context, orgID, userID uuid.UUID) (User, error) {
	var u User
	err := s.db.QueryRow(ctx,
		`SELECT id, org_id, email, display_name, role, created_at, token_version
		 FROM users WHERE id = $1 AND org_id = $2`,
		userID, orgID,
	).Scan(&u.ID, &u.OrgID, &u.Email, &u.DisplayName, &u.Role, &u.CreatedAt, &u.TokenVersion)
	return u, err
}

// BumpTokenVersion increments a user's token version, revoking their outstanding
// refresh tokens (used on logout).
func (s *Service) BumpTokenVersion(ctx context.Context, orgID, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET token_version = token_version + 1 WHERE id = $1 AND org_id = $2`,
		userID, orgID,
	)
	return err
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// dummyHash is a valid argon2id hash of a random password, used to equalize the
// timing of failed logins for non-existent users.
const dummyHash = "$argon2id$v=19$m=65536,t=1,p=4$AAAAAAAAAAAAAAAAAAAAAA$RdescudvJCsgt3ub+b+dWRWJTmaaJObG"

func isUniqueViolation(err error) bool {
	// pgx surfaces a *pgconn.PgError with code 23505 for unique violations. We
	// match on the SQLSTATE substring to avoid importing pgconn here.
	return err != nil && strings.Contains(err.Error(), "23505")
}
