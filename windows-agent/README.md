# Windows Agent (technician app)

.NET 8 technician app. The transport/WebRTC logic lives in a cross-platform core
(`AR.Agent.Core`, `net8.0`) that is unit-tested in CI on Linux; the UI is a
Windows-only WinUI 3 shell (`AR.Agent.App`).

## Layout
```
src/AR.Agent.Core/        cross-platform core (builds + tested in CI)
  Protocol/               SignalingEnvelope, AnnotationEvent (mirror shared/*.json)
  BackendClient.cs        HTTP: login, mint session token
  SignalingClient.cs      signaling WebSocket (agent role)
  SessionConnection.cs    SIPSorcery WebRTC: VP8 recv, Opus 2-way, annotations data channel
src/AR.Agent.App/         WinUI 3 shell (Windows-only): sign in, start session, status
tests/AR.Agent.Core.Tests/ xUnit protocol tests
```

## Status (Phase 2)
- ✅ `AR.Agent.Core`: backend client, signaling client, and the SIPSorcery peer
  connection wired for VP8 + Opus with the `annotations` data channel.
- ✅ WinUI shell wires sign-in → mint → signaling → WebRTC negotiation.
- ⏳ Live video rendering + annotation toolbar UI: Phase 3–4.

## Build
```bash
# Core + tests build anywhere with the .NET 8 SDK:
dotnet test tests/AR.Agent.Core.Tests/AR.Agent.Core.Tests.csproj

# The WinUI app builds on Windows (Windows App SDK):
dotnet build src/AR.Agent.App/AR.Agent.App.csproj
```

## Interop constraint
Media is locked to **VP8 video + Opus audio** to interoperate with the Unity
client's libwebrtc stack. The standards-based handshake is verified headlessly by
`interop-harness/`; SIPSorcery ⇄ libwebrtc is confirmed on-device (`docs/verification.md`).

## Packaging
Signed **MSIX** installer with auto-update — Phase 6.
