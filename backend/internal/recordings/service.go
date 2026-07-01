// Package recordings manages session recordings stored in S3-compatible object
// storage. Clients upload bytes directly to the bucket via a presigned URL; the
// backend only tracks metadata and never proxies the media.
package recordings

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MaxRecordingSizeBytes bounds the size Complete will accept. sizeBytes is
// self-reported by the uploading client — the backend never proxies media, so
// it cannot verify the object's actual size without an additional signed
// HEAD/GetObjectAttributes request against the bucket (not implemented here).
// This bound only rejects obviously invalid values (non-positive or absurdly
// large); it is not a substitute for verifying against the object store if
// accurate sizes matter for billing or quota enforcement.
const MaxRecordingSizeBytes = 10 << 30 // 10 GiB

// ErrInvalidSize is returned by Complete when sizeBytes is outside the allowed range.
var ErrInvalidSize = errors.New("size_bytes must be positive and within the allowed maximum")

// Presigner produces presigned upload URLs for object keys. Implemented by the
// MinIO/S3 adapter; faked in tests.
type Presigner interface {
	PresignPut(ctx context.Context, objectKey, contentType string, expiry time.Duration) (string, error)
}

// Recording is a stored or in-progress session recording.
type Recording struct {
	ID        uuid.UUID `json:"id"`
	SessionID uuid.UUID `json:"session_id"`
	ObjectKey string    `json:"object_key"`
	Content   string    `json:"content_type"`
	SizeBytes int64     `json:"size_bytes"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// Upload is returned when a recording slot is created: the client PUTs the bytes
// to URL, then calls Complete.
type Upload struct {
	RecordingID uuid.UUID `json:"recording_id"`
	ObjectKey   string    `json:"object_key"`
	URL         string    `json:"upload_url"`
	ExpiresIn   int       `json:"expires_in_seconds"`
}

// Service tracks recording metadata and mints upload URLs.
type Service struct {
	db        *pgxpool.Pool
	presigner Presigner
	urlTTL    time.Duration
}

// NewService constructs a recordings Service.
func NewService(db *pgxpool.Pool, presigner Presigner, urlTTL time.Duration) *Service {
	return &Service{db: db, presigner: presigner, urlTTL: urlTTL}
}

// CreateUpload reserves a recording row and returns a presigned PUT URL. The
// session must belong to the caller's org.
func (s *Service) CreateUpload(ctx context.Context, orgID, sessionID uuid.UUID, contentType string) (Upload, error) {
	if contentType == "" {
		contentType = "video/webm"
	}
	recordingID := uuid.New()
	objectKey := fmt.Sprintf("recordings/%s/%s.webm", sessionID, recordingID)

	// Verify the session is in the caller's org before issuing an upload URL.
	var exists bool
	if err := s.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM sessions WHERE id=$1 AND org_id=$2)`,
		sessionID, orgID,
	).Scan(&exists); err != nil {
		return Upload{}, err
	}
	if !exists {
		return Upload{}, fmt.Errorf("session not found")
	}

	url, err := s.presigner.PresignPut(ctx, objectKey, contentType, s.urlTTL)
	if err != nil {
		return Upload{}, fmt.Errorf("presign: %w", err)
	}

	if _, err := s.db.Exec(ctx,
		`INSERT INTO recordings (id, session_id, org_id, object_key, content_type, status)
		 VALUES ($1, $2, $3, $4, $5, 'pending')`,
		recordingID, sessionID, orgID, objectKey, contentType,
	); err != nil {
		return Upload{}, err
	}

	return Upload{
		RecordingID: recordingID,
		ObjectKey:   objectKey,
		URL:         url,
		ExpiresIn:   int(s.urlTTL.Seconds()),
	}, nil
}

// Complete marks a recording available after the client finishes uploading.
func (s *Service) Complete(ctx context.Context, orgID, recordingID uuid.UUID, sizeBytes int64) error {
	if sizeBytes <= 0 || sizeBytes > MaxRecordingSizeBytes {
		return ErrInvalidSize
	}
	tag, err := s.db.Exec(ctx,
		`UPDATE recordings SET status='available', size_bytes=$1, completed_at=now()
		 WHERE id=$2 AND org_id=$3 AND status='pending'`,
		sizeBytes, recordingID, orgID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("recording not found or already finalized")
	}
	return nil
}

// List returns recordings for a session in the caller's org.
func (s *Service) List(ctx context.Context, orgID, sessionID uuid.UUID) ([]Recording, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, session_id, object_key, content_type, size_bytes, status, created_at
		 FROM recordings WHERE session_id=$1 AND org_id=$2 ORDER BY created_at DESC`,
		sessionID, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Recording
	for rows.Next() {
		var r Recording
		if err := rows.Scan(&r.ID, &r.SessionID, &r.ObjectKey, &r.Content, &r.SizeBytes, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
