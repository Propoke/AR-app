# Backend API (Phase 1)

Base URL (local): `http://localhost:8080`

All request/response bodies are JSON. Authenticated endpoints require
`Authorization: Bearer <access_token>`.

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
  "ice_servers": [ { "urls": ["turn:127.0.0.1:3478?transport=udp"], "username": "...", "credential": "..." } ]
}
```

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
  "ice_servers": [ { "urls": ["..."], "username": "...", "credential": "..." } ]
}
```

Errors: `401` invalid/expired token, `429` too many attempts.

## Signaling WebSocket

```
GET /v1/signaling?room=<session_id>&role=<agent|phone>   (Upgrade: websocket)
```

Both peers connect to the same `room` (the `session_id`). The relay forwards each
peer's messages to the other and emits `peer-ready` once both are present. Message
envelope and types: see `shared/signaling.schema.json` and `shared/README.md`.

> Phase 1 uses the room id as the bearer for the room. Phase 2 will require a
> short-lived signaling join token minted at redeem/mint time.
