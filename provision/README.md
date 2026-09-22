# provision

Configuration for a network-computer host, as files in git rather than commands
typed into a box. No containers, no config-management daemon: real config files
under `files/`, a package list, and one idempotent `apply.sh` that copies them
into place and starts the services.

## Use

```sh
# on a fresh Ubuntu 24.04 box, as root, with the repo checked out:
NC_PUBLIC_IP=<the box ip> provision/apply.sh
```

Re-run it any time to converge the box back to what the repo says. To change how
a box is set up, edit a file under `files/`, commit, and re-run `apply.sh` (the
`infra/droplet.sh update` shortcut does the pull-and-restart for the binaries).

## The rule: no hand fixes

Anything fixed by hand on a live box (a missing default app, a sound setting, a
sysctl) must land here as a file or an `apply.sh` step, or the next box will have
the same problem. `verify.sh` is the checklist of every such fix: `apply.sh` runs
it last and it prints one line per item, failing loudly if something is missing.
Run it any time on a host:

```sh
sh /root/provision/verify.sh        # or: infra/droplet.sh provision (re-applies, then verifies)
```

When you fix something new, add a check for it to `verify.sh` in the same commit.

## Layout

```
apply.sh              idempotent installer/orchestrator (ends by running verify.sh)
verify.sh             checklist of everything a host needs; non-zero exit if not
packages.txt         apt packages, one per line
files/etc/...         copied verbatim into /etc on the host:
  X11/xorg.conf.d     headless 1080p dummy display
  pulse/              48 kHz virtual sink, stable socket path
  dconf/              polished desktop defaults (word wrap, line numbers, ...)
  xdg/                default apps: Firefox for links, xfce helpers (browser, terminal, files)
  sysctl.d/           disable IPv6 (no route on the test box)
  udev/rules.d/       /dev/uinput access for input injection
  systemd/system/     nc-xorg, nc-desktop, nc-audio, nc-rendezvous, nc-host
```

Secrets (`/etc/nc/env`: rendezvous password and host PIN) are generated on the
box the first time and never committed.
