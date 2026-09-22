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
2. `/config` now reports `mode` (`secure` when TLS is on, else `insecure`) and the
   browser client shows it in the session bar. A `-mode` flag to *force* the posture
   is still to do.
3. Self-signed + fingerprint pinning for domain-less secure mode.
4. Per-host pairing token (PIN once).
5. Per-session TURN credentials.
