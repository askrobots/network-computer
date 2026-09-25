# Remote support: handing your desk to someone else (or to an AI)

Status: **design, not built yet** (2026-09-25). Written down so it shapes the work that
comes before it.

Sometimes the person at the desk is stuck. A family member, a colleague or an IT person
could fix it in a minute if they could see the screen, and an AI could often fix it without
anyone. Today the only way to let someone in is to give them the desk's password and PIN,
which is giving them the desk forever. Support should be a **visit**: invited, visible,
limited and over when you say so.

## What already exists

- **Several devices can be connected at once**, and each gets the picture, the sound and
  the input (`nc-host` keeps a session per peer). This is how a phone and a Mac share a desk,
  and it is the base a helper's session would build on.
- **Pairing**: a device that got in once gets a pairing token and skips the PIN afterwards.
- **The voice assistant already acts on the desk** through actions with approval levels
  (safe / reviewed by a second model / asked out loud), and it can `look` at the screen.
  `nc-desk` gives the same actions to scripts. Remote AI support is a new front end to this
  layer, not a new layer.
- **Diagnostics**: `provision/verify.sh` (about 68 checks), `nc-health`, the service journals,
  and the desktop dashboard.

## 1. A person helping

**Invite, don't share.** From the desk (the 🔍 bar, a menu entry, or "let Sam help me" to
voice), make a **support invite**: a link plus a short code, good for one use within
15 minutes. The helper opens it in a browser; nothing to install. The desk's password,
PIN and pairing secret are never given out.

**You see them arrive and you let them in.** When the helper connects, the desk shows who it
is and what they asked for, and the owner taps **Allow**. Owner not there (the machine is
stuck at a login screen, say)? Then an invite made in advance with "no approval needed"
works, and only that way.

**Two levels, owner's choice, changeable at any time:**

| Level | Helper gets |
| --- | --- |
| **Watch** | the picture (and sound if allowed); no keyboard or mouse. Good for "show me what you see". |
| **Control** | keyboard and mouse too. The owner's input still works and **wins**: any owner touch or key pauses the helper's input for a few seconds (no fighting over the pointer). |

**Always visible while it lasts.** A banner on the desk and on every one of the owner's
devices: "Sam is watching / has control · 12 min · Stop". **Stop** ends the session at once and
spends the invite. It also ends by itself: at the time limit, when the owner disconnects (if
set), or when the helper closes the tab.

**Limits on what a helper can reach:**
- no file transfer, clipboard or printing to the helper's device unless the owner turns it on
  for this visit (a screen full of your documents is enough to share);
- no voice: the microphone button belongs to the owner;
- API keys and passwords never appear in the desk's UI, so they do not travel with the picture;
- helpers cannot make invites, see pairings or change the desk's settings.

**A record.** Every visit is written to the desk's log (who, when, level, how long, how it
ended) and, with the object server, to the owner's records. "Who was on my computer?" is a
question the desk can answer.

**Technically** (nc-host + rendezvous): an invite is a signed, expiring token minted by the
host (like pairing tokens, but single-use and scoped: `watch` or `control`, TTL, optional
file rights). The rendezvous admits a peer with such a token without the password. The host
marks that session as a helper: its input channel is ignored in Watch mode and gated by
owner activity in Control mode; file, clipboard and voice channels are refused. A control
message to the owner's clients drives the banner.

## 2. An AI helping

The same idea with an AI as the visitor, and the easier one to start with, since the pieces
exist:

- **"Something's wrong" button** (and "what's wrong with my computer?" to voice). The desk
  gathers what a technician would: `verify.sh`, failed services and their last log lines,
  disk and memory, what is on screen (a screenshot, with the same care as voice's `look`),
  and the last few actions voice took. It **redacts** (the app kit's `redactForAi` rules; no
  API keys, no file contents unless asked) and gives it to a support model.
- The AI **explains first**, then proposes fixes as **desk actions** with the voice's
  approval levels: restarting a service or freeing disk is safe or reviewed; anything that
  deletes, pays or changes settings is asked out loud. It says what it did and whether the
  check passes afterwards.
- **Escalation**: if it cannot fix it, it writes the problem up (what it saw, what it tried)
  and can make a **Watch** invite for a person, so the human helper starts where the AI left
  off instead of from "what's wrong?".
- **Support for many desks** (people.sh desks, a family's or a small office's): the same
  diagnostics can go to the desk's admin, with the owner's consent, from the controller on
  object.dbbasic.com (never from a desk holding another person's token).

## 3. Order of work

1. Owner banner showing every connected device (useful now: "is anyone else on?").
2. Helper sessions in nc-host: invite tokens, Watch only, Stop, time limit, log.
3. Control mode with owner-wins input.
4. "Something's wrong": diagnostics bundle + support model + actions with approval.
5. AI → human escalation with a pre-made Watch invite; object server records.

## Principles (see [DESIGN.md](DESIGN.md))

- **Say what really happened**: the owner always knows who is connected and what they did.
- **AI suggests, people decide**: the support AI proposes; approval follows the action's effect.
- **Less room, less shown**: on a phone the banner is one line with a Stop button.
