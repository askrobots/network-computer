# Use a domain (ideally with its DNS on DigitalOcean)

**Short version:** give the server a hostname like `nc.example.com` whose DNS lives in
DigitalOcean, and one command builds the server, points the name at it, and brings it
up with a real HTTPS certificate. Every client then just opens `https://nc.example.com`.
No fingerprints, no certificate warnings, and the browser microphone works.

```sh
NC_DOMAIN=nc.example.com infra/create-droplet.sh
```

## Why a real certificate matters here

Without one you have two options, and both hurt:

| | Plain HTTP | Self-signed HTTPS | **Domain + Let's Encrypt** |
|---|---|---|---|
| Video, audio, input encrypted | yes (WebRTC always is) | yes | yes |
| Login, PIN and setup encrypted | **no** | yes | yes |
| Browser microphone (Chrome, Firefox, **Safari**) | **blocked** | clunky; Safari often refuses the WebSocket | **works** |
| Phone app setup | URL only | URL + a 64-character fingerprint | **URL only** |
| Browser warning | none | "not secure", click through once per browser | **none** |
| Survives a rebuild | IP changes | IP changes; new cert, new fingerprint for every client | **same name, new cert fetched automatically** |

The microphone row is the one that forced this. Browsers only give a page the microphone
when it is served over HTTPS (or from localhost). That is a browser rule, not ours, so a
plain-HTTP server can stream the desktop to Safari but can never hear you from it.

## Why DigitalOcean DNS specifically

A droplet gets a **new IP address every time it is rebuilt**. With a certificate, the name
has to follow the server:

- **Your DNS on DigitalOcean + the API:** the tooling updates the A record itself, right after
  the droplet gets its IP, with a 60-second TTL. By the time provisioning finishes (several
  minutes) the name resolves, and the rendezvous asks Let's Encrypt for the certificate.
  Rebuild the server as often as you like; the address you give people never changes.
- **DNS anywhere else:** it still works, but after each rebuild you edit the A record at your
  provider by hand before the certificate can be issued.

The same `doctl` token that creates droplets manages DNS records, so there is nothing extra
to set up beyond moving the domain's DNS.

## One-time setup

1. **Move the domain's DNS to DigitalOcean.** In the DigitalOcean console: Networking →
   Domains → add `example.com`. At your registrar, set the nameservers to
   `ns1.digitalocean.com`, `ns2.digitalocean.com`, `ns3.digitalocean.com`. Existing records
   must be recreated in DigitalOcean first; only the record for your chosen host name is
   touched by these scripts.
2. **Let `doctl` write DNS.** A token with read and write scope covers droplets and domain
   records: `doctl auth init`. Check with `doctl compute domain list`.
3. **Pick a host name** that nothing else uses, e.g. `nc.example.com`. Use a subdomain: the
   scripts create or update exactly one A record and never touch the rest of the zone.

## What the scripts do

| Step | Command | Effect |
|---|---|---|
| Build + DNS + cert in one go | `NC_DOMAIN=nc.example.com infra/create-droplet.sh` | creates the droplet, points the A record at it (ttl 60), provisions with the domain |
| Point the name at the current droplet | `infra/droplet.sh dns nc.example.com` | creates or updates the one A record |
| Take the computer down | `infra/droplet.sh down` | parks the record at 127.0.0.1 (ttl 60), destroys the computer, keeps the desk |
| Turn HTTPS on for an existing box | `NC_DOMAIN=nc.example.com infra/droplet.sh provision` | records the domain in `/etc/nc/env`, restarts the rendezvous |
| Any DigitalOcean DNS name, any IP | `infra/dns-point.sh nc.example.com 203.0.113.5` | the building block the others use |

On the server, the rendezvous fetches and renews the certificate itself (Go's
`autocert`, no certbot), listening on 443 and 80. The desktop's own `nc-host` keeps
talking to it over a plain loopback listener (`-local 127.0.0.1:8765`) that never leaves
the machine. Firewall rules for 80/443 are opened by provisioning.

`provision/verify.sh` checks the result: the name resolves to this machine, the
certificate is valid without `-k`, the server reports `secure`, and the loopback
listener answers.

### Why park the record instead of deleting it

When a computer goes away its IP returns to DigitalOcean's pool and may be given to someone
else, so the name must stop pointing there. Deleting the record looks right but backfires:
resolvers cache "no such name" for the zone's negative TTL, 30 minutes on DigitalOcean, and
clients that auto-reconnect while the desk is down plant that answer in their resolvers.
After the next `up` they cannot reach the desk for up to half an hour. Parking the record at
127.0.0.1 with a 60-second TTL points at nothing anyone else can own, and is re-checked
within a minute once `up` points it at the new computer.

## Without a domain

Plain HTTP is fine on a trusted LAN. For the internet without a domain, `-mode secure`
makes a self-signed certificate and prints the fingerprint to pin in `nc-host`,
`nc-probe` and the phone app (see [SECURITY.md](SECURITY.md)). It is secure, just
less convenient, and browsers will not grant the microphone reliably.
