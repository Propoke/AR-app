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

1. **Foundations** *(current)* — backend skeleton (identity + session/token), Postgres/Redis,
   docker-compose, CI. Login + mint/redeem token (no media yet).
2. Connectivity — signaling + coturn; prove a WebRTC session connects (VP8/Opus).
3. Live video + audio.
4. Annotation data channel + 2D render.
5. True world anchoring.
6. Hardening & release.

## Quick start (Phase 1)

```bash
cd infra
docker compose up --build      # starts Postgres, Redis, coturn, and the backend
# backend listens on http://localhost:8080
curl http://localhost:8080/healthz
```

See `docs/api.md` for the auth + token endpoints and `backend/README.md` for local dev.
