# network computer

Your phone is the terminal. The computer is somewhere else.

`network-computer` streams a Linux, macOS or (later) Windows desktop to an iPhone with
audio, and sends the phone's keyboard, mouse and touch back. Plug the phone into a TV or
AirPlay it to an Apple TV and it is a desk. Everything is open source, runs as single
static binaries, and works when both ends are behind NAT.

Status: **Phase 0 spike.** The Go side streams a desktop at 1080p60 over WebRTC to a
browser or a headless probe, through NAT via a self-hosted rendezvous with STUN and TURN
relay. The native iOS app does not exist yet. See [docs/PLAN.md](docs/PLAN.md) for the
design and [docs/SPIKE.md](docs/SPIKE.md) for measurements.

![A Linux desktop streamed to a browser over WebRTC](docs/media/desktop.png)

*A cloud Linux desktop streamed to a Mac browser through NAT, browsing this very repo. 1280×720, direct path, ~45 ms, with audio.*

## How it works

```
 phone / browser                                     Linux / Mac / Windows host
 ┌──────────────────┐   WebRTC over UDP, direct       ┌──────────────────────┐
 │ video decode     │◄═══════════════════════════════►│ nc-host              │
 │ audio out        │   when hole punching works      │  ffmpeg capture      │
 │ keyboard, mouse  │                                 │  H.264 hw encode     │
 └────────┬─────────┘                                 │  input injection     │
          │                                           └──────────┬───────────┘
          │  signaling (WebSocket) + STUN + TURN relay fallback   │
          └───────────────┐                       ┌──────────────┘
                          ▼                       ▼
                    ┌──────────────────────────────────┐
                    │ nc-rendezvous  (one binary, one   │
                    │ VPS with a public IP, no database)│
                    └──────────────────────────────────┘
```

Three programs, all in Go:

| Binary | What it does |
|---|---|
| `nc-rendezvous` | Signaling over WebSocket, STUN and TURN on one UDP port, serves the browser client. Runs on any box with a public IP. |
| `nc-host` | Runs on the desktop you want to reach. Captures the screen with ffmpeg, encodes H.264 in hardware, streams over WebRTC, injects input. |
| `nc-probe` | Headless test client. Tells you whether a path is direct or relayed and what frame rate arrives. |

## Requirements

- Go 1.27 or newer to build (go.mod pins it; an older Go downloads the toolchain automatically).
- `ffmpeg` on the host machine (`brew install ffmpeg`, `apt install ffmpeg`).
- On macOS, the terminal running `nc-host` needs Accessibility permission to inject
  input (System Settings, Privacy & Security, Accessibility). Use `-dry-run` to skip that.
- On Linux the host needs X11 (or XWayland) for capture, and write access to `/dev/uinput` for input (root, or a udev rule for the `input` group).

## Quick start (everything on one machine)

```sh
git clone https://github.com/askrobots/network-computer
cd network-computer
go build -o bin/ ./cmd/...

# 1. rendezvous. It prints a generated password; or set one.
NC_PASSWORD=change-me ./bin/nc-rendezvous -http :8765 -turn :3478

# 2. host, in another terminal. It prints a 6-digit PIN; or set one.
NC_PASSWORD=change-me ./bin/nc-host -rendezvous http://127.0.0.1:8765 -name mybox

# 3. open http://127.0.0.1:8765/ in a browser, log in with user "nc" and the password,
#    pick the host, type its PIN, connect. Click the video to grab mouse and keyboard;
#    shift+esc releases.

# or test headlessly:
NC_PASSWORD=change-me NC_PIN=<pin> ./bin/nc-probe -rendezvous http://127.0.0.1:8765
```

## Across the internet

Put `nc-rendezvous` (and, for a cloud desktop, `nc-host`) on any machine with a
public IP. `infra/` scripts this for DigitalOcean (tested), Vultr, Linode and AWS,
or provision any Ubuntu box you already have with `infra/provision-host.sh <ip>`.
See [infra/README.md](infra/README.md). The rest of this section is the manual path.

Put `nc-rendezvous` on any machine with a public IP. Open TCP 8765 (or 443 with TLS) and
UDP 3478, plus UDP 49152 to 65535 for relayed sessions.

```sh
# on the VPS
NC_PASSWORD=change-me ./bin/nc-rendezvous -http :8765 -turn :3478 -public-ip 203.0.113.5

# with a Let's Encrypt certificate instead (listens on 443 and 80):
NC_PASSWORD=change-me ./bin/nc-rendezvous -turn :3478 -public-ip 203.0.113.5 -acme-domain nc.example.com

# on the desktop behind NAT
NC_PASSWORD=change-me ./bin/nc-host -rendezvous https://nc.example.com -name mybox

# from anywhere: does the path go direct or through the relay?
NC_PASSWORD=change-me NC_PIN=<pin> ./bin/nc-probe -rendezvous https://nc.example.com
NC_PASSWORD=change-me NC_PIN=<pin> ./bin/nc-probe -rendezvous https://nc.example.com -relay   # force TURN
```

## Authentication

Two layers, both simple on purpose:

1. **Rendezvous password.** HTTP Basic auth on everything the rendezvous serves, the
   WebSocket included. Set it with `-password` or `NC_PASSWORD`; if unset a random one is
   generated and printed at startup. The username defaults to `nc` (`-user`, `NC_USER`).
2. **Host PIN.** Each host has a 6-digit PIN (`-pin` or `NC_PIN`, generated if unset). A
   client must send it with its connection offer or the host refuses. So two people sharing
   one rendezvous cannot drive each other's machines.

Media is encrypted by WebRTC (DTLS-SRTP). The rendezvous sees signaling and, for relayed
sessions, encrypted packets. TURN credentials are still static in this spike; per-session
credentials and key-based pairing are on the plan.

## Host options worth knowing

```
-size 1920x1080     output resolution (default; "native" disables scaling)
-fps 60             capture rate
-bitrate 8M         video bitrate cap
-encoder            h264_videotoolbox (mac), libx264 (default elsewhere), h264_vaapi, h264_nvenc, ...
-ffmpeg-extra       extra ffmpeg args, e.g. VA-API: -ffmpeg-extra "-vaapi_device /dev/dri/renderD128 -vf format=nv12,hwupload"
-ffmpeg-args        replace the whole ffmpeg command; must end with "-f h264 pipe:1"
-audio-device       stream audio from this device (mac: avfoundation index, needs a loopback such as BlackHole; linux: a pulse source)
-display 1          capture a different screen
-dry-run            log input instead of injecting it
```

## Known limitations of the spike

- One ffmpeg per session; a second client on a macOS host will fail. Shared capture is next.
- No keyframe on demand; packet loss can show artifacts for up to 2 seconds.
- Windows input injection is a stub. macOS (CGEvent) and Linux (uinput) work.
- Audio is opt-in and system audio on macOS needs a loopback device.
- No native iOS app yet. Safari on an iPhone can open the browser client.

## Running it as an agent

A host is a real machine running `nc-host`, so an assistant with shell or input
access can operate it: run commands, install software, and drive the GUI while
reading the screen back. See [docs/AGENT.md](docs/AGENT.md).

## Layout

```
cmd/nc-rendezvous/   signaling, STUN/TURN, embedded web client
cmd/nc-host/         capture, encode, WebRTC, input
cmd/nc-probe/        headless test client
internal/proto/      signaling and input message types
internal/input/      per-OS input injection
docs/                plan, spike results
```

## License

MIT. See [LICENSE](LICENSE).
