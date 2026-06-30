# Backend API

Base URL (local): `http://localhost:8080`

All request/response bodies are JSON. Authenticated endpoints require
`Authorization: Bearer <access_token>`.

**Rate limits:** `/v1/auth/login` and `/v1/sessions/redeem` are throttled per
client IP (10/min and 20/min respectively); exceeding the limit returns `429` with
a `Retry-After` header. `/metrics` (Prometheus) and `/readyz` (dependency
readiness) are also exposed.

## Health

```
GET /healthz  ->  200 "ok"
```

## Auth

### Register an organization (self-service signup)
Creates a new org and its first **admin** user, returns a token pair.

```bash
curl -sX POST http://localhost:8080/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"org_name":"Acme Field Service","email":"tech@acme.com","password":"hunter2hunter2","display_name":"Tech One"}'
```
```json
{
  "access_token": "eyJ...",
  "refresh_token": "eyJ...",
  "expires_at": "2026-06-30T12:15:00Z",
  "user": { "id": "...", "org_id": "...", "email": "tech@acme.com", "role": "admin" }
}
```

### Login
```bash
curl -sX POST http://localhost:8080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"tech@acme.com","password":"hunter2hunter2"}'
```

### Refresh
```bash
curl -sX POST http://localhost:8080/v1/auth/refresh \
  -H 'Content-Type: application/json' \
  -d '{"refresh_token":"eyJ..."}'
```

### Current user
```bash
curl -s http://localhost:8080/v1/me -H "Authorization: Bearer $ACCESS"
```

## Sessions / connection tokens

### Mint a token (agent, authenticated)
The technician's Windows app calls this to start a session. The `pin` is shown
**once**; relay the `connect_id` + `pin` to the end-user out of band.

```bash
curl -sX POST http://localhost:8080/v1/sessions -H "Authorization: Bearer $ACCESS"
```
```json
{
  "session_id": "f2a1...",
  "connect_id": "048213765",
  "pin": "509134",
  "expires_at": "2026-06-30T12:10:00Z",
  "room": "f2a1...",
  "signaling_token": "eyJ...",
  "ice_servers": [ { "urls": ["turn:127.0.0.1:3478?transport=udp"], "username": "...", "credential": "..." } ]
}
```
`signaling_token` authorizes the agent to join `room` on the signaling WS.

### Redeem a token (phone, public)
The mobile app calls this with the values the user typed in. No auth — the token
is the credential. Single-use; max 5 PIN attempts before the token is burned.

```bash
curl -sX POST http://localhost:8080/v1/sessions/redeem \
  -H 'Content-Type: application/json' \
  -d '{"connect_id":"048213765","pin":"509134"}'
```
```json
{
  "session_id": "f2a1...",
  "room": "f2a1...",
  "signaling_token": "eyJ...",
  "ice_servers": [ { "urls": ["..."], "username": "...", "credential": "..." } ]
}
```

Errors: `401` invalid/expired token, `429` too many attempts.

## Session recordings (optional)

Available only when the backend has object storage configured (`S3_ENDPOINT`).
Clients upload bytes directly to the bucket via a presigned URL; the backend never
proxies media. All endpoints require a Bearer access token.

### Request an upload URL
`POST /v1/sessions/{id}/recordings` (body optional: `{"content_type":"video/webm"}`)
```json
{ "recording_id": "…", "object_key": "recordings/…/….webm", "upload_url": "https://…", "expires_in_seconds": 900 }
```
The client then `PUT`s the recording bytes to `upload_url`.

### Finalize after upload
`POST /v1/recordings/{id}/complete` with `{"size_bytes": 12345}` → `{ "status": "available" }`.

### List a session's recordings
`GET /v1/sessions/{id}/recordings` → `{ "recordings": [ … ] }`.

## Signaling WebSocket

```
GET /v1/signaling?room=<session_id>&role=<agent|phone>&token=<signaling_token>   (Upgrade: websocket)
```

Both peers connect to the same `room` (the `session_id`), presenting the
`signaling_token` from their mint/redeem response. The relay validates that the
token authorizes that exact room and role, then forwards each peer's messages to
the other and emits `peer-ready` once both are present. Message envelope and
types: see `shared/signaling.schema.json` and `shared/README.md`.

> The signaling token is a short-lived (default 4h) HMAC-signed JWT bound to
> `{room, role}`. The DB-free dev relay (`cmd/signaling-dev`) disables enforcement
> so the interop harness can run without minting tokens.
