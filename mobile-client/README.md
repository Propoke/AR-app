# Mobile Client (end-user app)

Unity + AR Foundation application for Android (ARCore) and iOS (ARKit).

## Status (Phase 2)
Core scripts implemented (not yet wired into a Unity scene / built — needs the
Unity Editor):
```
Assets/Scripts/
  Protocol/Messages.cs              signaling + AnnotationEvent + JoinInfo (mirror shared/*.json)
  Signaling/SignalingClient.cs      signaling WebSocket (phone role)
  WebRtc/PhoneSession.cs            Unity.WebRTC: send AR camera (VP8) + audio (Opus), answer offer
  AR/AnnotationAnchorManager.cs     raycast (u,v) → persistent ARAnchor (the world-anchoring core)
  SessionController.cs              redeem token → signaling → WebRTC → AR
Packages/manifest.json              com.unity.webrtc + AR Foundation + ARCore/ARKit + Newtonsoft
```
Remaining for an on-device build: a Unity scene with AR session origin, the
marker prefab, and the token-entry UI; plus blitting the AR camera background into
the streamed RenderTexture. See `docs/verification.md`.

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
