#!/bin/sh
# Day-to-day control of your desk and the computer it is attached to.
#   infra/droplet.sh up [size]        sit down: a computer of any size, desk attached, provisioned
#   infra/droplet.sh down             get up: destroy the computer and its DNS record; the desk stays
#   infra/droplet.sh status           computer, services, desk usage
#   infra/droplet.sh ssh              shell on the computer
#   infra/droplet.sh creds            address, login and PIN (kept on the desk)
#   infra/droplet.sh logs             follow nc-host and nc-rendezvous
#   infra/droplet.sh update           git pull + rebuild + restart the nc services
#   infra/droplet.sh provision        re-copy provision/ and re-run apply.sh
#   infra/droplet.sh dns <fqdn>       point an A record at this computer
#   infra/droplet.sh desk             the desk volume: size, region, attachment
#   infra/droplet.sh snapshot-desk    back up the desk (volume snapshot, ~$0.06/GB-month)
#   infra/droplet.sh off | on         power off (still billed) / on
# Defaults (domain, desk, user, region, size) come from infra/desk.env.
set -e
DIR=$(cd "$(dirname "$0")" && pwd); ROOT=$(cd "$DIR/.." && pwd)
[ -f "$DIR/desk.env" ] && . "$DIR/desk.env"
cd "$DIR"
# the computer: NC_DROPLET, else the droplet tagged "nc", else the host name
NAME=${NC_DROPLET:-$(doctl compute droplet list --tag-name nc --format Name --no-header 2>/dev/null | head -1)}
NAME=${NAME:-${NC_HOST_NAME:-nc}}
VOL=desk-${NC_DESK:-${NC_HOST_NAME:-nc}}

ip() { doctl compute droplet list --format Name,PublicIPv4 --no-header | awk -v n="$NAME" '$1==n{print $2}'; }
id() { doctl compute droplet list --format Name,ID --no-header | awk -v n="$NAME" '$1==n{print $2}'; }
volid() { doctl compute volume list --format ID,Name --no-header | awk -v n="$VOL" '$2==n{print $1; exit}'; }

case "$1" in
  up|create)
    [ -z "$(id)" ] || { echo "$NAME is already up at $(ip)"; exit 0; }
    ./create-droplet.sh "${NC_HOST_NAME:-nc}" "${NC_REGION:-nyc3}" "${2:-${NC_SIZE:-s-2vcpu-4gb}}" ;;
  down|destroy)
    DID=$(id); [ -n "$DID" ] || { echo "no computer is up; desk $VOL is untouched"; exit 0; }
    IP=$(ip)
    DOMAIN=${NC_DOMAIN:-$(ssh -o ConnectTimeout=5 root@"$IP" 'sed -n "s/^NC_DOMAIN=//p" /etc/nc/env' 2>/dev/null || true)}
    # park DNS first, so the name never points at an IP that is about to be released
    [ -z "$DOMAIN" ] || sh ./dns-park.sh "$DOMAIN" "$IP"
    doctl compute droplet delete "$DID" --force
    echo "$NAME destroyed (billing stopped); desk $VOL kept" ;;
  status)
    doctl compute droplet list --format Name,PublicIPv4,Status,Size,Region --no-header | awk -v n="$NAME" '$1==n' || true
    IP=$(ip); [ -n "$IP" ] && ssh -o ConnectTimeout=5 root@"$IP" '
      echo "services: $(systemctl is-active nc-xorg nc-desktop nc-audio nc-rendezvous nc-host | paste -sd" " -)"
      . /etc/nc/env; echo "hosts: $(curl -s -u "nc:$NC_PASSWORD" localhost:8765/hosts)"
      mountpoint -q /desk && echo "desk: $(df -h /desk | awk "NR==2{print \$3\" used of \"\$2\" (\"\$5\")\"}")" || echo "desk: not mounted"' 2>/dev/null || true ;;
  ssh)    ssh root@"$(ip)" ;;
  creds)  ssh root@"$(ip)" '. /etc/nc/env; if [ -n "$NC_DOMAIN" ]; then echo "address:  https://$NC_DOMAIN"; else echo "address:  http://$NC_PUBLIC_IP:8765"; fi
            echo "user:     nc"; echo "password: $NC_PASSWORD"; echo "host:     $NC_HOST_NAME"; echo "PIN:      $NC_PIN"' ;;
  logs)   ssh root@"$(ip)" journalctl -f -u nc-host -u nc-rendezvous ;;
  update) ssh root@"$(ip)" 'cd /opt/network-computer && git pull -q && export PATH=$PATH:/usr/local/go/bin && go build -o /usr/local/bin/ ./cmd/... && systemctl restart nc-rendezvous nc-host && echo updated' ;;
  provision) IP=$(ip); ssh root@"$IP" 'mkdir -p /root/provision'; scp -q -r "$ROOT/provision/." root@"$IP":/root/provision/
             ssh root@"$IP" "NC_PUBLIC_IP=$IP NC_HOST_NAME=$NC_HOST_NAME NC_DOMAIN=$NC_DOMAIN NC_DESK_USER=$NC_DESK_USER NC_KEYBOARD=$NC_KEYBOARD sh /root/provision/apply.sh" ;;
  dns)    sh ./dns-point.sh "${2:?usage: droplet.sh dns <name.domain>}" "$(ip)" ;;
  desk)   doctl compute volume list --format Name,Size,Region,DropletIDs,Tags --no-header | awk -v n="$VOL" '$1==n' | grep . || echo "no desk named $VOL yet (it is created on the first 'up')" ;;
  snapshot-desk)
    VID=$(volid); [ -n "$VID" ] || { echo "no desk named $VOL"; exit 1; }
    doctl compute volume snapshot "$VID" --snapshot-name "$VOL-$(date +%Y%m%d-%H%M)" --tag nc-desk >/dev/null
    echo "snapshot of $VOL taken" ;;
  off)    doctl compute droplet-action power-off "$(id)" --wait >/dev/null && echo "$NAME powered off (still billed; 'down' stops billing)" ;;
  on)     doctl compute droplet-action power-on "$(id)" --wait >/dev/null && echo "$NAME powered on at $(ip)" ;;
  *) sed -n "2,14p" "$DIR/droplet.sh" ;;
esac
