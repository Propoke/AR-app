# Terraform (skeleton)

Infrastructure-as-code for the self-hosted deployment. This is a **skeleton** for
Phase 1 — it documents the resources to provision and provides variable stubs.
Concrete provider resources are filled in during Phase 6 (hardening & release),
once the target cloud is chosen.

## Resources to provision
- **App host(s)** — VM(s) running the backend container + reverse proxy (Caddy/nginx, TLS).
- **coturn host** — VM with a **public IP**, UDP `3478` open, and the relay port
  range (`49160-49200`, matching `infra/coturn/turnserver.conf`) open in the firewall.
- **Postgres** — managed instance or a VM with backups.
- **Redis** — managed instance or a VM.
- **Object storage** — S3-compatible bucket (MinIO/cloud) for recordings (later).
- **DNS + TLS** — A/AAAA records for `api.` and `turn.`; ACME certificates.
- **Secrets** — `JWT_SECRET`, `TURN_SECRET`, DB credentials via the cloud secret store.

## Files (to be added)
```
main.tf        providers + module wiring
variables.tf   region, sizes, domain, secrets
network.tf     VPC, subnets, firewall (incl. coturn UDP range)
compute.tf     app + coturn VMs
data.tf        postgres + redis
dns.tf         records + certificates
outputs.tf     api/turn endpoints
```

## Firewall note (critical for media)
coturn must reach clients directly: open **UDP 3478** and the **relay port range**
to the internet on the coturn host, and set `external-ip` to its public address.
Without this, NAT-traversal fallback fails and some sessions cannot connect.
