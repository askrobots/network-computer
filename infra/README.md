# infra

Scripts for a throwaway test box. No containers: a plain Ubuntu droplet, the binaries
built from source, and systemd units.

- `create-droplet.sh [name] [region] [size]`: creates a DigitalOcean droplet with doctl
  and runs `setup-linux.sh` on it over ssh.
- `setup-linux.sh`: on any Ubuntu 24.04 box, installs ffmpeg and a headless Xorg with an
  xfce desktop at 1080p, builds `nc-rendezvous` and `nc-host`, generates a password and
  PIN into `/etc/nc/env`, opens the firewall, and starts everything as services.

After it runs, the box is both the rendezvous (public IP) and a "network computer"
host called `cloudbox`. Point `nc-host` on a machine behind NAT at the same rendezvous
to test the NAT path, and point the browser or the iPhone app at `http://<ip>:8765`.

Tear down: `doctl compute droplet delete <name>`.
