# SIPSorcery ⇄ Unity WebRTC handshake troubleshooting

The interop harness proves the standards-based path (signaling + VP8/Opus + data
channel + media flow) with a third WebRTC stack. The one thing it can't cover is
the exact **SIPSorcery (.NET agent)** ⇄ **`com.unity.webrtc` (Unity phone)**
binaries interoperating. This is the checklist for that first real connection.

Work top-down: each stage must succeed before the next can.

## 0. Reachability
- Both clients must reach the backend's HTTP + WSS. From the phone's browser, load
  `http://<backend>:8080/healthz`.
- coturn must be reachable from **both** peers on UDP 3478 and its relay range, and
  `external-ip` must be its public address. Most "connects then no media" issues
  are coturn/firewall, not code.

## 1. Signaling connects, but nothing happens
Symptoms: both sides log the WebSocket open, no offer/answer.
- Confirm both presented a valid `signaling_token` (agent from mint, phone from
  redeem) for the **same `room`**. A 401 on the WS upgrade means the token is
  wrong/expired/mismatched to the role.
- Confirm the agent receives **`peer-ready`**. The backend sends it to both peers
  once the room has two occupants (fixed: it used to go only to the second joiner).
  The agent sends its offer on `peer-ready`; if it never arrives, the phone likely
  failed to join (check its redeem + WS URL — it must be `ws(s)://`, derived from
  the HTTP URL).

## 2. Offer/answer exchange fails
Symptoms: offer sent, no answer, or `setRemoteDescription` throws.
- **SDP semantics**: `com.unity.webrtc` uses Unified Plan. Ensure SIPSorcery emits
  Unified-Plan SDP (current versions do). A "Plan B / Unified Plan" mismatch shows
  up as m-line/mid errors.
- **Payload of the envelope**: the agent sends `{ "type":"offer", "payload":{ "sdp": "…" } }`.
  The phone reads `payload.sdp`. A double-encoded or bare-string SDP won't parse.
- Log the full SDP on both ends and diff the m-lines.

## 3. Connected, but no video/audio
Symptoms: `connectionState = connected`, black panel or silence.
- **Codec lock**: both sides must offer/accept **VP8 + Opus**. Check the negotiated
  SDP contains `VP8/90000` and `opus/48000/2`. If SIPSorcery negotiated H264 or the
  Unity side only offered VP9, they won't decode each other. Pin VP8/Opus explicitly
  (`SessionConnection` on the agent; the peer config on Unity).
- **Agent decode**: the agent decodes VP8 via `Vp8VideoDecoder` (native VPX in
  `WindowsMedia.cs`). If the panel is black but RTP arrives (`OnRtpPacketReceived`
  fires), the decoder/renderer is the culprit — verify the frame is BGRA and the
  `WriteableBitmap` dimensions match.
- **Unity camera track**: the phone streams the AR background via a `RenderTexture`
  (`ARCameraStreamer` blits `ARCameraBackground`). If the agent sees a frozen/black
  frame, confirm the blit runs each frame and the `VideoStreamTrack` was created
  from that texture.
- **Audio**: the Unity mic uses `Microphone.Start` into a looping `AudioSource`
  feeding an `AudioStreamTrack`; the agent sends Opus via `SendAudio` and plays via
  the Windows audio sink. One-way audio usually means one side's device wasn't
  started or permissions were denied.

## 4. Media only works on the same network (fails across NAT)
- This is TURN. Force it locally: block direct UDP between the peers; the session
  must still connect via coturn. If it doesn't, the ICE servers returned by
  mint/redeem are wrong or coturn's credentials/`external-ip` are misconfigured.
- Verify the `username`/`credential` in the ICE server list validate against
  coturn's `static-auth-secret` (they're time-limited HMACs).

## 5. Annotations don't appear / don't anchor
- The data channel is named **`annotations`** and is created by the agent. Confirm
  the phone's `PhoneSession.OnDataChannel` binds a channel with that exact label.
- 2D marker shows but doesn't stick: the raycast found no surface. Move the phone to
  let ARCore/ARKit detect planes; the 2D overlay is the intended fallback until an
  anchor is established.
- Wrong position: verify the agent maps clicks through `VideoCoords` (letterbox-
  aware) so `(u,v)` matches the frame the phone raycasts.

## Fast diagnostics
- Turn on verbose logging on both peers around SDP, ICE candidate types (host /
  srflx / relay), and `connectionState`.
- Reproduce the exact scenario headlessly first: `interop-harness/run.sh` — if that
  passes but devices don't, the difference is in the SIPSorcery/Unity binaries,
  codecs, or the network path, not the protocol.
