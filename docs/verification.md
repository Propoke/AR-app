# Verification

How to verify each layer end-to-end. Automated checks run in CI; on-device checks
are manual until device farms are added.

## Automated (CI + local)

| What | How | Verifies |
|------|-----|----------|
| Backend unit + integration | `cd backend && go test -race ./...` | auth crypto, TURN creds, signaling relay handshake |
| Auth + token flow | `cd infra && docker compose up`, then the curl flow in `docs/api.md` | register → login → mint → redeem against real Postgres/Redis |
| **WebRTC interop** | `cd interop-harness && ./run.sh` | signaling handshake, **VP8+Opus** negotiation, **annotations data channel**, video track — two real WebRTC peers through the real relay |
| Agent core | `dotnet test windows-agent/tests/AR.Agent.Core.Tests` | protocol serialization matches the shared schemas |

## On-device (manual, Phase 2 → 5)

The interop harness proves the standards-based path; this confirms the real
SIPSorcery (.NET) ⇄ libwebrtc (Unity) binaries interoperate.

1. Run the backend stack (`docker compose up`) reachable from both machines.
2. Build/run the WinUI agent (`windows-agent/src/AR.Agent.App` on Windows): sign in,
   click **Start session**, note the connect ID + PIN.
3. Build the Unity client to an Android or iOS device, enter the ID + PIN.
4. Confirm:
   - **Phase 2:** peer connection reaches `connected`; logs show VP8/Opus.
   - **Phase 3:** the phone's live camera appears in the agent; two-way audio works.
   - **Phase 4:** an annotation drawn on the agent shows on the phone (2D overlay).
   - **Phase 5:** the annotation **sticks to the real object** as the phone moves,
     and the agent receives the echoed `anchorId`.
5. **Relay fallback:** block direct UDP between the peers and confirm the session
   still connects via coturn.
