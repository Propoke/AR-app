// Package session issues and redeems short-lived connection tokens (a public
// connect ID plus a secret PIN), the TeamViewer-style handshake that pairs a
// technician with an end-user's phone.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/propoke/ar-app/backend/internal/auth"
	"github.com/propoke/ar-app/backend/internal/turn"
)

var (
	// ErrInvalidToken is returned when a connect ID/PIN pair is wrong or expired.
	ErrInvalidToken = errors.New("invalid or expired connection token")
	// ErrTooManyAttempts is returned after too many wrong PIN guesses.
	ErrTooManyAttempts = errors.New("too many attempts")
)

const (
	connectIDDigits = 9
	pinDigits       = 6
	maxRedeemTries  = 5

	roleAgent = "agent"
	rolePhone = "phone"
)

// Token is the minted connection token returned to the technician.
type Token struct {
	SessionID      uuid.UUID `json:"session_id"`
	ConnectID      string    `json:"connect_id"`
	PIN            string    `json:"pin"`
	ExpiresAt      time.Time `json:"expires_at"`
	SignalingToken string    `json:"signaling_token"`
}

// JoinInfo is returned to the phone after a successful redeem; it contains the
// signaling room, a signaling join token, and the ICE servers needed to
// establish the WebRTC connection.
type JoinInfo struct {
	SessionID      uuid.UUID        `json:"session_id"`
	Room           string           `json:"room"`
	SignalingToken string           `json:"signaling_token"`
	ICEServers     []turn.ICEServer `json:"ice_servers"`
}

// tokenRecord is the ephemeral state stored in Redis for an outstanding token.
type tokenRecord struct {
	SessionID string `json:"session_id"`
	OrgID     string `json:"org_id"`
	AgentID   string `json:"agent_id"`
	PINHash   string `json:"pin_hash"`
}

// Service mints and redeems connection tokens.
type Service struct {
	db     *pgxpool.Pool
	redis  *redis.Client
	turn   *turn.Minter
	issuer *auth.Issuer
	ttl    time.Duration
}

// NewService constructs a session Service.
func NewService(db *pgxpool.Pool, rdb *redis.Client, turn *turn.Minter, issuer *auth.Issuer, ttl time.Duration) *Service {
	return &Service{db: db, redis: rdb, turn: turn, issuer: issuer, ttl: ttl}
}

// Mint creates a new pending session and a single-use connection token for it.
// The PIN is returned to the caller exactly once and only its hash is stored.
func (s *Service) Mint(ctx context.Context, orgID, agentID uuid.UUID) (Token, error) {
	pin, err := randomDigits(pinDigits)
	if err != nil {
		return Token{}, err
	}

	// Find an unused connect ID (collisions are astronomically unlikely but the
	// SET NX below guarantees uniqueness across live tokens regardless).
	var connectID string
	var sessionID uuid.UUID
	expiresAt := time.Now().Add(s.ttl)

	for tries := 0; tries < 5; tries++ {
		connectID, err = randomDigits(connectIDDigits)
		if err != nil {
			return Token{}, err
		}

		// Persist the pending session row first so we have a session id.
		err = s.db.QueryRow(ctx,
			`INSERT INTO sessions (org_id, agent_id, connect_id, status)
			 VALUES ($1, $2, $3, 'pending') RETURNING id`,
			orgID, agentID, connectID,
		).Scan(&sessionID)
		if err != nil {
			return Token{}, fmt.Errorf("create session: %w", err)
		}

		rec := tokenRecord{
			SessionID: sessionID.String(),
			OrgID:     orgID.String(),
			AgentID:   agentID.String(),
			PINHash:   hashPIN(pin),
		}
		payload, _ := json.Marshal(rec)
		ok, rerr := s.redis.SetNX(ctx, tokenKey(connectID), payload, s.ttl).Result()
		if rerr != nil {
			return Token{}, fmt.Errorf("store token: %w", rerr)
		}
		if ok {
			sigTok, err := s.issuer.IssueSignaling(sessionID.String(), string(roleAgent))
			if err != nil {
				return Token{}, fmt.Errorf("issue signaling token: %w", err)
			}
			return Token{
				SessionID:      sessionID,
				ConnectID:      connectID,
				PIN:            pin,
				ExpiresAt:      expiresAt,
				SignalingToken: sigTok,
			}, nil
		}
		// Connect ID already in use; roll back the orphaned session and retry.
		_, _ = s.db.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID)
	}
	return Token{}, errors.New("could not allocate a unique connect id")
}

// Redeem validates a connect ID/PIN pair from the phone. On success it marks the
// session active, consumes the token (single-use), and returns join info with
// fresh ICE servers.
func (s *Service) Redeem(ctx context.Context, connectID, pin string) (JoinInfo, error) {
	key := tokenKey(connectID)
	raw, err := s.redis.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return JoinInfo{}, ErrInvalidToken
	}
	if err != nil {
		return JoinInfo{}, fmt.Errorf("load token: %w", err)
	}

	// Count attempts against this connect ID; bail out after the limit.
	attempts, err := s.redis.Incr(ctx, attemptsKey(connectID)).Result()
	if err != nil {
		return JoinInfo{}, fmt.Errorf("track attempts: %w", err)
	}
	if attempts == 1 {
		_ = s.redis.Expire(ctx, attemptsKey(connectID), s.ttl).Err()
	}
	if attempts > maxRedeemTries {
		s.invalidate(ctx, connectID)
		return JoinInfo{}, ErrTooManyAttempts
	}

	var rec tokenRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return JoinInfo{}, fmt.Errorf("decode token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(hashPIN(pin)), []byte(rec.PINHash)) != 1 {
		return JoinInfo{}, ErrInvalidToken
	}

	sessionID, err := uuid.Parse(rec.SessionID)
	if err != nil {
		return JoinInfo{}, ErrInvalidToken
	}

	// Single-use: consume the token now that the PIN is verified.
	s.invalidate(ctx, connectID)

	if _, err := s.db.Exec(ctx,
		`UPDATE sessions SET status = 'active', connected_at = now()
		 WHERE id = $1 AND status = 'pending'`,
		sessionID,
	); err != nil {
		return JoinInfo{}, fmt.Errorf("activate session: %w", err)
	}

	sigTok, err := s.issuer.IssueSignaling(sessionID.String(), string(rolePhone))
	if err != nil {
		return JoinInfo{}, fmt.Errorf("issue signaling token: %w", err)
	}

	return JoinInfo{
		SessionID:      sessionID,
		Room:           sessionID.String(),
		SignalingToken: sigTok,
		ICEServers:     s.turn.Credentials(sessionID.String()),
	}, nil
}

// ICEServers returns fresh ICE servers for an already-known session (used by the
// authenticated agent side, which never redeems a PIN).
func (s *Service) ICEServers(sessionID uuid.UUID) []turn.ICEServer {
	return s.turn.Credentials(sessionID.String())
}

func (s *Service) invalidate(ctx context.Context, connectID string) {
	_, _ = s.redis.Del(ctx, tokenKey(connectID), attemptsKey(connectID)).Result()
}

func tokenKey(connectID string) string    { return "session:token:" + connectID }
func attemptsKey(connectID string) string { return "session:attempts:" + connectID }

func hashPIN(pin string) string {
	sum := sha256.Sum256([]byte(pin))
	return hex.EncodeToString(sum[:])
}

// randomDigits returns a cryptographically random decimal string of length n,
// preserving leading zeros.
func randomDigits(n int) (string, error) {
	const digits = "0123456789"
	buf := make([]byte, n)
	for i := range buf {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		if err != nil {
			return "", err
		}
		buf[i] = digits[idx.Int64()]
	}
	return string(buf), nil
}
