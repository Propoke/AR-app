# WebRTC interop harness

A headless, runnable proof of the connectivity architecture (Phase 2). It drives
**two real WebRTC peers** through the actual Go signaling relay and asserts the
parts that must hold for the Windows agent and Unity client to interoperate:

1. signaling handshake completes (`peer-ready` → `offer`/`answer`/`ice`),
2. negotiated SDP uses the **locked codecs: VP8 video + Opus audio**,
3. peer connections reach the `connected` ICE state,
4. an `AnnotationEvent` sent over the `annotations` **data channel** is delivered,
5. the agent observes the phone's incoming **video track**.

It uses [`werift`](https://github.com/shinyoshiaki/werift-webrtc) (a pure-TypeScript
WebRTC stack) for both peers. This is not the exact SIPSorcery/libwebrtc binaries
the real clients use, but it exercises the same standards-based handshake, codecs,
signaling protocol, and data channel against our real relay — which is what Phase 2
sets out to de-risk. The remaining SIPSorcery ⇄ libwebrtc check happens on real
Windows + device hardware (see `docs/verification.md`).

## Run

```bash
npm install
./run.sh          # builds the DB-free relay, runs the harness, tears down
```

Or against an already-running relay:

```bash
SIGNALING_URL=ws://127.0.0.1:8081 node src/harness.js
```

## Files
| File | Purpose |
|------|---------|
| `src/harness.js` | The two-peer scenario and assertions |
| `src/signaling.js` | Minimal signaling-WS client (same contract as the real clients) |
| `run.sh` | Builds `cmd/signaling-dev`, starts it, runs the harness, cleans up |

The relay under test is `backend/cmd/signaling-dev` — the signaling service with no
Postgres/Redis dependency, so the handshake can be tested in isolation.
