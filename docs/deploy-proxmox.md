# Deploying the backend to a Proxmox cluster

This deploys the **backend** (API, signaling, coturn, Postgres, Redis, monitoring,
and optional TLS) onto Proxmox. The clients (Windows agent, phone app) then point
at it — see `docs/first-session.md`.

The whole stack is Docker Compose, run inside **one VM** (or LXC). A Proxmox
*cluster* only matters for HA/multi-node — covered at the end; a single VM is the
right start.

## Two access scenarios (one compose file)

`docker-compose.prod.yml` supports both, and you can move from one to the other
without rebuilding:

1. **WireGuard / LAN (start here).** Phone and laptop join a WireGuard tunnel to
   your home network and reach the VM directly. The tunnel already encrypts
   everything, so you can skip TLS and use `http://<vm-ip>:8080`. **There is no NAT
   between peers inside the tunnel**, which removes the usual coturn headache.
2. **External / public (later).** Expose the API over HTTPS via Caddy and open the
   media ports to the internet for clients that aren't on the tunnel.

> The only networking rule that matters for media: **coturn's `TURN_EXTERNAL_IP`
> must be the address clients actually reach it on**, and the UDP relay range must
> be reachable. In scenario 1 that's the VM's tunnel/LAN IP (easy). In scenario 2
> it's your WAN IP with port-forwards.

---

## 1. Create the VM (recommended over LXC for Docker)

- **Template**: Debian 12 (cloud image or ISO).
- **Resources**: 2 vCPU / 4 GB RAM / 40 GB disk to start (coturn is light; media
  never transits the app).
- **Disk**: on ZFS/SSD. Postgres lives here.
- **Network**: bridged (`vmbr0`) with a **static DHCP lease** so its IP is stable.
- Enable the **QEMU guest agent** for clean snapshots.

Install Docker:
```bash
sudo apt update && sudo apt install -y ca-certificates curl git
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker "$USER"   # re-login after this
```

## 2. WireGuard tunnel

Run WireGuard so the phone and laptop can reach the VM's network from anywhere.
Two common places to run the WG server:
- **On your router/firewall** (OPNsense/pfSense/UniFi) — clients get routes to the
  LAN, and reach the VM at its **LAN IP** (e.g. `192.168.1.50`).
- **On the VM itself** (or a dedicated LXC) — clients reach it at its **WG IP**
  (e.g. `10.8.0.1`).

Either way, note the IP the clients will use to reach the VM — call it `VM_IP`.
That single IP is what goes in `TURN_EXTERNAL_IP`, `TURN_URLS`, and the client
Server URL.

> **MTU**: WireGuard adds ~60 bytes of overhead. If media stutters or large frames
> fail, lower the WG interface MTU (e.g. 1380) on the clients.

## 3. Get the code and configure
```bash
git clone <your-repo-url> ar-app && cd ar-app/infra
cp .env.example .env
```
Edit `.env` for the **WireGuard scenario**:
```bash
JWT_SECRET=<openssl rand -base64 48>
TURN_SECRET=<openssl rand -base64 48>
POSTGRES_USER=ar
POSTGRES_PASSWORD=<random>
POSTGRES_DB=ar

# The IP clients reach the VM on over the tunnel/LAN (VM_IP from §2):
TURN_EXTERNAL_IP=10.8.0.1
TURN_URLS=turn:10.8.0.1:3478?transport=udp

# Grafana admin (published on :3000 over the tunnel — never expose to the WAN):
GRAFANA_PASSWORD=<random>

# DOMAIN is only needed for the external/TLS scenario; leave blank for now.
# DOMAIN=support.example.com
```
coturn reads `TURN_SECRET` and `TURN_EXTERNAL_IP` directly, so its secret always
matches the backend.

## 4. Bring it up (WireGuard scenario)
```bash
cd ar-app/infra
docker compose -f docker-compose.prod.yml up -d --build
```
This starts the backend, Postgres, Redis, coturn, Prometheus, and Grafana — but
**not** Caddy (it's behind the `tls` profile). Verify from a device on the tunnel:
```bash
curl http://<VM_IP>:8080/healthz     # -> ok
curl http://<VM_IP>:8080/readyz      # -> ready
```
Create the first technician:
```bash
curl -sX POST http://<VM_IP>:8080/v1/auth/register -H 'Content-Type: application/json' \
  -d '{"org_name":"Acme","email":"tech@acme.com","password":"hunter2hunter2","display_name":"Tech"}'
```
Then in the clients set the **Server URL** to `http://<VM_IP>:8080` (the agent's
Server field; Unity's backend URL). The WS URL (`ws://<VM_IP>:8080`) is derived.
Continue with `docs/first-session.md`.

## 5. Monitoring (Prometheus + Grafana)

Both start automatically. Prometheus scrapes the backend's `/metrics` on the
internal network; Grafana is published on **`:3000`** (tunnel/LAN only).

- Open `http://<VM_IP>:3000`, log in (`admin` / your `GRAFANA_PASSWORD`).
- The **Prometheus datasource** and an **"AR App Overview"** dashboard are
  auto-provisioned (active signaling rooms, request rate by status, p95 latency,
  5xx error rate). Config lives in `infra/prometheus/` and `infra/grafana/`.
- Do **not** forward port 3000 to the internet. Reach it over the tunnel, or put it
  behind Caddy with auth in the external scenario.

## 6. Going external (later)

When you want clients that aren't on the tunnel:

1. **DNS**: point `DOMAIN` (e.g. `support.example.com`) at your WAN IP.
2. **Router port-forwards** to the VM's LAN IP:
   | Port | Proto | Purpose |
   |------|-------|---------|
   | 80, 443 | TCP | Caddy: ACME + HTTPS/WSS |
   | 3478 | UDP | TURN/STUN |
   | 49160–49200 | UDP | TURN relay range |
   (If you enable the **Proxmox firewall**, allow the same at the node/VM level.)
3. **`.env` changes**: set `DOMAIN`, point `TURN_URLS` at the domain
   (`turn:support.example.com:3478?transport=udp`), and set `TURN_EXTERNAL_IP` to
   your **WAN IP**. If the VM is behind NAT you can use coturn's mapping form
   `TURN_EXTERNAL_IP=<WAN_IP>` (the `-n` config advertises it for relay candidates).
4. **Start with TLS**:
   ```bash
   docker compose -f docker-compose.prod.yml --profile tls up -d
   ```
   Caddy obtains a Let's Encrypt cert automatically. Clients now use
   `https://$DOMAIN` (WS derives to `wss://`).

> **Serving both at once** (tunnel clients on the WG IP *and* WAN clients): coturn
> can advertise multiple relay addresses. Run it with several `--external-ip` flags
> (one per reachable address) — the simplest path is a dedicated coturn config file
> listing each `external-ip`. Only needed once you truly have both client types.

> **TLS without opening 80/443** (e.g. tunnel-only but you still want real certs):
> use a Caddy build with a DNS provider plugin and the ACME **DNS-01** challenge, so
> the cert is issued via DNS instead of inbound HTTP. Media (coturn UDP) still needs
> to be reachable regardless.

## 7. Backups, updates
- **Backups**: schedule a Proxmox backup job for the VM. Also `pg_dump` on cron:
  `docker compose -f docker-compose.prod.yml exec -T postgres pg_dump -U ar ar > dump.sql`.
- **Updates**: `git pull && docker compose -f docker-compose.prod.yml up -d --build`
  (add `--profile tls` if you run TLS). Migrations apply automatically on startup.

## 8. Optional: HA across the Proxmox cluster
A single VM is fine for most use. For redundancy:
- Run the backend on **2+ VMs** on different nodes with `SIGNALING_FANOUT=redis`
  (validate a two-node test first — see `docs/runbook.md`).
- Use **shared Postgres and Redis** rather than compose-local ones.
- Front the API/WSS with a load balancer; coturn scales behind the shared secret.
- Enable **Proxmox HA** so a VM restarts on another node on failure.

---

## Appendix: LXC instead of a VM
Docker runs in an **unprivileged** Debian/Ubuntu LXC with `features:
nesting=1,keyctl=1`. Install Docker as in §1. `network_mode: host` (coturn) works;
confirm the relay UDP range is reachable through the container's network. The VM
path is more predictable for Docker — use LXC only if you're comfortable with the
caveats.

## Repo changes made for this deployment
- `docker-compose.prod.yml`: coturn reads `TURN_SECRET`/`TURN_REALM`/
  `TURN_EXTERNAL_IP`/relay range from the env (fixes a secret mismatch, enables
  NAT/tunnel addressing); Caddy is behind a `tls` profile so the WireGuard scenario
  runs without it; the backend is published on `:8080` for direct tunnel access;
  **Prometheus + Grafana** added with auto-provisioned datasource and dashboard.
- `.env.example`: `TURN_EXTERNAL_IP`, `TURN_MIN_PORT`, `TURN_MAX_PORT`,
  `GRAFANA_USER`, `GRAFANA_PASSWORD`.
- `infra/prometheus/prometheus.yml`, `infra/grafana/**`: monitoring config.
No application code changes are needed.
