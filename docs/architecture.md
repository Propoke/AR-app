# Architecture

## Overview

A technician on a **Windows app** mints a connection token; an end-user enters it
in a **mobile app** (Unity + AR Foundation). The phone streams its camera over
**WebRTC**; the technician draws annotations that **anchor to the real world** in
the user's view. Two-way audio runs throughout.

```
 Windows Agent (.NET 8 / WinUI 3 / SIPSorcery)        Mobile Client (Unity / AR Foundation / com.unity.webrtc)
        │  HTTPS (auth, mint)                                   │  HTTPS (redeem)
        │  WSS  (signaling)                                     │  WSS  (signaling)
        └───────────────► Backend (Go modular monolith) ◄───────┘
                          ├ identity   (JWT, RBAC, orgs)
                          ├ session    (token mint/redeem, Redis)
                          └ signaling  (WebSocket relay)
                          Postgres · Redis · coturn (STUN/TURN) · MinIO
        └──────────────── WebRTC media + data channel (P2P, coturn relay fallback) ───────────────┘
```

## Why WebRTC
Peer-to-peer when NAT allows (low latency, low server cost), relayed via coturn
when it doesn't. Media is encrypted end-to-end (DTLS-SRTP). Annotations ride a
WebRTC **data channel**, with the signaling WebSocket as a fallback before the
peer connection is up.

## Connection handshake
1. Agent logs in (`/v1/auth/login`) → access token.
2. Agent mints a token (`POST /v1/sessions`) → `connect_id`, `pin`, `room`, ICE servers.
3. Agent opens the signaling WS (`role=agent`) and waits.
4. User types `connect_id` + `pin`; phone redeems (`POST /v1/sessions/redeem`) →
   `room`, ICE servers.
5. Phone opens the signaling WS (`role=phone`). Relay sends `peer-ready`.
6. Agent sends SDP `offer`; phone replies `answer`; both trickle `ice` candidates.
7. WebRTC connects: phone camera + two-way audio flow; annotations over the data channel.

## Component responsibilities
| Component | Responsibility |
|-----------|----------------|
| identity | Orgs, users, roles (admin/agent/viewer), JWT access+refresh, argon2id passwords |
| session | Mint single-use connect ID + PIN (Redis, TTL), redeem → activate session, ICE creds |
| signaling | In-memory room relay of SDP/ICE/annotation frames (Redis fan-out in Phase 2) |
| coturn | STUN/TURN with REST shared-secret credentials minted per session |
| Postgres | Orgs, users, durable session history, audit log |
| Redis | Ephemeral tokens, attempt counters, (later) signaling presence |

## The hard part: true world-anchored annotations
The agent sees a flat frame; the anchor must live in the phone's 3D scene.
1. Agent clicks → normalized `(u,v)` + frame id + style → `AnnotationEvent` over the data channel.
2. Phone raycasts `(u,v)` into the AR scene (`ARRaycastManager`) to find the 3D hit.
3. Phone creates a persistent `ARAnchor` (`ARAnchorManager`) and attaches the marker prefab.
4. Anchor lifecycle is echoed back so both sides stay in sync.

See `shared/annotation.schema.json` for the event contract.

## Scaling notes (beyond Phase 1)
- Signaling: add Redis pub/sub so any backend instance can relay any room.
- Split the monolith into identity / session / signaling deployables behind the same API.
- coturn horizontally scaled with a shared secret; media never transits the app servers.
