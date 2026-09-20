# dbbasic network computer — plan

Date: 2026-09-19 (revised: no Docker, no commercial software, we own the middle layers)

## The idea

The iPhone is the terminal. The computer is somewhere else (Linux, Mac, later Windows).
The phone shows the remote screen, plays its audio, and sends keyboard, mouse, and touch
input back. Plug the phone into a TV or USB-C hub and it becomes a desk.

Constraints:
- Open source only, end to end. No commercial services, no Docker.
- "Easy to run anywhere": a single static binary per role, no runtime dependencies beyond
  what a package manager installs in one command.
- Host is behind NAT with no public IP. Phone may be behind strict (symmetric) NAT.
- iOS app built with our own Xcode account.

## Architecture

Three programs, all ours, plus a rendezvous point on one small VPS.

```
 iPhone (USB-C hub / TV)                                 Linux / Mac / Windows host
 ┌───────────────────────┐                                ┌───────────────────────┐
 │ ios app (Swift)       │                                │ nc-host (Go binary)   │
 │  - libwebrtc          │   WebRTC over UDP, direct      │  - Pion WebRTC        │
 │  - VideoToolbox decode│◄══════════════════════════════►│  - capture + encode   │
 │  - audio out          │   when hole punching works     │    (ffmpeg subprocess │
 │  - kbd/mouse/touch    │                                │     or native shim)   │
 │  - external display   │                                │  - input injection    │
 └───────────┬───────────┘                                └───────────┬───────────┘
             │                                                        │
             │   signaling (WebSocket) + STUN + TURN relay fallback   │
             └────────────────────────┐   ┌─────────────────────────┘
                                      ▼   ▼
                            ┌──────────────────────┐
                            │ nc-rendezvous        │  one Go binary on a VPS
                            │  (Go binary)         │  with a public IP.
                            │  - host registry     │  systemd unit, one config
                            │  - signaling         │  file, SQLite or flat file.
                            │  - STUN + TURN       │
                            └──────────────────────┘
```

**Why WebRTC instead of a VPN mesh plus Sunshine/Moonlight.** NetBird or Headscale means
several services, a database, and a separate VPN app on the phone. Sunshine is a large
C++ build with per-platform quirks. WebRTC gives us NAT traversal, encryption, congestion
control, jitter buffering, and audio/video/data channels in one library that is BSD
licensed on both ends. Pion (Go) on the host and rendezvous, Google's libwebrtc on iOS.
We write the capture, input, pairing, and UI. That is the part we want to own anyway.

**Why Go.** One codebase cross-compiles to linux/amd64, linux/arm64, darwin, windows with
no runtime. Pion, pion/turn, pion/ice, quic, uinput, and Windows syscalls are all pure Go.
cgo only where the OS forces it (macOS screen capture and event injection).

## The three programs

### nc-rendezvous (VPS, Go)
- Serves WebSocket signaling: host registers under its public key; phone asks for a host
  by key; both exchange SDP and ICE candidates through it.
- STUN server (pion/stun) so both sides learn their public address.
- TURN server (pion/turn) as relay of last resort when both ends are symmetric NAT.
  Credentials are short lived and minted per session so nobody else can relay through it.
- State is a single SQLite file or a JSON file. Config is one TOML file. Runs as a systemd
  service from a single static binary. No reverse proxy required: it terminates TLS itself
  with a Let's Encrypt cert (autocert) or a self-signed cert pinned in the QR code.
- Can also run on the host itself if the host ever gets a public IP; the phone does not care.

### nc-host (Linux / Mac / Windows, Go)
- Registers with the rendezvous, answers WebRTC offers, streams one video track, one audio
  track, and one data channel for input and control.
- **Capture and encode, v1:** spawn `ffmpeg` and read Annex-B H.264/HEVC from its stdout,
  packetize into RTP with Pion's packetizers. One extra install (`apt install ffmpeg`,
  `brew install ffmpeg`, winget) buys us every capture backend and every hardware encoder:
  - Linux: `x11grab` (+ `h264_vaapi`, `hevc_vaapi`, `h264_nvenc`, or `libx264 -tune zerolatency`)
  - macOS: `avfoundation` screen capture + `hevc_videotoolbox -realtime 1`
  - Windows: `gdigrab` or `ddagrab` + `h264_nvenc`, `hevc_amf`, `h264_qsv`
  - Audio: `pulse`/`pipewire` monitor, `avfoundation`, `dshow` → Opus.
- **Capture and encode, v2:** replace ffmpeg per platform with a native shim once latency
  or cursor handling demands it: ScreenCaptureKit + VideoToolbox on Mac (cgo), PipeWire on
  Wayland, Desktop Duplication on Windows. Sunshine's source is the reference for all three.
- **Input injection:** Linux `uinput` (pure Go, works under X11 and Wayland); Windows
  `SendInput` (syscall, no cgo); macOS `CGEvent` (small cgo shim). Keyboard sends key codes,
  mouse sends relative deltas and absolute touch positions, scroll, buttons.
- **Headless mode:** for a box with no monitor, start a virtual display (Xorg with a dummy
  driver, or a headless Wayland compositor such as sway or cage) so the phone is the only
  screen. This is the natural "network computer" configuration.
- Pairing: on first run prints a QR code (host public key, rendezvous URL, one-time token).
- Install: one binary + one config file + a systemd / launchd / Windows service unit that
  the binary writes itself (`nc-host install`).

### ios app (Swift)
- libwebrtc via a SwiftPM package built from Google's BSD source (the stasel/WebRTC build).
  Hardware H.264/HEVC decode, Opus playout, jitter buffer, NACK/FEC all come with it.
- Signaling over URLSession WebSocket. Pairing by scanning the host's QR code with the
  camera. DTLS fingerprints are checked against the paired host key, so the rendezvous
  cannot man-in-the-middle a session even if compromised.
- **External display:** iPhone 15 and later output DisplayPort over USB-C. The app opens a
  separate window scene on the external screen at native resolution; the phone screen
  becomes a trackpad, keyboard, and control strip. Lightning phones only mirror at 1080p.
- **Hardware keyboard and mouse** from the hub via GameController (GCKeyboard, GCMouse with
  relative deltas). Verify GCMouse delivers on iPhone, not only iPad.
- **Phone target is iPhone 12 class.** Hardware H.264 and HEVC decode, 2532 x 1170 screen, Lightning port. The stream defaults to 1080p60 and never exceeds it. TV output from that phone is AirPlay to an Apple TV, which adds roughly 100 ms; the app drives the Apple TV as a second UIWindowScene the same way it would a wired display.
- **Codecs:** H.264 first (spike), HEVC next; AV1 only on A17 Pro and later.
- iOS suspends apps in the background, so design for a sub-second reconnect.
- Xcode: a free account sideloads for 7 days at a time; a paid one is needed for
  TestFlight. No special entitlements needed for this design (no VPN extension).

## Protocol (docs/protocol.md, to write)
- Signaling messages: JSON over WebSocket, versioned. register, list, offer, answer, ice.
- Data channel: small binary messages for input (key, mouse move, button, scroll, touch),
  clipboard, and control (resolution, bitrate, codec, display selection).
- Identity: Ed25519 keypair per host and per phone; everything signed; the rendezvous
  only routes.

## Phases

### Phase 0: spike (a few days)
- nc-rendezvous with signaling + STUN + TURN, running on a VPS as a systemd unit.
- nc-host on Linux streaming an X11 desktop through ffmpeg + Pion to a test page or a
  minimal iOS app. Measure latency and bitrate. Confirm hole punching from cellular and
  from a symmetric NAT, and that TURN fallback works.

### Phase 1: usable on Linux
- Input injection via uinput, audio track, pairing by QR, config and service install.
- iOS app: pairing, stream view, on-screen trackpad and keyboard, hardware kbd/mouse.

### Phase 2: TV and Mac
- External display mode on iPhone. Clipboard sync. Bitrate and resolution controls.
- nc-host on macOS: ffmpeg avfoundation first, then ScreenCaptureKit shim.

### Phase 3: Windows and native capture
- nc-host on Windows (ddagrab + SendInput). Native capture shims where ffmpeg latency or
  cursor handling is not good enough. Wayland via PipeWire.

### Phase 4 (optional): more than a screen
- Reuse the punched UDP path to carry a WireGuard tunnel (wireguard-go) so the phone can
  also reach ssh, files, and other services on the host. Same rendezvous, no new server.

## Open questions to settle in Phase 0
- ffmpeg pipeline latency at 1080p60 with hardware encoders; is it under 20 ms?
- Does GCMouse give relative motion on iPhone with a USB mouse through a hub?
- Real hole-punching success rate; how much bandwidth the TURN relay costs when used.
- Does libwebrtc on iOS render smoothly to an external UIWindowScene at 4K?

## Repo layout

```
docs/          plan, protocol, decisions, measurements
rendezvous/    nc-rendezvous (Go)
host/          nc-host (Go), per-OS capture and input packages, service install
ios/           Swift app
shared/        protocol message definitions shared by host and rendezvous
```

## Audio, microphone, camera, files (added 2026-09-20)

Everything here is additional tracks and data channels on the one peer connection.
The transport does not change. The work is on the host, where each feature needs a
virtual device so that ordinary desktop apps see the phone's microphone or camera.

```
 phone  ──video (camera)──►  host   virtual camera   (apps see "nc camera")
 phone  ──audio (mic)─────►  host   virtual mic      (apps see "nc mic")
 phone  ◄──audio (system)──  host   loopback capture (phone is the speaker)
 phone  ◄──video (screen)──  host   screen capture
 phone  ◄─► data: input      keyboard, mouse (absolute and relative), touch, scroll
 phone  ◄─► data: files      chunked transfer both ways, directory listing
 phone  ◄─► data: control    clipboard, resolution, bitrate, device toggles
```

WebRTC gives echo cancellation, noise suppression and A/V sync on the phone for free,
which matters as soon as the phone both hears the desktop and sends its mic.

### Per-OS mechanism and difficulty

| Feature | Linux | macOS | Windows |
|---|---|---|---|
| Desktop audio to phone | PipeWire/Pulse monitor source via ffmpeg. Easy. A null sink makes the phone the only speaker. | ScreenCaptureKit captures system audio natively (needs the native shim, not ffmpeg). Interim: BlackHole loopback driver, open source. | WASAPI loopback, native shim. Medium. |
| Phone mic to desktop | Null sink `nc-mic`, apps use its monitor as mic; fed by ffmpeg pulse output. Easy. | BlackHole as the virtual mic, fed by ffmpeg audiotoolbox output. Medium. | Needs a virtual audio driver. No good open source option; hard. Last. |
| Phone camera to desktop | v4l2loopback kernel module, fed by ffmpeg v4l2 output. Easy. | Camera Extension (CMIOExtension), signed system extension. Medium-hard, needs our Xcode account. | Virtual camera DirectShow filter; OBS's is GPL and reusable. Medium-hard. |
| Files both ways | Data channel, chunked, resumable. Same on all three. Phone side: Files picker and share sheet in the app, drag and drop in the browser. |
| Clipboard | Control channel. Host side: xclip/wl-copy, pbcopy, Windows clipboard API. Easy. |
| Mouse and keyboard | uinput | CGEvent (done) | SendInput |

### Phone-side input model
- Touch as trackpad: relative motion, tap to click, two-finger scroll, so the protocol
  needs a relative move event next to the absolute one.
- Touch as direct: absolute position on the mirrored screen.
- On-screen keyboard plus a modifier bar (ctrl, alt, cmd, esc, arrows).
- Hardware keyboard and mouse through GameController when connected.
- Mic and camera are explicit toggles on the phone with a visible indicator on both ends.

### Order
1. Desktop audio to phone on Linux and macOS (macOS via BlackHole until the native shim).
2. Phone mic to desktop on Linux, then macOS.
3. Files and clipboard.
4. Phone camera to desktop on Linux, then the macOS Camera Extension.
5. Windows audio; Windows mic and camera last.

## Client feel (added 2026-09-20 after the first real use)
- **Local cursor echo.** Draw the pointer locally the instant the mouse moves, and have
  the host stop drawing the remote cursor into the video (`-capture_cursor 0`,
  `-draw_mouse 0`). Send the cursor shape and hotspot over the control channel when it
  changes, so the local pointer looks right. This is the standard remote-desktop trick and
  removes the round trip plus encode and decode latency from every mouse movement.
- **Stats overlay** should be collapsible or moved to a corner; it sits over the desktop.
  The user asked for it to stay as is until Firefox and audio testing are finished.
