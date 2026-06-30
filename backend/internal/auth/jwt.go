// Package auth issues and validates JWT access/refresh tokens and provides HTTP
// middleware for authenticating requests.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenKind distinguishes access tokens from refresh tokens.
type TokenKind string

const (
	KindAccess  TokenKind = "access"
	KindRefresh TokenKind = "refresh"
)

// Claims is the JWT payload for authenticated users.
type Claims struct {
	OrgID string    `json:"org"`
	Role  string    `json:"role"`
	Kind  TokenKind `json:"knd"`
	jwt.RegisteredClaims
}

// KindSignaling marks a token that authorizes joining one signaling room as one role.
const KindSignaling TokenKind = "signaling"

// SignalingClaims authorizes a peer to join a specific signaling room as a role.
type SignalingClaims struct {
	Room string    `json:"room"`
	Role string    `json:"role"`
	Kind TokenKind `json:"knd"`
	jwt.RegisteredClaims
}

// Issuer mints and verifies tokens with a single HMAC secret.
type Issuer struct {
	secret       []byte
	accessTTL    time.Duration
	refreshTTL   time.Duration
	signalingTTL time.Duration
}

// NewIssuer constructs an Issuer.
func NewIssuer(secret []byte, accessTTL, refreshTTL, signalingTTL time.Duration) *Issuer {
	return &Issuer{secret: secret, accessTTL: accessTTL, refreshTTL: refreshTTL, signalingTTL: signalingTTL}
}

// IssueSignaling mints a short-lived token authorizing the holder to join the
// given room as the given role on the signaling WebSocket.
func (i *Issuer) IssueSignaling(room, role string) (string, error) {
	now := time.Now()
	claims := SignalingClaims{
		Room: room,
		Role: role,
		Kind: KindSignaling,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.signalingTTL)),
			ID:        uuid.NewString(),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("sign signaling token: %w", err)
	}
	return signed, nil
}

// ParseSignaling validates a signaling token and returns its claims.
func (i *Issuer) ParseSignaling(raw string) (*SignalingClaims, error) {
	claims := &SignalingClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return i.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims.Kind != KindSignaling {
		return nil, errors.New("not a signaling token")
	}
	return claims, nil
}

// Issue creates a signed token for the given user.
func (i *Issuer) Issue(kind TokenKind, userID, orgID uuid.UUID, role string) (string, time.Time, error) {
	ttl := i.accessTTL
	if kind == KindRefresh {
		ttl = i.refreshTTL
	}
	now := time.Now()
	exp := now.Add(ttl)
	claims := Claims{
		OrgID: orgID.String(),
		Role:  role,
		Kind:  kind,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.NewString(),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, exp, nil
}

// Parse validates a token's signature and expiry and returns its claims.
func (i *Issuer) Parse(raw string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return i.secret, nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}
