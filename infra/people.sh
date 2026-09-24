#!/bin/sh
# Desks for other people: one machine and one desk volume each, set up for them.
#   infra/people.sh new NAME          a profile from the template: infra/people/NAME.env (edit it)
#   infra/people.sh up NAME           create their desk: volume, machine, DNS, provisioning, their things
#   infra/people.sh welcome NAME      what to send them: address, login, PIN, how to connect
#   infra/people.sh list              everyone, and whose machine is up
#   infra/people.sh health [NAME|all] check services; restart any that stopped (and power on if off)
#   infra/people.sh update [NAME|all] bring them to the latest version (re-provision)
#   infra/people.sh NAME <droplet.sh command>   anything else: status, ssh, logs, idle, down, creds...
# Profiles are gitignored (they can hold keys). Their desk volumes are never deleted by any script.
set -e
DIR=$(cd "$(dirname "$0")" && pwd); P=$DIR/people
profile() { f=$P/$1.env; [ -f "$f" ] || { echo "no profile $f (infra/people.sh new $1)"; exit 1; }; echo "$f"; }
people() { for f in "$P"/*.env; do n=$(basename "$f" .env); [ "$n" = example ] || echo "$n"; done; }
run() { NC_DESK_ENV=$(profile "$1") sh "$DIR/droplet.sh" "$2" ${3:+"$3"}; }

case "$1" in
  new)
    N=${2:?usage: people.sh new NAME}
    echo "$N" | grep -Eq '^[a-z][a-z0-9-]{1,30}$' || { echo "NAME: lowercase letters, digits, dashes"; exit 1; }
    f=$P/$N.env; [ ! -e "$f" ] || { echo "$f exists"; exit 1; }
    BASE=$( [ -f "$DIR/desk.env" ] && . "$DIR/desk.env"; echo "${NC_DOMAIN#*.}" )
    sed -e "s/alice\\.example\\.com/$N.${BASE:-example.com}/" -e "s/=alice /=$N /; s/=alice$/=$N/" \
        -e "s/\"Alice\"/\"$N\"/" -e "s/Call me Alice/Call me $N/" "$P/example.env" > "$f"
    chmod 600 "$f"
    echo "profile: $f (edit it: name, time zone, apps, voice style, keys), then: infra/people.sh up $N" ;;
  up)
    f=$(profile "${2:?usage: people.sh up NAME}")
    NC_DESK_ENV=$f sh "$DIR/create-droplet.sh"
    echo; sh "$0" welcome "$2" ;;
  welcome)
    f=$(profile "${2:?usage: people.sh welcome NAME}")
    ( . "$f"; IP=$(doctl compute droplet list --format Name,PublicIPv4 --no-header | awk -v n="$NC_HOST_NAME" '$1==n{print $2}')
      [ -n "$IP" ] || { echo "$NC_HOST_NAME is not up"; exit 1; }
      ssh -o ConnectTimeout=8 root@"$IP" '. /etc/nc/env; printf "%s\n%s\n%s\n" "$NC_PASSWORD" "$NC_PIN" "$NC_HOST_NAME"' | {
        read -r PASS; read -r PIN; read -r HOST
        ADDR=${NC_DOMAIN:+https://$NC_DOMAIN}; ADDR=${ADDR:-http://$IP:8765}
        cat <<EOT
Hi ${NC_WELCOME_NAME:-$2},

Your computer is ready. Open it from any browser (or the Network Computer app):

  Address   $ADDR
  User      nc
  Password  $PASS
  Computer  $HOST
  PIN       $PIN   (asked once per device)

Click the picture to start using it; Shift+Esc gives the mouse back. There is a
Welcome note on its Desktop with the basics (search, voice, copy and paste, files).
EOT
      } )
    ;;
  list)
    UP=$(doctl compute droplet list --format Name,PublicIPv4,Size,Status --no-header)
    printf "%-14s %-26s %-14s %s\n" PERSON ADDRESS SIZE MACHINE
    for n in $(people); do
      ( . "$P/$n.env"; m=$(echo "$UP" | awk -v h="$NC_HOST_NAME" '$1==h{print $4" "$2}')
        printf "%-14s %-26s %-14s %s\n" "$n" "${NC_DOMAIN:-(ip only)}" "${NC_SIZE:-?}" "${m:-down (desk kept)}" )
    done ;;
  health)
    for n in $( [ -z "$2" ] || [ "$2" = all ] && people || echo "$2" ); do
      ( . "$(profile "$n")"
        ID=$(doctl compute droplet list --format Name,ID,Status --no-header | awk -v h="$NC_HOST_NAME" '$1==h{print $2" "$3}')
        [ -n "$ID" ] || { echo "$n: down (not created)"; exit 0; }
        set -- $ID
        if [ "$2" = off ] && [ "${NC_KEEP_RUNNING:-yes}" = yes ]; then
          doctl compute droplet-action power-on "$1" --wait >/dev/null && echo "$n: was off, powered on"; fi
        IP=$(doctl compute droplet get "$1" --format PublicIPv4 --no-header)
        ssh -o ConnectTimeout=10 -o BatchMode=yes root@"$IP" '
          bad=""; for s in nc-xorg nc-desktop nc-audio nc-rendezvous nc-host nc-object-server nc-object-daemon nc-voice; do
            systemctl is-active --quiet $s || { systemctl restart $s; bad="$bad $s"; }; done
          echo "${bad:+restarted:$bad}${bad:-all services up}; desk $(df -h /desk 2>/dev/null | awk "NR==2{print \$5}") used"' 2>/dev/null |
          sed "s/^/$n: /" || echo "$n: unreachable over ssh" )
    done ;;
  update)
    for n in $( [ -z "$2" ] || [ "$2" = all ] && people || echo "$2" ); do
      echo "== $n"; run "$n" provision | grep -E "FAIL|ok, |!!" || true
    done ;;
  ''|-h|--help) sed -n 2,10p "$0" ;;
  *)  run "$1" "${2:-status}" "$3" ;;
esac
