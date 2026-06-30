# Backend

Go modular monolith hosting three services behind one HTTP server:

- **identity** — orgs, users, roles, JWT auth (argon2id passwords)
- **session** — connection-token mint/redeem (Redis), session history (Postgres)
- **signaling** — WebRTC SDP/ICE/annotation relay over WebSockets

It is structured so each service can later be split into its own deployable.

## Layout
```
cmd/server          process entrypoint (wires everything)
internal/config     env-based configuration
internal/store      Postgres + Redis connections, embedded SQL migrations
internal/crypto     argon2id password hashing
internal/auth       JWT issue/verify + middleware
internal/identity   user/org service + HTTP handlers
internal/session    token mint/redeem service + HTTP handlers
internal/turn       coturn REST credential minting
internal/signaling  in-memory room hub + WebSocket handler
internal/httpapi    router + logging/recovery middleware
internal/httputil   JSON request/response helpers
```

## Run locally
The easiest path is the full stack via Docker:
```bash
cd ../infra && docker compose up --build
```

Or run just the backend against your own Postgres/Redis:
```bash
export DATABASE_URL="postgres://ar:ar@localhost:5432/ar?sslmode=disable"
export REDIS_URL="redis://localhost:6379/0"
go run ./cmd/server
```
Migrations run automatically on startup.

## Develop
```bash
go test -race ./...   # tests
go vet ./...          # static checks
gofmt -l .            # formatting (CI enforces this)
```

## Configuration
All via environment variables (see `internal/config/config.go`):

| Var | Default | Notes |
|-----|---------|-------|
| `HTTP_ADDR` | `:8080` | listen address |
| `DATABASE_URL` | local dev dsn | Postgres |
| `REDIS_URL` | local dev url | Redis |
| `JWT_SECRET` | insecure dev value | **set a strong secret in prod** |
| `ACCESS_TOKEN_TTL` | `15m` | duration or seconds |
| `REFRESH_TOKEN_TTL` | `720h` | |
| `SESSION_TOKEN_TTL` | `10m` | connect ID/PIN lifetime |
| `TURN_SECRET` | dev value | must match coturn `static-auth-secret` |
| `TURN_URLS` | local turn | comma-separated ICE URLs |
| `TURN_CRED_TTL` | `1h` | minted TURN credential lifetime |

See `../docs/api.md` for endpoints and `../shared/` for the message schemas.
