// WebRTC interop harness.
//
// Spins up two peers — an "agent" (offerer, receives video, owns the annotations
// data channel) and a "phone" (answerer, sends a video track) — and drives them
// through the real Go signaling relay. It asserts:
//   1. the signaling handshake completes (peer-ready -> offer/answer/ICE),
//   2. the negotiated SDP uses VP8 video + Opus audio (the locked codecs),
//   3. the peer connections reach the "connected" ICE state,
//   4. an AnnotationEvent sent over the data channel arrives at the phone.
//
// Run:
//   SIGNALING_URL=ws://127.0.0.1:8081 node src/harness.js
//
// This validates the connectivity architecture (signaling + WebRTC + codecs +
// data channel) end-to-end without Windows/Unity/devices. The SIPSorcery and
// com.unity.webrtc stacks must offer/accept the same VP8/Opus to interoperate.

import { randomUUID } from "node:crypto";
import {
  RTCPeerConnection,
  RTCRtpCodecParameters,
  MediaStreamTrack,
} from "werift";
import { SignalingClient } from "./signaling.js";

const SIGNALING_URL = process.env.SIGNALING_URL || "ws://127.0.0.1:8081";
const TIMEOUT_MS = Number(process.env.TIMEOUT_MS || 15000);

// The locked codec set both real clients must also use.
function lockedCodecs() {
  return {
    audio: [
      new RTCRtpCodecParameters({
        mimeType: "audio/opus",
        clockRate: 48000,
        channels: 2,
        payloadType: 96,
      }),
    ],
    video: [
      new RTCRtpCodecParameters({
        mimeType: "video/VP8",
        clockRate: 90000,
        payloadType: 97,
      }),
    ],
  };
}

function newPeer() {
  return new RTCPeerConnection({
    codecs: lockedCodecs(),
    iceServers: [], // host candidates only — sufficient for loopback.
  });
}

// waitFor resolves when predicate() is true, polling until the deadline.
function waitFor(label, predicate) {
  return new Promise((resolve, reject) => {
    const start = Date.now();
    const tick = () => {
      if (predicate()) return resolve();
      if (Date.now() - start > TIMEOUT_MS) return reject(new Error(`timeout waiting for ${label}`));
      setTimeout(tick, 50);
    };
    tick();
  });
}

async function main() {
  const room = randomUUID();
  console.log(`[harness] room=${room} signaling=${SIGNALING_URL}`);

  const agentPc = newPeer();
  const phonePc = newPeer();

  // Agent receives the phone's video, sends/receives audio, owns the data channel.
  agentPc.addTransceiver("video", { direction: "recvonly" });
  agentPc.addTransceiver("audio", { direction: "sendrecv" });
  const dc = agentPc.createDataChannel("annotations");

  // Phone sends a (synthetic) video track and audio.
  const videoTrack = new MediaStreamTrack({ kind: "video" });
  const audioTrack = new MediaStreamTrack({ kind: "audio" });
  phonePc.addTransceiver(videoTrack, { direction: "sendonly" });
  phonePc.addTransceiver(audioTrack, { direction: "sendrecv" });

  // --- result flags we assert on ---
  let phoneGotAnnotation = false;
  let agentGotVideoTrack = false;

  agentPc.onTrack.subscribe((track) => {
    if (track.kind === "video") agentGotVideoTrack = true;
  });

  phonePc.onDataChannel.subscribe((channel) => {
    channel.onMessage.subscribe((data) => {
      const text = typeof data === "string" ? data : data.toString();
      const evt = JSON.parse(text);
      if (evt.op === "create" && evt.kind) phoneGotAnnotation = true;
      console.log(`[phone] received annotation: ${text}`);
    });
  });

  // --- signaling wiring ---
  const agentSig = new SignalingClient(SIGNALING_URL, room, "agent");
  const phoneSig = new SignalingClient(SIGNALING_URL, room, "phone");

  agentPc.onIceCandidate.subscribe(({ candidate }) => {
    if (candidate) agentSig.send("ice", candidate.toJSON());
  });
  phonePc.onIceCandidate.subscribe(({ candidate }) => {
    if (candidate) phoneSig.send("ice", candidate.toJSON());
  });

  agentSig.on("answer", async (env) => {
    await agentPc.setRemoteDescription(env.payload);
  });
  agentSig.on("ice", async (env) => {
    await agentPc.addIceCandidate(env.payload);
  });

  phoneSig.on("offer", async (env) => {
    await phonePc.setRemoteDescription(env.payload);
    const answer = await phonePc.createAnswer();
    await phonePc.setLocalDescription(answer);
    phoneSig.send("answer", phonePc.localDescription);
  });
  phoneSig.on("ice", async (env) => {
    await phonePc.addIceCandidate(env.payload);
  });

  // The agent sends its offer once the relay reports the phone is present.
  agentSig.on("peer-ready", async () => {
    const offer = await agentPc.createOffer();
    await agentPc.setLocalDescription(offer);
    agentSig.send("offer", agentPc.localDescription);
  });

  await phoneSig.connect();
  await agentSig.connect(); // agent joins second -> receives peer-ready -> offers

  // --- assertions ---
  await waitFor("ICE connected", () =>
    agentPc.connectionState === "connected" && phonePc.connectionState === "connected"
  );
  console.log("[harness] ✓ peer connections connected");

  // Codec lock: the negotiated SDP must carry VP8 + Opus.
  const sdp = (agentPc.localDescription?.sdp || "") + (phonePc.localDescription?.sdp || "");
  if (!/VP8/i.test(sdp)) throw new Error("VP8 not present in negotiated SDP");
  if (!/opus/i.test(sdp)) throw new Error("Opus not present in negotiated SDP");
  console.log("[harness] ✓ negotiated codecs include VP8 + Opus");

  // Data channel: agent -> phone AnnotationEvent.
  await waitFor("data channel open", () => dc.readyState === "open");
  const annotation = {
    op: "create",
    id: randomUUID(),
    kind: "arrow",
    point: { u: 0.5, v: 0.42 },
    color: "#ff3b30",
  };
  dc.send(JSON.stringify(annotation));
  console.log("[agent] sent annotation over data channel");

  await waitFor("phone received annotation", () => phoneGotAnnotation);
  console.log("[harness] ✓ annotation delivered over data channel");

  if (!agentGotVideoTrack) {
    console.warn("[harness] note: agent did not observe an incoming video track event");
  } else {
    console.log("[harness] ✓ agent received phone video track");
  }

  agentSig.close();
  phoneSig.close();
  await agentPc.close();
  await phonePc.close();

  console.log("\n[harness] PASS — signaling + WebRTC handshake + codecs + data channel verified");
  process.exit(0);
}

main().catch((err) => {
  console.error("\n[harness] FAIL:", err.message);
  process.exit(1);
});
