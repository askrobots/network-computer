---
# DBBASIC.md — publish manifest for dbbasic.com.
#
# Convention: one DBBASIC.md at the root of any project directory. The dbbasic.com
# agent discovers them across the disk (e.g. `find / -name DBBASIC.md` or a locate
# index) and publishes each project to dbbasic.com from the fields below. Copy this
# file into any project and edit the values; the format is versioned by `version`.
#
# The agent should treat `ready: false` or `visibility: draft` as "do not publish
# yet". Paths under `media` and `content_file` are relative to this file.
dbbasic:
  version: 1
  ready: true                         # false = agent skips publishing
  slug: network-computer              # url slug on dbbasic.com
  title: Network Computer
  tagline: Your phone is the terminal.
  status: alpha                       # idea | alpha | beta | live
  visibility: public                  # public | unlisted | draft
  updated: 2026-09-21

  publish:
    - type: project                   # project | page | post | link
      path: /projects/network-computer

  summary: >
    Stream a Linux or Mac desktop to a phone, tablet, or browser over WebRTC —
    with audio, keyboard, mouse and touch — even when both ends are behind NAT.
    Open source, single Go binaries, and one Flutter client for every device.

  tags: [webrtc, remote-desktop, nat-traversal, go, flutter, pion, open-source, self-hosted]

  repos:
    - name: Host, rendezvous, browser client (Go)
      url: https://github.com/askrobots/network-computer
    - name: Flutter client (Android / iOS / iPad / desktop)
      url: https://github.com/askrobots/network-computer-flutter

  links:
    - label: Source
      url: https://github.com/askrobots/network-computer
    - label: Roadmap
      url: https://github.com/askrobots/network-computer/blob/main/docs/ROADMAP.md

  media:
    - path: docs/media/desktop.png
      alt: A Linux desktop streamed to a browser over WebRTC, browsing its own repo
      role: hero                      # hero | screenshot | icon

  # Optional: how to build and test this project on Linux/Windows (used by a
  # build box + scripts/build-linux.sh; falls back to per-type defaults).
  build:
    linux: null                       # e.g. "flutter build linux --release"
    windows: null                     # e.g. "flutter build windows --release"
    macos: null
  test:
    linux: null                       # e.g. "xvfb-run -a flutter test"

  # Optional: built artifacts to publish as downloads on dbbasic.com. A build sweep
  # fills these in (path relative to this file, or a URL once uploaded). The
  # dbbasic.com agent uploads/links them per platform.
  downloads:
    # - platform: macos                # macos | linux | windows | android | ios
    #   version: 0.1.0
    #   path: build/macos/Build/Products/Release/App.app   # or a .dmg/.zip
    # - platform: linux
    #   path: build/linux/x64/release/bundle
    # - platform: windows
    #   path: build/windows/x64/runner/Release

  # Optional: a markdown file whose body is the published page content. If absent,
  # the body of this DBBASIC.md (below the frontmatter) is used.
  content_file: null
---

# Network Computer

**Your phone is the terminal. The computer is somewhere else.**

Network Computer streams a Linux or Mac desktop to an iPhone, iPad, Android device,
or any browser, and sends keyboard, mouse and touch back — with the desktop's audio
playing on the device. Plug the device into a TV or AirPlay it, and it becomes a
desk. It works when the host has no public IP and the client is behind a strict NAT,
by hole-punching through a small self-hosted rendezvous and falling back to a relay
only when it must.

## Why it's different

- **Runs anywhere, no containers.** Three small Go binaries: a rendezvous, a host,
  and a headless probe. One config file each, systemd/launchd units they install
  themselves.
- **One client for every device.** A single Flutter app targets Android, iPhone,
  iPad and desktop; a browser client covers everything else with nothing to install.
- **Real NAT traversal.** WebRTC with a self-hosted STUN/TURN rendezvous. Measured
  ~42 ms and 60 fps direct across the internet; automatic relay fallback.
- **Open source, end to end.** MIT. Nothing proprietary in the path.
- **An AI can drive it.** The same host is a machine an assistant can operate over a
  shell and the live GUI — see the project's agent notes.

## Status

Alpha. The Go stack streams a desktop with audio and input over NAT today; the
Flutter client and provisioning across DigitalOcean, Vultr, Linode and AWS are in
place. Native iPad polish and an AI-with-voice mode are on the roadmap.
