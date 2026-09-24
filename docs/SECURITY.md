# Security modes: secure vs insecure, and fixing repeated prompts

Date: 2026-09-22. Design notes, not all implemented yet.

## What is and isn't protected today

- **Media is always encrypted.** Video, audio and input ride WebRTC DTLS-SRTP
  end to end, even when signaling is plain HTTP. This never changes.
- **Signaling is only as protected as its transport.** The WebSocket carries the
  username, password, PIN, and SDP/ICE. Over plain HTTP those are in the clear:
  a passive sniffer on the path can read the password and PIN; an active attacker
  can swap DTLS fingerprints in the SDP (MITM).
- **Auth today:** HTTP Basic (user+password) on everything the rendezvous serves,
  plus a per-host 6-digit PIN checked on the offer. TURN credentials are static.

So: fine on a trusted LAN, not fine across the open internet without TLS.

## Two explicit modes

Make the posture a deliberate choice the client shows, not an accident.

### Insecure mode (LAN / trusted network) — the easy default there
- Plain HTTP/WS. No cert to manage. Lowest friction.
- The client labels the session "insecure (LAN)" so nobody assumes otherwise.
- Appropriate when host, rendezvous and client are on a network you trust.

### Secure mode (internet) — one of two ways to get TLS

**Prefer the domain.** It is the only option under which browsers (Safari especially)
reliably grant the microphone, and it needs no fingerprints. With the domain's DNS on
DigitalOcean it is one command; see [DOMAIN.md](DOMAIN.md).
1. **With a domain (best):** `nc-rendezvous -acme-domain nc.example.com` already
   gets a real Let's Encrypt cert. Clients use `https`/`wss`, no warnings, full
   integrity. This path exists today.
2. **Without a domain (self-contained):** the rendezvous generates a self-signed
   cert and prints its **fingerprint** in the pairing string / QR. Native clients
   (iOS, Flutter) pin that fingerprint and connect over TLS with no CA and no
   warning; a browser shows a one-time "accept certificate" because it can't pin.
   This gives encryption + MITM protection without owning a domain.

A `-mode secure|insecure` (or `-insecure` opt-in) flag sets the posture; the
`/config` response tells clients which mode they're in so the UI can show it.

## Fixing "asked for username/PIN more than once"

Root cause: HTTP Basic auth makes the browser pop its own native login dialog for
the page and the WebSocket, duplicating our own connect form. Fix by dropping
Basic auth for a token the client gets once:

1. Serve the page itself without auth.
2. `POST /auth {user, password}` → a short-lived signed token.
3. Client sends the token on `/config`, `/hosts` (Bearer header) and on the
   WebSocket as `?token=…` (browsers can't set WS headers, a query param works).
4. The connect form is then the *only* place credentials are entered — one prompt.

The **PIN** is entered once and remembered (already stored in localStorage /
app prefs). Next step is a **pairing token**: enter the PIN once per host, the
client stores a signed pairing credential, and never asks again for that host —
the QR-pairing idea from the original plan.

## Recommended end state

- LAN: insecure mode, one prompt, remembered. No cert.
- Internet: secure mode. Domain + ACME if you have one; self-signed + pinned
  fingerprint if you don't. Token auth so it's one prompt. Per-host pairing token
  so the PIN is a one-time step.
- Always: per-session TURN credentials instead of the static ones (minted by the
  rendezvous when it hands out `/config`).

## Order to build
1. ~~Token auth (`/auth` → token; token on WS query param).~~ **Done 2026-09-22.**
   The page is public (no browser dialog), `POST /auth` mints a 12-hour HMAC token,
   and `/config`, `/hosts` and `/ws` accept Bearer / `?token=` / Basic. 401s carry no
   `WWW-Authenticate`, so nothing pops a native login box.
2. ~~Mode flag.~~ **Done.** `-mode auto|secure|insecure`. `/config` and `/auth` report
   the posture and both clients show "insecure" in the session bar.
3. ~~Self-signed + fingerprint pinning.~~ **Done.** `-mode secure` (or `-tls-self`)
   creates an ECDSA P-256 cert in `-state-dir/tls` on first run and reuses it, so the
   fingerprint is stable. `nc-host`, `nc-probe` take `-tls-fingerprint` / `NC_TLS_FP`
   and accept exactly that key (checked on every connection, resumed ones included).
4. ~~Per-host pairing token.~~ **Done.** After a correct PIN the host returns a token
   (HMAC-SHA256, bound to the host name, 90 days) signed with a secret it keeps in
   `-state-dir/pair.key` (0600). Clients store it per rendezvous+host and send it
   instead of the PIN, and every successful connect refreshes it. `-reset-pairings`
   rotates the secret and revokes every pairing. Clients no longer store the PIN.
5. ~~Per-session TURN credentials.~~ **Done.** TURN REST scheme: each `/config`
   response carries a fresh `<expiry>:<id>` username with an HMAC password under a
   secret only the rendezvous holds (`-turn-secret` / `NC_TURN_SECRET`, random if
   unset). Verified by forcing a relayed session.

## Review of the desk (2026-09-23)

A pass over everything a desk now runs, after voice, the computer controller and
the object server were added. For each part: what it can do, who can reach it, what
stands in the way.

### From the internet

| Part | Reachable by | Protection |
|---|---|---|
| Rendezvous (443, 80 for certificates) | anyone | Let's Encrypt TLS; a 16-character random password exchanged for a 12-hour HMAC token; **10 wrong passwords from one address in 10 minutes and that address is refused for the rest of the window** (loopback exempt, so the desk's own host can't be locked out) |
| TURN relay (UDP 3478, 49152-65535) | anyone | relay credentials minted per `/config` request, expire in 12 hours |
| A host | clients that got through the rendezvous | the host PIN once, then a signed pairing token |
| SSH (22) | anyone | keys only (password login off) |
| 8765 | nobody when a domain is set | the firewall closes it; the rendezvous listens there on loopback only |

avahi and cups are disabled (nothing on a desk needs them); `verify.sh` checks that
only the intended ports are public.

### On the desk

The desk user (`dan` here) has no sudo. Everything below runs as that user or as root.

| Part | What it can do | Who can use it | Protection |
|---|---|---|---|
| `/run/nc-host/send.sock` | push files to connected devices; relay voice on/off and voice events | the desk user | root-owned, group = the desk user, mode 0660 (was 0666: any account) |
| Object server (127.0.0.1:8001) | the user's records, files, AI keys, notes, tasks | sessions; the admin token | loopback only; data 0700 as the desk user; admin token root-only (0600) |
| Sign-in helper (localhost:8009) | a signed-in object server session in the desk's browser | anyone with a one-time code | a code (60 s, single use) is needed; codes come only from `/run/nc-voice/helper.sock` (0600, the desk user); the Host header must be localhost (no DNS rebinding); `next` must be a local path. Record search moved to the socket too. Before: any local process could get a session. |
| `nc-voice` | everything the controller can do, driven by an AI | the client's 🎙️ | see below |
| `nc-desk` (controller) | windows, mouse, keys, files in the home folder | the desk user | files only inside home, never overwriting, trash undoable, **hidden files and folders refused** (`~/.ssh`, `~/.config`...) |

### Voice: the AI is not the only gate

- Only the user gives instructions. Text from web pages (`read_page`), files, records
  and screenshots is data; the AI is told so, and to report such text instead of obeying it.
- Three levels: safe actions run; click, drag, type and keys get a second model's review
  (it sees the user's words, the focused window, and exactly what is about to happen);
  trash, opening anything that would run a program (`.desktop`, scripts, executables),
  and whatever the reviewer is unsure of are asked out loud; no answer means no.
- The reviewer is told that typing or pressing Return in a terminal runs commands.
  Tested: typing `rm -rf ~/Documents` into a terminal when the user only asked what was
  in a folder was refused; `ls` when asked for was allowed.
- No blind clicks: a click needs a screenshot from the same request.
- Everything voice does is logged in the object server (`source: voice`).

### Clients

- The phone and desktop app keeps the rendezvous password and pairing tokens in the
  keychain (iOS keychain, macOS login keychain), moved from plain preferences.
- The web page keeps them in the browser's local storage, like a saved password; the
  page is served by the rendezvous itself (no third-party scripts).

### Accepted, for now

- Any process running as the desk user can do what that user can: read the object
  server's data and keys (it runs as that user), use the helper socket, drive the
  desktop. The line is between accounts, not between programs of one user.
- `nc-voice` holds the object server admin token in its environment; the desk user can
  read it. The object server runs as that same user, so it adds no reach.
- The object server's tasks auto-approve after 48 hours; voice doesn't use them for
  approvals for that reason.
