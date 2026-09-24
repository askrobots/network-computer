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
#   infra/droplet.sh idle             how long since the last device disconnected
#   infra/droplet.sh autostop N|off   from this Mac: 'down' after N idle minutes (checked every 10 min)
# Defaults (domain, desk, user, region, size) come from the profile in NC_DESK_ENV:
# infra/desk.env unless people.sh picked a person's (infra/people/NAME.env).
set -e
DIR=$(cd "$(dirname "$0")" && pwd); ROOT=$(cd "$DIR/.." && pwd)
ENVF=${NC_DESK_ENV:-$DIR/desk.env}
[ -f "$ENVF" ] && . "$ENVF"
export NC_DESK_ENV=$ENVF
cd "$DIR"
# the computer: NC_DROPLET, else the profile's host name, else the droplet tagged "nc"
NAME=${NC_DROPLET:-$NC_HOST_NAME}
NAME=${NAME:-$(doctl compute droplet list --tag-name nc --format Name --no-header 2>/dev/null | head -1)}
NAME=${NAME:-nc}
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
  update) ssh root@"$(ip)" 'cd /opt/network-computer && git pull -q && export PATH=$PATH:/usr/local/go/bin && go build -buildvcs=false -o /usr/local/bin/ ./cmd/... && systemctl restart nc-rendezvous nc-host && echo updated' ;;
  provision) IP=$(ip); ssh root@"$IP" 'mkdir -p /root/provision'; scp -q -r "$ROOT/provision/." root@"$IP":/root/provision/
             # the profile's personal settings (keys too) go as a file, never on a command line
             [ -f "$ENVF" ] && { scp -q "$ENVF" root@"$IP":/root/provision/person.env; ssh root@"$IP" 'chmod 600 /root/provision/person.env'; }
             SIZE=$(doctl compute droplet get "$(id)" -o json | grep -o '"size_slug": *"[^"]*"' | head -1 | cut -d'"' -f4)
             PRICE=$(doctl compute size list --format Slug,PriceHourly --no-header | awk -v s="$SIZE" '$1==s{print $2}')
             ssh root@"$IP" "NC_PUBLIC_IP=$IP NC_HOST_NAME=$NC_HOST_NAME NC_DOMAIN=$NC_DOMAIN NC_DESK_USER=$NC_DESK_USER NC_KEYBOARD=$NC_KEYBOARD NC_SIZE=$SIZE NC_PRICE_HOURLY=$PRICE sh /root/provision/apply.sh" ;;
  dns)    sh ./dns-point.sh "${2:?usage: droplet.sh dns <name.domain>}" "$(ip)" ;;
  desk)   doctl compute volume list --format Name,Size,Region,DropletIDs,Tags --no-header | awk -v n="$VOL" '$1==n' | grep . || echo "no desk named $VOL yet (it is created on the first 'up')" ;;
  snapshot-desk)
    VID=$(volid); [ -n "$VID" ] || { echo "no desk named $VOL"; exit 1; }
    doctl compute volume snapshot "$VID" --snapshot-name "$VOL-$(date +%Y%m%d-%H%M)" --tag nc-desk >/dev/null
    echo "snapshot of $VOL taken" ;;
  off)    doctl compute droplet-action power-off "$(id)" --wait >/dev/null && echo "$NAME powered off (still billed; 'down' stops billing)" ;;
  on)     doctl compute droplet-action power-on "$(id)" --wait >/dev/null && echo "$NAME powered on at $(ip)" ;;
  idle)   IP=$(ip); [ -n "$IP" ] || { echo "no computer is up"; exit 0; }
          ssh -o ConnectTimeout=8 root@"$IP" 'cat /run/nc-host/idle 2>/dev/null' | awk -v now="$(date +%s)" '
            $1=="busy" {print $2 " device(s) connected"; exit}
            $1=="idle-since" {printf "idle for %d min\n", (now-$2)/60; exit}
            {print "unknown"}' ;;
  autostop)
    # The decision and the 'down' run here, on this Mac: the DigitalOcean token
    # never goes on the desk (a desk that could delete droplets is too much power).
    PLIST=$HOME/Library/LaunchAgents/com.dbbasic.nc-autostop.plist; LOG=$HOME/Library/Logs/nc-autostop.log
    case "$2" in
      off)
        launchctl bootout "gui/$(/usr/bin/id -u)" "$PLIST" 2>/dev/null; rm -f "$PLIST"
        IP=$(ip); [ -z "$IP" ] || ssh -o ConnectTimeout=8 root@"$IP" 'rm -f /etc/nc/autostop' 2>/dev/null
        echo "auto-stop off" ;;
      check)   # run by launchd every 10 minutes
        N=${3:?minutes}; IP=$(ip); [ -n "$IP" ] || exit 0
        S=$(ssh -o ConnectTimeout=8 -o BatchMode=yes root@"$IP" 'cat /run/nc-host/idle 2>/dev/null') || exit 0
        set -- $S
        if [ "$1" = idle-since ] && [ $(( ($(date +%s) - $2) / 60 )) -ge "$N" ]; then
          echo "$(date '+%F %T') idle $(( ($(date +%s) - $2) / 60 )) min >= $N: down" >> "$LOG"
          "$DIR/droplet.sh" down >> "$LOG" 2>&1
        fi ;;
      ''|*[!0-9]*) echo "usage: droplet.sh autostop MINUTES|off"; exit 1 ;;
      *)
        mkdir -p "$(dirname "$PLIST")"
        cat > "$PLIST" <<EOT
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.dbbasic.nc-autostop</string>
  <key>ProgramArguments</key><array><string>/bin/sh</string><string>$DIR/droplet.sh</string><string>autostop</string><string>check</string><string>$2</string></array>
  <key>StartInterval</key><integer>600</integer>
  <key>EnvironmentVariables</key><dict><key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string></dict>
  <key>StandardErrorPath</key><string>$LOG</string>
</dict></plist>
EOT
        launchctl bootout "gui/$(/usr/bin/id -u)" "$PLIST" 2>/dev/null; launchctl bootstrap "gui/$(/usr/bin/id -u)" "$PLIST"
        IP=$(ip); [ -z "$IP" ] || ssh -o ConnectTimeout=8 root@"$IP" "echo $2 > /etc/nc/autostop" 2>/dev/null
        echo "auto-stop on: 'down' after $2 idle minutes, checked every 10 min from this Mac (log: $LOG)" ;;
    esac ;;
  *) sed -n "2,14p" "$DIR/droplet.sh" ;;
esac
