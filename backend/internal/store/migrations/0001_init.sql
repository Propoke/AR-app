-- Initial schema: organizations, users, and an immutable session audit log.
-- Ephemeral connection tokens and signaling-room membership live in Redis, not here.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE organizations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Roles within an organization. 'admin' manages users/seats, 'agent' runs support
-- sessions from the Windows app, 'viewer' has read-only access to history.
CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email         text NOT NULL,
    password_hash text NOT NULL,
    display_name  text NOT NULL DEFAULT '',
    role          text NOT NULL DEFAULT 'agent' CHECK (role IN ('admin', 'agent', 'viewer')),
    disabled      boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, email)
);

CREATE INDEX idx_users_email ON users (lower(email));

-- Durable record of support sessions for history and auditing. The live connection
-- token (ID + PIN) is never persisted here; only its lifecycle is recorded.
CREATE TABLE sessions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    agent_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Public, non-secret connection id shown to both parties (the "9-digit ID").
    connect_id    text NOT NULL,
    status        text NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'active', 'ended', 'expired')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    connected_at  timestamptz,
    ended_at      timestamptz
);

CREATE INDEX idx_sessions_org ON sessions (org_id, created_at DESC);
CREATE INDEX idx_sessions_agent ON sessions (agent_id, created_at DESC);

-- Append-only audit trail for security-relevant events.
CREATE TABLE audit_log (
    id          bigserial PRIMARY KEY,
    org_id      uuid,
    actor_id    uuid,
    action      text NOT NULL,
    detail      jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_org ON audit_log (org_id, created_at DESC);
