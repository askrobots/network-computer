# Phase 0 spike: results and how to run it

Date: 2026-09-20. Everything below was run on the M1 iMac (macOS 26.6, ffmpeg 9.0.1, Go 1.27).

## What exists

| Binary | Role |
|---|---|
| `nc-rendezvous` | signaling over WebSocket, STUN and TURN on one UDP port, serves the browser test page. One static binary, one process, no database. |
| `nc-host` | registers with the rendezvous, answers WebRTC offers, captures the screen with ffmpeg, streams H.264 over Pion, injects input from the data channel (macOS via CGEvent; other OSes log only, for now). |
| `nc-probe` | headless client. Connects like the phone will, reports the ICE path (direct or relayed) and received frame rate. This is the NAT diagnostic tool. |
| `web/index.html` | browser client with live stats: candidate path, RTT, fps, bitrate, decode and jitter-buffer times. Grabs mouse and keyboard when the video is focused. |

## Results

Local loop on the iMac, host and client on the same machine:

| Measurement | Result |
|---|---|
| Signaling to connected | 18 to 32 ms direct, about 2 s via forced TURN relay |
| First video packet after offer | 0.8 s direct (ffmpeg startup dominates) |
| Frame rate delivered, 1080p60 | 57 to 58 fps, both direct and relayed |
| Keyframe interval | every 2 s as configured |
| Bitrate on a static desktop | about 1 Mbit/s; cap is 8 Mbit/s |
| Input events over data channel | arrive at host, correct types |
| TURN relay fallback | works, same frame rate |

## Things learned that change the design

1. **ffmpeg must run with `-fps_mode passthrough`.** Without it ffmpeg duplicates screen
   frames to a nominal rate and pushed about 180 fps into the encoder. That was the cause of
   the 12 fps first run.
2. **avfoundation grabs the full framebuffer.** The iMac panel is 5120 x 2880 to ffmpeg even
   though the UI is 4480 x 2520. Always scale on the host. Default is now 1920 x 1080.
3. **avfoundation ignores the framerate request** ("Configuration of video device failed,
   falling back to default") but still captures at 60 Hz. Harmless.
4. **Only one avfoundation screen capture can run at a time.** Two ffmpeg captures deadlock
   each other and ignore SIGTERM. The host must run exactly one capture and fan it out to
   sessions, rather than one ffmpeg per session as the spike does. That also fixes the
   keyframe-on-demand problem: one encoder, shared, with a periodic keyframe.
5. **No keyframe on demand.** ffmpeg on a pipe cannot be told to emit an IDR when a client
   sends a PLI. A 2 s GOP bounds recovery but the native encoder shims (VideoToolbox,
   VA-API, NVENC) are the real fix and are the first thing to replace in nc-host.
6. **Port 8080 was taken by nginx on this machine**; the rendezvous default is now 8765.
7. **Basic auth is in**: rendezvous password (HTTP Basic, covers the WebSocket) plus a per-host PIN checked on every offer. Verified: 401 without credentials, offers with a wrong PIN are refused.

## Client target

The phone target is iPhone 12 class: hardware H.264 and HEVC decode, 2532 x 1170 screen,
Lightning port. So the stream defaults to 1080p60 and never exceeds it. TV output from an
iPhone 12 is AirPlay to an Apple TV, which adds roughly 100 ms; the app can drive the
Apple TV as a second screen the same way it would a wired display.

## Run it

```sh
go build -o bin/ ./cmd/...

# 1. rendezvous (on a VPS: add -public-ip <its IP>, optionally -acme-domain <host>). Set NC_PASSWORD or it prints a generated one.
./bin/nc-rendezvous -http :8765 -turn :3478

# 2. host (mac: the process needs Accessibility permission to inject input;
#    use -dry-run to only log input)
NC_PASSWORD=... ./bin/nc-host -rendezvous http://127.0.0.1:8765 -name imac   # prints its PIN

# 3a. headless check of the path and frame rate
NC_PASSWORD=... NC_PIN=... ./bin/nc-probe -rendezvous http://127.0.0.1:8765 -duration 10s
NC_PASSWORD=... NC_PIN=... ./bin/nc-probe -rendezvous http://127.0.0.1:8765 -relay   # force TURN

# 3b. browser: open http://<rendezvous>:8765/ , pick the host, connect.
#     click the video to grab mouse and keyboard, shift+esc releases.
```

Linux host: `-encoder libx264` is the default (x11grab); with a VA-API GPU try
`-encoder h264_vaapi -ffmpeg-extra "-vaapi_device /dev/dri/renderD128 -vf format=nv12,hwupload"`.
Audio: `-audio-device <n>` (mac avfoundation audio index, needs a loopback device such
as BlackHole for system audio; linux: a pulse monitor source).

## Next

- Single shared capture per host, fan out to sessions.
- Native VideoToolbox shim on macOS for keyframe on demand and lower capture latency.
- uinput injector on Linux, SendInput on Windows.
- Pairing keys and per-session TURN credentials in the rendezvous.
- iOS app: libwebrtc, same signaling, AirPlay second screen.

## Internet test, 2026-09-20

Rendezvous and a headless xfce desktop on a DigitalOcean droplet (2 vCPU, no GPU,
software x264 at 720p30), client on the iMac behind home NAT, browser client.

| Measurement | Result |
|---|---|
| ICE path | srflx to host, direct (no relay needed) |
| RTT | 40 ms |
| Video | 1280x720, 30 fps, about 6 Mbit/s |
| Decode | 2.4 ms per frame, jitter buffer 12 ms |
| Forced TURN relay | connects in 2.7 s, same frame rate |
| Input | mouse, clicks, keyboard usable, after the two-device uinput fix |

Verdict from the user: works, and is good. Next: the same test with the iPhone app on
cellular against the iMac host behind home NAT, which is the NAT-to-NAT case.
