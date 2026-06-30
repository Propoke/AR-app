# Windows Agent (technician app)

> **Status: placeholder — implemented in Phase 2+.**

.NET 8 + WinUI 3 desktop application for the support technician.

## Planned responsibilities
- Login / token refresh against the backend (`/v1/auth/*`).
- Mint a connection token (`POST /v1/sessions`) and display `connect_id` + `pin`
  for the technician to relay to the end-user.
- Connect to the signaling WebSocket (`role=agent`), exchange SDP/ICE.
- WebRTC via **SIPSorcery**: receive the phone's camera (VP8), two-way Opus audio.
- Annotation toolbar (arrow/circle/text/freehand) → send `AnnotationEvent`s over
  the WebRTC data channel (`shared/annotation.schema.json`).
- Session history.

## Interop constraint
Lock media to **VP8 video + Opus audio** to interoperate with the Unity client's
libwebrtc stack. Validate SDP negotiation early (Phase 2) — this is the top risk.

## Packaging
Signed **MSIX** installer with auto-update (Phase 6).
