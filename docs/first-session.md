# Running your first session (device-to-device)

This is the hands-on path to a real technician↔phone AR session. It assumes the
backend is healthy (it is — CI green) and walks through the two clients, which
have not yet run on hardware.

## Prerequisites
- **Backend host** reachable from both the Windows PC and the phone (a LAN IP or a
  deployed domain). coturn needs a public/routable IP for NAT traversal.
- **Windows 10/11 PC** with the **.NET 8 SDK** (not just the runtime — check with
  `dotnet --list-sdks`; installing Visual Studio's *runtime* components alone
  won't give you `dotnet build`) and the Windows App SDK (Visual Studio 2022 with
  the WinUI workload is easiest). `windows-agent/global.json` pins the build to
  the .NET 8 SDK band specifically — if you also have .NET 9/10 installed, this
  keeps `dotnet build` from picking the newer one, which `Microsoft.WindowsAppSDK`
  1.6.x doesn't recognize as compatible and fails with a
  `Microsoft.Windows.SDK.NET.Ref` version error.
- **A phone**: Android (ARCore-supported) or iOS (ARKit, needs a Mac + Xcode to build).
- **Unity** 2022 LTS or newer with AR Foundation, ARCore/ARKit, and the WebRTC
  package (already pinned in `mobile-client/Packages/manifest.json`).

## 1. Start the backend
```bash
cd infra
cp .env.example .env            # set JWT_SECRET, TURN_SECRET, TURN_URLS (public coturn), etc.
docker compose up --build
```
Confirm from another machine on the network:
```bash
curl http://<backend-host>:8080/healthz     # -> ok
```
Create a technician account (once):
```bash
curl -sX POST http://<backend-host>:8080/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"org_name":"Acme","email":"tech@acme.com","password":"hunter2hunter2","display_name":"Tech"}'
```

## 2. Windows agent
```bash
cd windows-agent
dotnet build src/AR.Agent.App/AR.Agent.App.csproj -c Release -p:Platform=x64
# run the produced AR.Agent.App.exe (or F5 in Visual Studio)
```
In the app: set **Server URL** to `http://<backend-host>:8080`, sign in, click
**Start session**, and note the **ID + PIN**. (You can also set `AR_BACKEND_URL`
to prefill the server field.)

> First real build of the WinUI app — expect to resolve a SIPSorcery API detail or
> two. `AR.Agent.Core` already compiles/tests in CI, so issues will be in the app
> layer or native media (`WindowsMedia.cs`). If the build fails on a
> `Microsoft.Windows.SDK.NET.Ref` version error, you likely have a newer .NET SDK
> (9/10) installed that's shadowing 8.x — confirm `dotnet --list-sdks` shows an
> 8.0.4xx+ entry; `global.json` should already pin to it.

## 3. Mobile client — assemble the scene

**Fast path (recommended):** create the AR rig from the menu, then let the Editor
tool wire the rest:
1. **GameObject ▸ XR ▸ XR Origin (AR)** and **GameObject ▸ XR ▸ AR Session**.
2. **AR App ▸ Wire Session Scene** (added by `Assets/Editor/ARSceneBootstrap.cs`).
   This adds the AR managers, `ARCameraStreamer`, a marker prefab, the token-entry
   UI (`TokenEntryUI`), the 2D overlay, and the controller graph — all references
   wired. Then set the **backend URL** on the `Session` object and build.

The tool is a scaffold; review the result in the Inspector and adjust visuals. The
manual equivalent is below if you prefer to build it by hand.

### Manual scene assembly
In Unity, open `mobile-client` and build a scene with:

1. **AR rig** — GameObject menu → XR → **XR Origin (AR)**. This creates an XR Origin
   with an AR Camera. On the AR Camera, confirm `ARCameraManager` and
   `ARCameraBackground`, and add **`ARCameraStreamer`**.
2. **AR Session** — add an **AR Session** GameObject (XR → AR Session).
3. **Managers** — on the XR Origin add `ARRaycastManager`, `ARAnchorManager`, and
   `ARPlaneManager` (planes help anchors land).
4. **Marker prefab** — a small 3D object (e.g. a sphere or an arrow model) with a
   `Renderer`; save it as a prefab. This is what sticks to the real world.
5. **2D overlay** — a screen-space **Canvas**; add **`Annotation2DOverlay`** to it,
   with a small UI `Image` prefab as its marker (fallback before anchoring).
6. **Controllers** — an empty GameObject with **`AnnotationAnchorManager`** (assign
   the raycast/anchor managers, the marker prefab, and the overlay) and
   **`PhoneSession`**.
7. **Session controller** — an empty GameObject with **`SessionController`**; assign:
   the `ARCameraStreamer`, a microphone `AudioSource`, a playback `AudioSource`,
   `PhoneSession`, and `AnnotationAnchorManager`. Set the **backend URL** (the WS
   URL is derived).
8. **Token entry UI** — a Canvas with two input fields (ID, PIN) and a Connect
   button whose `onClick` calls `SessionController.Connect(id, pin)`.

Player settings:
- **Android**: XR Plug-in Management → enable **ARCore**; set minimum API level per
  ARCore; enable camera + microphone permissions.
- **iOS**: enable **ARKit**; add camera and microphone usage descriptions.

Build to the device (Android: Build and Run; iOS: build the Xcode project, sign, run).

## 4. Run the session
1. Agent: **Start session** → read out the ID + PIN.
2. Phone: enter ID + PIN → **Connect**.
3. Expect, in order:
   - connection reaches `connected` (both sides);
   - the phone's **camera appears** in the agent's panel; **two-way audio** works;
   - the agent **clicks the video** → a marker appears on the phone (2D first, then
     it **anchors** to the real object and stays put as the phone moves);
   - the agent sees the echoed `anchorId` in its status line.

## 5. If media doesn't flow
- **Connects but no video/audio**: coturn unreachable. Verify its public IP, that
  UDP 3478 and the relay port range are open, and that `TURN_URLS`/`TURN_SECRET`
  match between the backend and `coturn/turnserver.conf`.
- **Force-test relay**: block direct UDP between the peers; the session should still
  connect via coturn.
- **Codec mismatch / no decode**: both sides must use **VP8 + Opus** (already locked
  in the harness-verified config). Check the agent's `SessionConnection` and the
  Unity peer both offer/accept these.

For the SIPSorcery ⇄ Unity connection specifically — the highest-risk step — work
through **`docs/handshake-troubleshooting.md`**, a top-down checklist for the first
real device connection. See also `docs/verification.md` for the per-layer checklist
and `docs/runbook.md` for operations.
