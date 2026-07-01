# AR Remote Support Ecosystem

A "TeamViewer for AR field support". A Windows technician app generates a short-lived
connection token; an end-user enters it in a mobile app to start a session. The phone
streams its live camera feed over WebRTC, and the technician draws annotations that
**anchor to the real world** in the user's camera view to guide them through
troubleshooting. Two-way audio runs throughout.

## Repository layout

| Path | Contents |
|------|----------|
| `backend/` | Go backend: identity, session/token, and signaling services (modular monolith) |
| `windows-agent/` | .NET 8 + WinUI 3 technician app (SIPSorcery WebRTC) — *later phase* |
| `mobile-client/` | Unity + AR Foundation client (`com.unity.webrtc`) — *later phase* |
| `shared/` | Signaling + `AnnotationEvent` message schemas (single source of truth) |
| `infra/` | Docker, docker-compose, coturn config, Terraform, k8s/helm |
| `docs/` | Architecture, API, runbooks |

## Stack decisions

| Area | Choice |
|------|--------|
| AR depth (v1) | True world-anchored annotations (ARCore/ARKit spatial anchors) |
| Mobile | Unity + AR Foundation, `com.unity.webrtc` |
| Hosting / media | Self-hosted: coturn, custom signaling, Postgres/Redis, Terraform |
| Windows agent | .NET 8 + WinUI 3, WebRTC via SIPSorcery |
| Backend | Go (modular monolith) |
| Transport | WebRTC (DTLS-SRTP media + data channel for annotations), WSS signaling |

## Delivery phases

1. ✅ **Foundations** — backend (identity + session/token), Postgres/Redis,
   docker-compose, CI. Login + mint/redeem token.
2. ✅ **Connectivity** — signaling relay + WebRTC handshake proven (VP8/Opus +
   data channel) via the headless interop harness; agent core (.NET/SIPSorcery) and
   Unity client scripts in place. On-device SIPSorcery⇄libwebrtc check pending.
3. ✅ **Live video + audio** — agent renders the phone camera (VP8 decode) and runs
   two-way Opus audio; Unity streams the AR feed + mic. Bidirectional RTP flow
   proven by the media interop harness.
4. ✅ **Annotation data channel + 2D render** — agent toolbar → normalized (u,v) →
   data channel; phone 2D telestration overlay. Wire contract enforced by the
   schema-conformance test.
5. ✅ **True world anchoring** — phone raycasts (u,v) → persistent `ARAnchor`, echoes
   the anchor id back; letterbox-correct coordinate mapping (unit-tested).
6. ✅ **Hardening & release** — signed signaling join tokens, IP rate limiting,
   Prometheus metrics + readiness, Caddy TLS prod compose, GHCR/MSIX release
   pipeline. Session recordings + on-device builds are the remaining follow-ups.

Guides: **`docs/first-session.md`** (device-to-device session — Windows agent +
Unity scene assembly), **`docs/deploy-proxmox.md`** (backend on a Proxmox VM/LXC,
incl. coturn NAT), `docs/handshake-troubleshooting.md` (SIPSorcery ⇄ Unity),
`docs/runbook.md` (operations), and `docs/verification.md` (how each layer is
tested). The remaining device-only check is SIPSorcery ⇄ libwebrtc on real Windows
+ phone hardware.

## Quick start (Phase 1)

```bash
cd infra
docker compose up --build      # starts Postgres, Redis, coturn, and the backend
# backend listens on http://localhost:8080
curl http://localhost:8080/healthz
```

See `docs/api.md` for the auth + token endpoints and `backend/README.md` for local dev.
