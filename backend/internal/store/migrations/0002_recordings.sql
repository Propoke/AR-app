-- Session recordings stored in S3-compatible object storage. Rows track the
-- object lifecycle; the bytes live in the bucket, uploaded directly by the client
-- via a presigned URL (the backend never proxies the media).

CREATE TABLE recordings (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id   uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    object_key   text NOT NULL,
    content_type text NOT NULL DEFAULT 'video/webm',
    size_bytes   bigint NOT NULL DEFAULT 0,
    status       text NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'available', 'failed')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);

CREATE INDEX idx_recordings_session ON recordings (session_id, created_at DESC);
CREATE INDEX idx_recordings_org ON recordings (org_id, created_at DESC);
