# Operations runbook

> Deploying on **Proxmox**? See `docs/deploy-proxmox.md` for a VM/LXC walkthrough,
> including the coturn NAT/port-forwarding specifics.

## Deploy (self-hosted)

1. Provision hosts/DNS/firewall per `infra/terraform/` (app host, coturn host with a
   public IP and the UDP relay range open, Postgres, Redis).
2. Set secrets/domain in the environment (see `infra/.env.example`): `JWT_SECRET`,
   `TURN_SECRET` (must match `coturn/turnserver.conf`), DB creds, `DOMAIN`,
   `TURN_URLS` (public coturn address).
3. Bring up the stack:
   ```bash
   cd infra
   DOMAIN=support.example.com docker compose -f docker-compose.prod.yml up -d --build
   ```
4. Caddy obtains TLS automatically. Verify:
   ```bash
   curl https://$DOMAIN/healthz     # -> ok
   curl https://$DOMAIN/readyz      # -> ready (200) when DB+Redis are up
   ```

## Health & readiness
- `GET /healthz` — process liveness (always 200 if serving).
- `GET /readyz` — 200 only when Postgres and Redis are reachable; use for load-balancer
  readiness and deploy gating. Returns 503 if a dependency is down.

## Observability
- `GET /metrics` — Prometheus. Key series:
  - `ar_http_requests_total{route,status}` — traffic and error rates.
  - `ar_http_request_duration_seconds` — latency histogram.
  - `ar_signaling_rooms` — active sessions (sampled every 15s).
- Point Prometheus at `/metrics`; build Grafana panels for error rate, p95 latency,
  and active rooms. Alert on readiness failures and elevated 5xx.

## Common incidents
| Symptom | Likely cause | Action |
|---------|--------------|--------|
| Sessions connect then drop / no media | coturn unreachable | Check coturn host public IP, UDP 3478 + relay range open, `TURN_URLS`/`TURN_SECRET` match |
| `/readyz` 503 | Postgres or Redis down | Check the data stores; backend fails closed on readiness only, requests still error |
| Logins/redeems returning 429 | Rate limit tripped (brute force or NAT sharing) | Expected protection; tune limits in `cmd/server/main.go` if a NAT legitimately shares an IP |
| Signaling 401 | Expired/invalid signaling token | Token TTL is `SIGNALING_TOKEN_TTL` (default 4h); the client must re-mint/redeem |
| Phone can't redeem | Token expired (TTL) or already used | Connection tokens are single-use, ~10 min TTL; agent mints a fresh one |

## Scaling
- Signaling delivery is pluggable via `SIGNALING_FANOUT`. Default `local` is
  single-instance; set `redis` to distribute frames and presence across replicas
  over Redis pub/sub (`internal/signaling/redis.go`). Validate a two-instance
  deployment before relying on it — the local path is unit-tested, the Redis path
  needs a multi-instance integration test.
- coturn scales horizontally behind the shared secret; media never transits the app.
- Postgres/Redis: use managed instances with backups in production. With
  `SIGNALING_FANOUT=redis`, Redis is on the signaling hot path — size accordingly.

## Secrets rotation
- Rotating `JWT_SECRET` invalidates outstanding access/refresh and signaling tokens
  (users re-login; in-flight sessions re-handshake). Roll during a maintenance window.
- Rotate `TURN_SECRET` in coturn and the backend together.
