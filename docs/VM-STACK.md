# One VM, everything: voice AI, the dbbasic apps, and the object server

Plan, 2026-09-22. Where the network computer goes next: a VM you talk to, running
the dbbasic apps on its desktop and the object server as its brain.

## Where we are

`https://nc.dbbasic.com` streams a desktop with audio out, microphone in (phone app;
browsers and Safari now that it has a real certificate), keyboard/mouse/touch, token
auth, pairing, and per-session relay credentials. `provision/verify.sh` checks 34
things on every provision. So the box can already hear you and speak to you; nothing
on it listens or answers yet.

## 1. One TLS front door (Caddy)

Today the rendezvous owns ports 80/443 for its Let's Encrypt cert. The object server's
own install path puts **Caddy** (Apache-2.0) on 80/443 in front of `127.0.0.1:8001`.
Both can't hold the ports, and more apps are coming, so Caddy becomes the one front door:

| Hostname | Caddy proxies to | What |
|---|---|---|
| `nc.dbbasic.com` | `127.0.0.1:8765` (WebSockets too) | rendezvous + web client |
| `objects.<test-domain>` | `127.0.0.1:8001` | object server |
| later: one per app | its loopback port | other services |

The rendezvous then listens on loopback only and needs a small `-behind-proxy` flag so
`/config` reports `secure` (trusting `X-Forwarded-Proto` from 127.0.0.1 only).
STUN/TURN stays on 3478/udp directly; Caddy doesn't need to see it.

Careful: `dbbasic.com` itself is served in production by an object server elsewhere.
The test VM must use a separate hostname so nothing here touches the live site.

## 2. The object server (optional profile)

`askrobots/dbbasic-object-server`: stdlib Python + uvicorn, live versioned objects,
an app suite (projects, notes, tasks, contacts, articles, links, calendar, files, a
shell you can talk to), one permission engine across web/API/search/files/MCP/AI.
Its `scripts/install.sh` is idempotent and makes a systemd service on
`127.0.0.1:8001` with an admin token in `/etc/dbbasic-object-server.env`.

- `provision/profiles/object-server.sh`: clone, run its installer, add the Caddy site.
- Profiles are opt-in: `NC_PROFILES=object-server,apps` in `/etc/nc/env`, recorded
  like `NC_HOST_NAME` so a re-provision keeps them.
- `verify.sh` gains `/health` = 200 and the Caddy route, only when the profile is on.

## 3. The dbbasic apps on the desktop

Inventory of `/Volumes/T9` (2026-09-22):

| Kind | Projects | On the VM as |
|---|---|---|
| Flutter desktop apps | dbbasicshell (terminal), dove (email), writer-native, spreadsheet-native, slides-native, draw, slate, scroll, webmaster, porter | Linux bundles in `/opt/dbbasic/<app>` + menu/panel launchers |
| Mobile-only | tape (recorder) | skip on desktop |
| Hub | object server | service behind Caddy (profile) |
| Python services / tools | content, forms, passwd, census, cli, video, website, studio, tsv (library), dbbasic | services where they serve, libraries where they don't |
| Node | dbbasic-browser (x402) | desktop app once packaged |
| Unclear / empty | 1brc, calc, lens, perf, player | check before planning |

Delivery reuses the build box (`docs/BUILDBOX.md`): build Linux bundles, then
`provision/profiles/apps.sh` unpacks them and writes `.desktop` entries. Each project's
`DBBASIC.md` gains `install:` and `desktop:` next to its existing `build:`, so one sweep
builds, installs on the VM, and publishes to dbbasic.com.

## 4. Talking back and forth with an AI

The audio plumbing exists; this adds a listener and a voice:

```
phone mic → nc-mic-in (done) → end-of-turn → speech-to-text → AI → text-to-speech → nc sink → phone (done)
```

Open-source parts: **whisper.cpp** (MIT) for speech-to-text, **Piper** (MIT) for the
voice, **Silero VAD** (MIT) for end-of-turn later. The AI is either the object server's
AI shell (so it can act on your notes/tasks through the same permission engine) or a
Claude API key held on the VM, never on the client.

Phases, each shippable:
1. **Push-to-talk.** Hold the mic button; release ends the turn. No detection errors.
   Transcript and reply appear in a desktop window as well as out loud.
2. **Hands-free.** VAD decides end-of-turn. The phone's echo cancellation already keeps
   the AI's own voice out of the microphone.
3. **Barge-in.** Speaking stops the AI mid-sentence.
4. **Actions.** It can do things: object server data over its API/MCP, and the desktop
   itself (shell + GUI, `docs/AGENT.md`), with a spoken confirmation before anything
   destructive.

Latency target: under ~1.5 s from end of speech to first sound. Rough budget: speech-to-
text 0.3–0.7 s (whisper `small.en` on 4 vCPU), first AI tokens 0.3–0.8 s, first spoken
sentence 0.1–0.2 s (Piper, streamed per sentence), network ~0.05 s.

Safety: push-to-talk first, the mic button is already red while live, the AI runs as its
own user with least privilege, and its key lives in the VM's secret store.

## 5. Working together

- The object server is the shared memory: notes, tasks, contacts, files, permissions.
- Desktop apps read and write it over its HTTP API (dove turns mail into tasks, writer
  saves articles, draw saves files).
- The AI sees both sides: data through MCP, the screen through the network computer.
- `DBBASIC.md` in every project ties build, install, desktop entry and publishing together.

## Sizing

Desktop + object server + whisper wants more than today's 2 vCPU / 4 GB:
`s-4vcpu-8gb` (~$0.07/hr, ~$48/month). Speech-to-text can move to a GPU box later.

## Order of work

1. Caddy front door; rendezvous behind it (`-behind-proxy`). verify.sh stays green.
2. Object server profile on its own test hostname; health check in verify.sh.
3. Push-to-talk voice loop with a transcript window.
4. Linux builds of three flagship apps (dbbasicshell, writer, dove) as desktop apps.
5. Hands-free and barge-in; AI actions with confirmations.
6. The remaining apps; `install:`/`desktop:` in DBBASIC.md; one sweep for everything.

## Decisions needed

- Test hostname for the object server (must not be `dbbasic.com` itself).
- The AI: object server's AI shell, a Claude API key, or both.
- Whether to move the test VM to 4 vCPU / 8 GB before the voice work.
