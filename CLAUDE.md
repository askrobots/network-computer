# network-computer — agent context

A desk you reach from anything: Go host + rendezvous (WebRTC), a browser client,
a Flutter client, and the provisioning that builds a Linux desk on DigitalOcean
with the DBBASIC object server, voice, and the dbbasic apps. Public (MIT); the
dbbasic apps it installs are private and only ever arrive as built binaries.

**Read [docs/DESIGN.md](docs/DESIGN.md) before changing anything a person sees**:
less room, less shown (most important first, the rest behind a tap); touch as a
first-class way in; every capability also an action; AI suggests, people decide;
plain files; say what really happened.

Where things are:

- `cmd/` — nc-host, nc-rendezvous (and its browser client in `web/`), nc-probe.
- `provision/` — `apply.sh` (idempotent), `verify.sh` (the checks), and the files
  it copies into `/` (`files/`): the desk's own tools (`nc-voice`, `nc-desk`,
  `nc-device`, `nc-dashboard`, `nc-apps`, `nc-object-files`...).
- `infra/` — `droplet.sh` (a desk's life), `people.sh` (desks for others),
  `apps.sh` (build the dbbasic apps on a desk).
- `docs/` — DESKTOP.md (what the desk does), DESKS.md, DBBASIC-APPS.md, DESIGN.md.

Rules that are not obvious from the code:

- Never put the DigitalOcean token on a desk; desk-management scheduling belongs
  on object.dbbasic.com, not on a Mac or a desk.
- Package installs must never restart the desk's own services
  (`/etc/needrestart/conf.d/nc.conf`): that closes every window of a session.
- Test client changes yourself (a Mac build with auto-connect, screenshots)
  before asking anyone to try them; the person may be using the desk while you
  work, so stay out of their windows and never type into apps holding their data.
- `go build ./... && go vet ./...`, `sh -n` for scripts, and `provision/verify.sh`
  on a desk after provisioning.
