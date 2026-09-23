# Desks: hot desking, storage, and automatic start and stop

Plan, 2026-09-22. How the network computer becomes something people and companies
use every day without leaving expensive servers running.

## The model

**The computer is disposable. The desk is yours.**

- The **computer** is a cloud server of whatever size the task needs. It is built from a
  saved image or by provisioning, used, then destroyed. Nothing on it matters.
- The **desk** is what persists: settings, keys, documents, models. It attaches to whichever
  computer you sit down at. Users always see the same address, files and settings, whether
  today's computer has 2 CPUs or a GPU.

"Sit down" creates a computer, attaches the desk, provisions, and points DNS.
"Get up" destroys the computer and keeps the desk.

## Storage, split by lifecycle

Backups, resets and deletion all happen per volume, so data with different rules gets
different volumes (or does not live on a volume at all).

| Kind | Size | Rule | Home |
|---|---|---|---|
| Identity and secrets: API keys, login, PIN, pairing secret, TLS cert | KB | never lose; must follow you anywhere | desk volume now; later a region-free encrypted store (the object server) |
| Prefs: desktop, editor, browser profile (without its cache) | MB | keep, back up | `desk` volume |
| Documents and exports, in our own file formats | GB, grows | keep, scheduled snapshots | `desk` volume |
| Speech and AI models | GB | reproducible, no backup | `models` volume, wipe anytime |
| Projects: a repo, a dataset, a client's files | varies | attach while working; maybe team-shared | one volume per project |
| Demo desk | small | reset to clean after every use | own volume, restored from a snapshot |
| OS, installed apps, caches | GB | rebuilt every boot, installed on demand | the computer's own disk |

Why secrets and the certificate belong on the desk, not the computer:

- **Let's Encrypt allows 5 identical certificates per week.** Rebuilding more often than that
  would lock out https for a week. Keeping the certificate cache on the desk reuses it.
- **A rebuild today mints a new password and PIN.** Keeping them and the pairing secret on the
  desk means a rebuilt computer is the same desk to every paired phone and browser.
- API keys are readable only by the desk's account and the AI service. DigitalOcean encrypts
  volumes at rest. Keys never go in the repo or onto clients.

Constraints that shape this (DigitalOcean):

- **Volumes are region-locked.** A desk in nyc3 cannot attach to a computer in London. The
  tiny, critical things (keys, identity, prefs) should eventually live region-free and sync
  down at sit-down; big things stay regional.
- **A volume attaches to one computer at a time.** Models shared by many desks belong in
  object storage (Spaces, $5/month minimum) rather than a volume.
- **Splitting costs nothing extra:** pricing is per GB, the minimum is 1 GB per volume, and
  a computer can take a handful of volumes (7 on DigitalOcean at time of writing).
- **Volumes grow online but never shrink:** start small.

Dev setup to build first: `desk` 2 GB (prefs, documents, secrets; snapshotted) and
`models` 3 GB (speech + one small local model; not backed up). $0.50/month together at
$0.10/GB-month. Mount logic takes a list, so project and demo volumes are just more entries,
and `verify.sh` checks each one.

On-computer AI: whisper.cpp (speech-to-text) and Piper (voice) are practical on CPU with
about 0.5 GB of models; 4 vCPUs keep up with speech. A small local model (Llama 3.2 3B,
about 2 GB) runs with llama.cpp on 8 vCPUs, usable but slow; larger models want a GPU size
or an API key.

## Sizes and hourly cost

DigitalOcean list prices, 2026-09-22. A computer bills while it exists, even powered off.

| Size | CPU / RAM | Per hour | All month | For |
|---|---|---|---|---|
| s-1vcpu-1gb | 1 / 1 GB | $0.009 | $6 | the always-on controller (below) |
| s-2vcpu-4gb | 2 / 4 GB | $0.036 | $24 | everyday desktop |
| s-4vcpu-8gb | 4 / 8 GB | $0.071 | $48 | voice AI, object server |
| c-4 | 4 dedicated / 8 GB | $0.125 | $84 | performance tests, steady CPU |
| s-8vcpu-16gb | 8 / 16 GB | $0.143 | $96 | build box, small local model |
| c-8 | 8 dedicated / 16 GB | $0.250 | $168 | heavy tests |
| gpu-4000adax1-20gb | 8 + GPU / 32 GB | $0.760 | $565 | local speech and AI models on GPU |

GPU sizes exist only in some regions, and a desk volume attaches only in its own region.

Example: desk volumes $0.50/month + 40 hours on s-4vcpu-8gb ($2.86) = about $3.40/month,
against $24/month for an always-on s-2vcpu-4gb.

## Automatic shutdown and wake

The feature that makes hot desking pay.

**Shape.** A small always-on controller (s-1vcpu-1gb, about $6/month) holds the rendezvous,
the domain and the relay; desks come and go behind it. `nc-host` already takes any rendezvous
URL, so the architecture allows this today. The object server is the natural home for the
controller logic: it already has users, permissions and API keys.

**Shutting down.**
- Idle = no clients connected, no input for N minutes, and nothing busy (a build or AI job,
  audio playing, sustained CPU). A user can say "keep awake for 3 hours".
- Shutdown means **destroying** the computer, not powering it off (powered off still bills).
  Desk volumes remain.
- **Unsaved work is the honest risk:** cloud servers cannot hibernate; a snapshot keeps the
  disk, not memory. So: warn first (on the desk and as a phone notification, with a
  countdown), skip anything showing signs of unsaved work, and make our own apps autosave.

**Waking.**
- On connect: the client shows "starting your desk, about a minute"; the controller boots from
  a saved image, attaches the person's volumes, and the client connects when the host registers.
- On schedule: pre-start at 8:50 on workdays in the user's time zone, hard-stop at night.
- For work, not only people: a nightly build, or the AI needing the desk for a task asked from
  a phone. Start, run, stop.
- Boot time decides how it feels: a saved disk image gets it to 1-2 minutes; a small pool of
  pre-started spares makes it instant at a standing cost.

**Controls a company expects:** per-team budgets and a cap on concurrent desks, size limits per
role, an audit log of every start and stop and why, a hard rule that it never deletes a volume,
and a dry-run mode.

**Savings example:** 10 people on s-4vcpu-8gb, 8 h/day, 22 days/month: about $125/month with
auto-stop plus $6 for the controller, against about $480 always-on.

## Who wants what

### A company
Pilot must-haves:
- **Company sign-in** (Google Workspace, Microsoft Entra, Okta) with multi-factor, instead of a
  shared password and PIN. Disabling someone in the directory cuts their desk off at once.
- **One desk per person**, from a company-approved image.
- **Idle shutdown and schedules** (above).
- **Backups:** scheduled desk snapshots with retention; hand a leaver's files to their manager.
- **Audit log:** who connected, when, from where and what device; admin changes.

As it grows: central secrets (company keys in one vault, per-user keys, rotation, AI spend per
person and team); data controls (region, clipboard / download / print policies); private
networking (desks without public addresses, outbound limits, device checks); managed clients
(apps via their device management, pre-configured and branded); helpdesk assist with the
user's consent; cost reporting and budget alerts; regions near their people.

Where it runs: in the company's own cloud account (the multi-provider `infra/` scripts), or fully
managed by dbbasic. Both are real options and different businesses.

The strongest fit: the **object server as the control plane**. It already has users, a permission
engine, API keys and billing, so it can manage who has a desk, what size, policies, keys and the
audit trail, and the same permission engine decides what the AI may see and do for each person.

### A traveler
- **Locked-down networks** (hotels, airports) often allow only port 443. Our relay listens on UDP
  3478 only, so there the stream fails. Needs a **relay over TLS on 443**.
- **Thin or metered links:** a data-saver profile (lower resolution and frame rate, or audio only)
  and bitrate that adapts. Today the host sends a fixed bitrate.
- **Latency:** pick the nearest region at sit-down, which needs region-free identity and a way to
  bring the desk along.
- **Drops:** reconnect already works and the desk keeps running while they are offline.
- **Hotel TV and a Bluetooth keyboard:** the planned external-display mode.
- **Safety:** nothing stored on the device (a plus at borders), Face ID to open the app, and
  revoking one lost phone (today `-reset-pairings` revokes every device).
- **Cost:** pay only while on, auto-shutdown in their current time zone.

### A salesperson doing demos
- **A demo desk that resets** from a demo image with realistic sample data, separate from their
  own desk, so nothing leaks between prospects.
- **Warm before the meeting:** pre-start 10 minutes before the calendar slot, boot from a saved
  image, a bigger size for that hour only.
- **Show anywhere:** iPad to a projector, or the web client shared in Zoom or Teams, with a
  **presentation mode** that hides our toolbar, stats and notices.
- **Let the customer drive:** a **guest link** that expires after the meeting, needs no account,
  and is view-only or allows control.
- **Customer guest Wi-Fi:** the same relay on 443, with a phone hotspot as fallback.
- **The wow moment:** talking to the computer and watching the AI act (the voice loop).
- **Polish:** their own domain (`demo.company.com`), branding, optional session recording.

## Build order

1. **Desk volumes** (`desk` + `models`), a real user account living on the desk, secrets /
   certificate / pairing moved onto it, and "sit down" / "get up" commands. Tested by rebuilding
   on a different size and confirming prefs, a test API key, the certificate and a phone pairing
   all survive.
2. **Saved disk image** for 1-2 minute boots.
3. **Relay over TLS on port 443** (travelers and demos fail without it).
4. **Host reports idle and busy.**
5. **Controller** on an always-on s-1vcpu-1gb: idle shutdown with warnings, wake on connect,
   schedules, audit log.
6. **Adaptive bitrate and data-saver profile.**
7. **Sales kit:** guest links, presentation mode, demo reset.
8. **Company sign-in, one desk per person, region-free identity** in the object server.
9. Nearest-region selection, single-device revoke.
