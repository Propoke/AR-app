# Deploying the backend to a Proxmox cluster

This deploys the **backend** (API, signaling, coturn, Postgres, Redis, Caddy TLS)
onto Proxmox. The clients (Windows agent, phone app) then point at it — see
`docs/first-session.md`.

The whole stack is Docker Compose, so on Proxmox you run it inside **one VM** (or
LXC). A Proxmox *cluster* only matters if you want HA/multi-node — covered at the
end; a single VM is the right starting point.

> **The one thing that makes or breaks this: coturn + NAT.** WebRTC media only
> flows if both peers can reach coturn over UDP. On Proxmox your VM is almost
> always behind NAT, so you must (a) set `TURN_EXTERNAL_IP` to the address clients
> actually reach, and (b) open/forward UDP 3478 **and** the relay range. Get this
> wrong and sessions connect but show no video.

---

## 1. Create the VM (recommended over LXC for Docker)

A VM avoids the Docker-in-LXC caveats (nesting, keyctl, cgroups). LXC works too —
see the appendix.

- **Template**: Debian 12 (cloud image or ISO).
- **Resources**: 2 vCPU / 4 GB RAM / 40 GB disk is plenty to start (coturn is light;
  media never transits the app). Bump CPU if you expect many concurrent sessions.
- **Disk**: put it on your fast storage (ZFS/SSD). Postgres lives here.
- **Network**: bridged (`vmbr0`) so it gets a LAN IP. Give it a **static lease** on
  your router/DHCP so port-forwards stay valid.
- **Guest agent**: enable it (QEMU guest agent) for clean snapshots.

Install Docker in the VM:
```bash
sudo apt update && sudo apt install -y ca-certificates curl git
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker "$USER"   # re-login after this
```

## 2. Get the code and configure
```bash
git clone <your-repo-url> ar-app && cd ar-app/infra
cp .env.example .env
```
Edit `.env` — the values that matter for prod:
```bash
# Strong secrets (generate with: openssl rand -base64 48)
JWT_SECRET=<random>
TURN_SECRET=<random>
POSTGRES_USER=ar
POSTGRES_PASSWORD=<random>
POSTGRES_DB=ar

# The public domain Caddy serves + gets a TLS cert for (see §4).
DOMAIN=support.example.com

# coturn reachability (see §3). Clients connect to this URL:
TURN_URLS=turn:support.example.com:3478?transport=udp
# The IP clients reach coturn on — your WAN/public IP (or the VM LAN IP for a
# LAN-only test). REQUIRED in prod.
TURN_EXTERNAL_IP=203.0.113.10
```
With `docker-compose.prod.yml`, coturn now takes `TURN_SECRET` and
`TURN_EXTERNAL_IP` directly from the environment, so its shared secret always
matches the backend (a common misconfiguration otherwise).

## 3. Networking — the critical part

Ports the backend needs reachable **from the clients**:

| Port | Proto | Purpose | Where clients are |
|------|-------|---------|-------------------|
| 443 (and 80 for ACME) | TCP | HTTPS API + WSS signaling (via Caddy) | anywhere |
| 3478 | UDP | TURN/STUN | anywhere |
| 49160–49200 | UDP | TURN relay range | anywhere |

Pick your scenario:

**A. LAN only** (agent PC + phone on the same network as the VM):
- `TURN_EXTERNAL_IP` = the VM's LAN IP (e.g. `192.168.1.50`).
- `DOMAIN`/`TURN_URLS` = that LAN IP or a local hostname. No router changes.
- TLS: use a real domain with DNS-01 (§4) or accept plain HTTP for testing.

**B. Internet reachable** (phone on cellular) — **home-lab standard**:
- On your **router**, port-forward to the VM's LAN IP:
  - TCP 80, 443 → VM
  - UDP 3478 → VM
  - UDP 49160–49200 → VM
- `TURN_EXTERNAL_IP` = your **WAN/public IP**. Point `DOMAIN`'s DNS A record at it.
- If you use the **Proxmox firewall**, allow the same ports at the datacenter/node/VM
  level (it's off by default; if you enable it, add these rules).

> A pure HTTP tunnel (e.g. Cloudflare Tunnel) can carry the API + signaling, but
> **not** TURN media (that's raw UDP). For remote/cellular clients you still need
> the UDP ports reachable. Keep media on a direct port-forward.

## 4. TLS

Caddy gets a certificate automatically. Two ways:

- **HTTP challenge (simplest)**: forward TCP 80 + 443 to the VM (scenario B).
  Caddy obtains a Let's Encrypt cert on first run. Nothing else to do.
- **DNS challenge (no inbound 80/443 needed, good for LAN-only)**: use a Caddy
  build with your DNS provider plugin and set the provider credentials. This lets
  Caddy prove the domain via DNS, so you get real certs even without exposing 80/443.

The clients (WinUI `HttpClient`, Unity `UnityWebRequest`/`ClientWebSocket`) validate
TLS, so prefer real certs over self-signed.

## 5. Bring it up
```bash
cd ar-app/infra
docker compose -f docker-compose.prod.yml up -d --build
docker compose -f docker-compose.prod.yml ps
```
Verify:
```bash
curl https://$DOMAIN/healthz     # -> ok
curl https://$DOMAIN/readyz      # -> ready (200) once Postgres+Redis are up
```
Create the first technician (once):
```bash
curl -sX POST https://$DOMAIN/v1/auth/register -H 'Content-Type: application/json' \
  -d '{"org_name":"Acme","email":"tech@acme.com","password":"hunter2hunter2","display_name":"Tech"}'
```
Test TURN reachability from outside (e.g. the Trickle ICE page, or `turnutils_uclient`):
point a WebRTC ICE test at `turn:$DOMAIN:3478` with a credential minted by the
backend — you should see a `relay` candidate. No relay candidate = §3 is wrong.

Then follow `docs/first-session.md` for the clients.

## 6. Backups, updates, monitoring
- **Backups**: schedule Proxmox VM snapshots/backups (Backup job → this VM). For
  Postgres specifically, also run `pg_dump` on a cron to a separate location:
  `docker compose -f docker-compose.prod.yml exec -T postgres pg_dump -U ar ar > dump.sql`.
- **Updates**: `git pull && docker compose -f docker-compose.prod.yml up -d --build`.
  The backend applies DB migrations automatically on startup.
- **Monitoring**: `/metrics` is Prometheus-formatted. Run Prometheus + Grafana
  (add them to the compose or a separate LXC) and scrape `https://$DOMAIN/metrics`.
  See `docs/runbook.md` for the key series and alerts.

## 7. Optional: HA across the Proxmox cluster
A single VM is fine for most use. For redundancy:
- Run the backend on **2+ VMs** on different nodes, with `SIGNALING_FANOUT=redis`
  so signaling works across replicas (see `docs/runbook.md` — validate a two-node
  test first).
- Use **shared Postgres and Redis** (managed, or their own HA setup) rather than the
  compose-local ones, so state is shared.
- Put a load balancer (HAProxy/Caddy) in front for the API/WSS. coturn can run on
  each node or a dedicated one; it scales horizontally behind the shared secret.
- Enable **Proxmox HA** on the VMs so they restart on another node if one fails.

This is only worth it once a single VM is proven end-to-end.

---

## Appendix: LXC instead of a VM
Docker runs in an LXC with some extra setup:
- Use an **unprivileged** Debian/Ubuntu container with `features: nesting=1,keyctl=1`.
- Install Docker as in §1.
- `network_mode: host` (coturn) works, but confirm the relay UDP range is reachable
  through the container's network.
- Snapshots are lighter than a VM, but the VM path is more predictable for Docker —
  use LXC only if you're comfortable with the caveats.

## Repo changes made for Proxmox/prod
- `docker-compose.prod.yml`: coturn now reads `TURN_SECRET`, `TURN_REALM`,
  `TURN_EXTERNAL_IP`, and the relay range from the environment (via CLI flags),
  fixing the secret mismatch and enabling NAT traversal.
- `.env.example`: added `TURN_EXTERNAL_IP`, `TURN_MIN_PORT`, `TURN_MAX_PORT`.
No application code changes are needed to deploy on Proxmox.
