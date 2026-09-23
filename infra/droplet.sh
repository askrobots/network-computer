#!/bin/sh
# Day-to-day control of the test droplet.
#   infra/droplet.sh create [name]   create + set up (see create-droplet.sh)
#   infra/droplet.sh status          ip, state, services
#   infra/droplet.sh ssh             shell on it
#   infra/droplet.sh creds           rendezvous url, password, host PIN
#   infra/droplet.sh logs            follow nc-host and nc-rendezvous logs
#   infra/droplet.sh update          git pull + rebuild + restart services on the box
#   infra/droplet.sh provision       re-copy provision/ files and re-run apply.sh
#                                    (NC_DOMAIN=nc.example.com ... turns on a real https cert)
#   infra/droplet.sh dns <fqdn>      point an A record in DigitalOcean DNS at this droplet
#   infra/droplet.sh off | on        power off (still billed) / power on
#   infra/droplet.sh destroy         delete it (stops billing); recreate takes ~5 min
set -e
NAME=${NC_DROPLET:-nc-test}
ROOT=$(cd "$(dirname "$0")/.." && pwd)   # repo root, resolved before we cd
cd "$(dirname "$0")"

ip() { doctl compute droplet list --format Name,PublicIPv4 --no-header | awk -v n="$NAME" '$1==n{print $2}'; }
id() { doctl compute droplet list --format Name,ID --no-header | awk -v n="$NAME" '$1==n{print $2}'; }

case "$1" in
  create) shift; ./create-droplet.sh "${1:-$NAME}" "${2:-nyc3}" "${3:-s-2vcpu-4gb}" ;;
  status)
    doctl compute droplet list --format Name,PublicIPv4,Status,Size,Region --no-header | awk -v n="$NAME" '$1==n' || true
    IP=$(ip); [ -n "$IP" ] && ssh -o ConnectTimeout=5 root@"$IP" 'systemctl is-active nc-xorg nc-desktop nc-rendezvous nc-host | paste -sd" " -; echo "hosts: $(curl -s -u nc:$(grep NC_PASSWORD /etc/nc/env|cut -d= -f2) localhost:8765/hosts)"' 2>/dev/null || true ;;
  ssh)    ssh root@"$(ip)" ;;
  creds)  IP=$(ip); echo "rendezvous: http://$IP:8765"; ssh root@"$IP" cat /etc/nc/env ;;
  logs)   ssh root@"$(ip)" journalctl -f -u nc-host -u nc-rendezvous ;;
  update) ssh root@"$(ip)" 'cd /opt/network-computer && git pull -q && export PATH=$PATH:/usr/local/go/bin && go build -o /usr/local/bin/ ./cmd/... && systemctl restart nc-rendezvous nc-host && echo updated' ;;
  provision) IP=$(ip); ssh root@"$IP" 'mkdir -p /root/provision'; scp -q -r "$ROOT/provision/." root@"$IP":/root/provision/; ssh root@"$IP" "NC_PUBLIC_IP=$IP NC_HOST_NAME=$NAME NC_DOMAIN=$NC_DOMAIN sh /root/provision/apply.sh" ;;
  dns)
    FQDN=${2:?usage: droplet.sh dns <name.domain>}; IP=$(ip)
    # longest DigitalOcean zone that is a suffix of the name
    ZONE=$(doctl compute domain list --format Domain --no-header | awk -v f="$FQDN" '
      { z=$1; n=length(z)
        if (length(f) > n && substr(f, length(f)-n) == "." z && n > best) { best=n; zone=z } }
      END { print zone }')
    [ -n "$ZONE" ] || { echo "no DigitalOcean DNS zone for $FQDN"; exit 1; }
    REC=${FQDN%."$ZONE"}
    ID=$(doctl compute domain records list "$ZONE" --format ID,Type,Name --no-header | awk -v n="$REC" '$2=="A" && $3==n{print $1; exit}')
    if [ -n "$ID" ]; then
      doctl compute domain records update "$ZONE" --record-id "$ID" --record-data "$IP" --record-ttl 60 >/dev/null
    else
      doctl compute domain records create "$ZONE" --record-type A --record-name "$REC" --record-data "$IP" --record-ttl 60 >/dev/null
    fi
    echo "$FQDN -> $IP (A record in $ZONE, ttl 60)" ;;
  off)    doctl compute droplet-action power-off "$(id)" --wait >/dev/null && echo "$NAME powered off (still billed; destroy to stop billing)" ;;
  on)     doctl compute droplet-action power-on "$(id)" --wait >/dev/null && echo "$NAME powered on at $(ip)" ;;
  destroy) doctl compute droplet delete "$(id)" --force && echo "$NAME destroyed" ;;
  *) sed -n '2,11p' "$0" ;;
esac
