# Mobile Client (end-user app)

> **Status: placeholder — implemented in Phase 2+.**

Unity + AR Foundation application for Android (ARCore) and iOS (ARKit).

## Planned responsibilities
- Token entry screen: user types `connect_id` + `pin`.
- Redeem the token (`POST /v1/sessions/redeem`) → `room` + ICE servers.
- Connect to the signaling WebSocket (`role=phone`), exchange SDP/ICE.
- WebRTC via **`com.unity.webrtc`**: stream the AR camera background as a VP8 video
  track, two-way Opus audio.
- Receive `AnnotationEvent`s (`shared/annotation.schema.json`), raycast the
  normalized point into the AR scene (`ARRaycastManager`), and create a persistent
  `ARAnchor` (`ARAnchorManager`) with the marker prefab so annotations stick to
  real objects as the phone moves.

## Interop constraint
Lock media to **VP8 video + Opus audio** to interoperate with the .NET agent's
SIPSorcery stack.

## Distribution
Google Play (Android) and App Store (iOS). Start iOS provisioning/signing early
(Phase 6 dependency).
