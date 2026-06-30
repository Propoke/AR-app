// Media-flow harness (Phase 3).
//
// harness.js proves the handshake, codec lock, and data channel. This goes one
// step further and proves *media actually flows*: it synthesizes RTP on the
// sender tracks and asserts the receiving peer's tracks deliver packets. That is
// the core de-risk for "live video + two-way audio":
//   1. phone video RTP   -> agent receives  (the live camera path)
//   2. phone audio RTP   -> agent receives  (end-user mic -> technician)
//   3. agent audio RTP   -> phone receives  (technician mic -> end-user)
//
// Run: SIGNALING_URL=ws://127.0.0.1:8081 node src/media-harness.js

import { randomUUID } from "node:crypto";
import {
  RTCPeerConnection,
  RTCRtpCodecParameters,
  MediaStreamTrack,
  RtpPacket,
  RtpHeader,
} from "werift";
import { SignalingClient } from "./signaling.js";

const SIGNALING_URL = process.env.SIGNALING_URL || "ws://127.0.0.1:8081";
const TIMEOUT_MS = Number(process.env.TIMEOUT_MS || 15000);
const VIDEO_PT = 97; // VP8
const AUDIO_PT = 96; // Opus

function codecs() {
  return {
    audio: [new RTCRtpCodecParameters({ mimeType: "audio/opus", clockRate: 48000, channels: 2, payloadType: AUDIO_PT })],
    video: [new RTCRtpCodecParameters({ mimeType: "video/VP8", clockRate: 90000, payloadType: VIDEO_PT })],
  };
}

const newPeer = () => new RTCPeerConnection({ codecs: codecs(), iceServers: [] });

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

// Builds a minimal but valid RTP packet with a 100-byte dummy payload.
function rtpPacket(payloadType, seq, timestamp, ssrc) {
  const header = new RtpHeader({
    version: 2,
    payloadType,
    sequenceNumber: seq & 0xffff,
    timestamp: timestamp >>> 0,
    ssrc,
    marker: false,
  });
  return new RtpPacket(header, Buffer.alloc(100, 1));
}

async function main() {
  const room = randomUUID();
  console.log(`[media] room=${room}`);

  const agentPc = newPeer();
  const phonePc = newPeer();

  // Agent: receives phone video, sends+receives audio.
  agentPc.addTransceiver("video", { direction: "recvonly" });
  const agentAudio = new MediaStreamTrack({ kind: "audio" });
  agentPc.addTransceiver(agentAudio, { direction: "sendrecv" });

  // Phone: sends video, sends+receives audio.
  const phoneVideo = new MediaStreamTrack({ kind: "video" });
  const phoneAudio = new MediaStreamTrack({ kind: "audio" });
  phonePc.addTransceiver(phoneVideo, { direction: "sendonly" });
  phonePc.addTransceiver(phoneAudio, { direction: "sendrecv" });

  // Count received RTP per side/kind.
  const recv = { agentVideo: 0, agentAudio: 0, phoneAudio: 0 };
  agentPc.onTrack.subscribe((track) => {
    track.onReceiveRtp.subscribe(() => {
      if (track.kind === "video") recv.agentVideo++;
      else recv.agentAudio++;
    });
  });
  phonePc.onTrack.subscribe((track) => {
    track.onReceiveRtp.subscribe(() => {
      if (track.kind === "audio") recv.phoneAudio++;
    });
  });

  // Signaling (agent offers once the phone is present).
  const agentSig = new SignalingClient(SIGNALING_URL, room, "agent");
  const phoneSig = new SignalingClient(SIGNALING_URL, room, "phone");
  agentPc.onIceCandidate.subscribe(({ candidate }) => candidate && agentSig.send("ice", candidate.toJSON()));
  phonePc.onIceCandidate.subscribe(({ candidate }) => candidate && phoneSig.send("ice", candidate.toJSON()));
  agentSig.on("answer", (env) => agentPc.setRemoteDescription(env.payload));
  agentSig.on("ice", (env) => agentPc.addIceCandidate(env.payload));
  phoneSig.on("offer", async (env) => {
    await phonePc.setRemoteDescription(env.payload);
    const answer = await phonePc.createAnswer();
    await phonePc.setLocalDescription(answer);
    phoneSig.send("answer", phonePc.localDescription);
  });
  phoneSig.on("ice", (env) => phonePc.addIceCandidate(env.payload));
  agentSig.on("peer-ready", async () => {
    const offer = await agentPc.createOffer();
    await agentPc.setLocalDescription(offer);
    agentSig.send("offer", agentPc.localDescription);
  });

  await phoneSig.connect();
  await agentSig.connect();

  await waitFor("ICE connected", () =>
    agentPc.connectionState === "connected" && phonePc.connectionState === "connected"
  );
  console.log("[media] ✓ connected; pumping RTP…");

  // Pump RTP on all three sending tracks.
  let seq = 0, vTs = 0, aTs = 0;
  const ssrc = { v: 11111111, pa: 22222222, aa: 33333333 };
  const pump = setInterval(() => {
    vTs += 3000; aTs += 960; seq++;
    try {
      phoneVideo.writeRtp(rtpPacket(VIDEO_PT, seq, vTs, ssrc.v));
      phoneAudio.writeRtp(rtpPacket(AUDIO_PT, seq, aTs, ssrc.pa));
      agentAudio.writeRtp(rtpPacket(AUDIO_PT, seq, aTs, ssrc.aa));
    } catch { /* tracks may briefly not be ready */ }
  }, 20);

  try {
    await waitFor("agent video RTP", () => recv.agentVideo > 0);
    console.log(`[media] ✓ agent received phone VIDEO (${recv.agentVideo} pkts)`);
    await waitFor("agent audio RTP", () => recv.agentAudio > 0);
    console.log(`[media] ✓ agent received phone AUDIO (${recv.agentAudio} pkts)`);
    await waitFor("phone audio RTP", () => recv.phoneAudio > 0);
    console.log(`[media] ✓ phone received agent AUDIO (${recv.phoneAudio} pkts)`);
  } finally {
    clearInterval(pump);
  }

  agentSig.close();
  phoneSig.close();
  await agentPc.close();
  await phonePc.close();

  console.log("\n[media] PASS — bidirectional media RTP flows through the WebRTC session");
  process.exit(0);
}

main().catch((err) => {
  console.error("\n[media] FAIL:", err.message);
  process.exit(1);
});
