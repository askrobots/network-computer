# infra

Booting and provisioning hosts. No containers: a plain Ubuntu box, the binaries
built from source, and systemd units. The config itself lives in `../provision/`
as versioned files; these scripts just create a machine and run the provisioner
on it.

## How it fits together

```
create-<provider>.sh   boots an Ubuntu 24.04 instance in a region
        │
        └── provision-host.sh <ip>   copies ../provision/ and runs apply.sh
                    │
                    └── ../provision/apply.sh   installs, configures, starts services
```

`provision/apply.sh` is provider-agnostic. Any Ubuntu 24.04 box works, including
bare metal or a machine you already have:

```sh
infra/provision-host.sh <ip> [host-name] [ssh-user]
```

## Recommended: a domain on DigitalOcean DNS

```sh
NC_DOMAIN=nc.example.com infra/create-droplet.sh     # droplet + DNS + Let's Encrypt, one command
infra/droplet.sh dns nc.example.com                  # re-point the name at the current droplet
NC_DOMAIN=nc.example.com infra/droplet.sh provision  # turn HTTPS on for an existing box
```

A rebuilt droplet gets a new IP; with the domain's DNS on DigitalOcean the scripts move
the A record (ttl 60) themselves, so the address you hand out never changes and the
certificate is always real. `dns-point.sh` is the building block. Full reasoning and the
one-time setup: [../docs/DOMAIN.md](../docs/DOMAIN.md).

## Providers

Only DigitalOcean has been tested end to end. The others are written the same
way and should work, but treat them as first runs.

| Script | Provider | Default region | Notes |
|---|---|---|---|
| `create-droplet.sh` | DigitalOcean | nyc3 | **Tested.** `doctl` auth. No central-US region; ~42 ms from Houston. |
| `create-vultr.sh` | Vultr | dfw (Dallas) | `vultr-cli` + `VULTR_API_KEY`. Dallas is ~10-20 ms from Houston. |
| `create-linode.sh` | Linode / Akamai | us-central (Dallas) | `linode-cli configure`. Dallas metro. |
| `create-aws.sh` | AWS EC2 | us-east-2 (Ohio) | `aws configure`. ~25-30 ms. Opens a security group with the ports. |

Latency is set by geography, not machine type. From Houston, a Dallas box
(Vultr/Linode) or an AWS Houston Local Zone wins; DigitalOcean's closest US
region is New York.

### Getting a key

- **DigitalOcean:** `doctl auth init` (token at cloud.digitalocean.com/account/api/tokens)
- **Vultr:** `export VULTR_API_KEY=...` (my.vultr.com/settings/#settingsapi)
- **Linode:** `linode-cli configure` (cloud.linode.com/profile/tokens)
- **AWS:** `aws configure` (an IAM access key)

Each provider needs your SSH public key registered; the scripts use
`~/.ssh/id_ed25519.pub` and tell you the one-liner to upload it if it is missing.

### AWS Houston Local Zone (lowest latency from Houston)

Local Zones put compute in-metro (single-digit to low-teens ms) but need a bit
more plumbing than a standard region. Once `aws configure` works:

```sh
# 1. opt the zone in (Houston is a child of us-east-1)
aws ec2 modify-availability-zone-group --region us-east-1 \
  --group-name us-east-1-iah-1 --opt-in-status opted-in

# 2. you then need a subnet in us-east-1-iah-1a with a route to an
#    internet gateway, and to launch there. Ask and this can be scripted
#    into a create-aws-localzone.sh once you have an account to test on.
```

Dallas Local Zone is `us-east-1-dfw-1` if Houston is unavailable.

## DigitalOcean day-to-day

```
infra/droplet.sh status | ssh | creds | logs | update | provision | dns <name> | off | on | destroy
```

`update` pulls and rebuilds the binaries; `provision` re-copies `../provision/`
and re-applies it. Powered-off droplets are still billed; `destroy` when idle
and recreate in about five minutes.
