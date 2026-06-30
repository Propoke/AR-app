# Shared protocol

Single source of truth for the messages exchanged between the Windows agent, the
mobile client, and the backend. The backend's signaling relay treats most payloads
as opaque; these schemas are the contract the **agent** and **phone** must both
implement.

| File | Purpose |
|------|---------|
| `signaling.schema.json` | Envelope for messages over the signaling WebSocket (`/v1/signaling`) |
| `annotation.schema.json` | `AnnotationEvent` — the AR annotation operations drawn by the agent and rendered on the phone |

## Transport summary

```
Agent ──WS /v1/signaling?room&role=agent──┐
                                          ├── backend relay (forwards to counterpart)
Phone ──WS /v1/signaling?room&role=phone──┘

WebRTC peer connection (after SDP/ICE exchange):
  - media:  phone camera (VP8) + two-way audio (Opus)
  - data channel "annotations": AnnotationEvent messages (also relayable over WS
    before the peer connection is established)
```

## Signaling envelope types

| `type` | Direction | Meaning |
|--------|-----------|---------|
| `peer-ready` | server → peer | The counterpart is already in the room; agent may send its offer |
| `offer` | agent → phone | WebRTC SDP offer (`payload` = `{ "sdp": "..." }`) |
| `answer` | phone → agent | WebRTC SDP answer |
| `ice` | both | ICE candidate (`payload` = candidate init object) |
| `annotation` | both | An `AnnotationEvent` (see `annotation.schema.json`) |
| `bye` | both | Peer is leaving / session ending |
| `error` | server → peer | A malformed message was received |

## Codec lock

To keep SIPSorcery (.NET agent) and `com.unity.webrtc` (Unity phone) interoperable,
both sides MUST offer/accept **VP8** video and **Opus** audio. This is enforced by
agreement, not by the relay.
