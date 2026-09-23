#!/bin/sh
# Create a computer, attach its desk, and provision it.
# Usage: infra/create-droplet.sh [name] [region] [size]
# Defaults come from infra/desk.env (see desk.env.example), then built-ins.
#
# The desk is a small DigitalOcean volume (desk-$NC_DESK, $NC_DESK_GB GB) that
# outlives the computer: settings, documents, secrets, and the desk's identity
# (login, PIN, pairing, certificate). It is created once, attached at boot, and
# never deleted by any script. With NC_DOMAIN set (DNS on DigitalOcean) the
# hostname is pointed at the new computer before provisioning, for https.
set -e
DIR=$(cd "$(dirname "$0")" && pwd)
[ -f "$DIR/desk.env" ] && . "$DIR/desk.env"
NAME=${1:-${NC_HOST_NAME:-nc}}; REGION=${2:-${NC_REGION:-nyc3}}; SIZE=${3:-${NC_SIZE:-s-2vcpu-4gb}}
DESK=${NC_DESK:-$NAME}; DESK_GB=${NC_DESK_GB:-1}; VOL=desk-$DESK
IMAGE=ubuntu-24-04-x64

KEY_ID=$(doctl compute ssh-key list --format ID --no-header | paste -sd, -)
[ -n "$KEY_ID" ] || { echo "no SSH key on DigitalOcean; add one with: doctl compute ssh-key import mykey --public-key-file ~/.ssh/id_ed25519.pub"; exit 1; }

# the desk: find it, or create it once
VOL_ID=$(doctl compute volume list --format ID,Name,Region --no-header | awk -v n="$VOL" '$2==n{print $1; exit}')
if [ -n "$VOL_ID" ]; then
  VOL_REGION=$(doctl compute volume list --format ID,Region --no-header | awk -v i="$VOL_ID" '$1==i{print $2}')
  [ "$VOL_REGION" = "$REGION" ] || { echo "desk $VOL is in $VOL_REGION; a computer in $REGION cannot attach it"; exit 1; }
  ATTACHED=$(doctl compute volume get "$VOL_ID" --format DropletIDs --no-header | tr -d '[] ')
  [ -z "$ATTACHED" ] || { echo "desk $VOL is attached to droplet $ATTACHED; run 'infra/droplet.sh down' first"; exit 1; }
  echo "desk $VOL found ($VOL_REGION)"
else
  echo "creating desk $VOL (${DESK_GB} GB, $REGION)..."
  VOL_ID=$(doctl compute volume create "$VOL" --region "$REGION" --size "${DESK_GB}GiB" \
    --fs-type ext4 --fs-label desk --tag nc-desk --desc "network-computer desk for $DESK" \
    --format ID --no-header)
fi

echo "creating $NAME ($SIZE, $REGION) with desk attached..."
doctl compute droplet create "$NAME" --region "$REGION" --size "$SIZE" --image "$IMAGE" \
  --ssh-keys "$KEY_ID" --tag-name nc --volumes "$VOL_ID" --wait --format ID,Name,PublicIPv4 --no-header

IP=""
for i in $(seq 1 30); do
  IP=$(doctl compute droplet list --format Name,PublicIPv4 --no-header | awk -v n="$NAME" '$1==n{print $2}')
  [ -n "$IP" ] && break; sleep 3
done
[ -n "$IP" ] || { echo "no public IP yet; run: infra/droplet.sh status"; exit 1; }
echo "droplet $NAME at $IP"
if [ -n "$NC_DOMAIN" ]; then
  # point DNS now: it propagates while the box provisions
  sh "$DIR/dns-point.sh" "$NC_DOMAIN" "$IP"
fi
echo "waiting for ssh..."
for i in $(seq 1 30); do ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 root@"$IP" true 2>/dev/null && break; sleep 5; done

NC_DOMAIN=$NC_DOMAIN NC_DESK_USER=${NC_DESK_USER:-user} sh "$DIR/provision-host.sh" "$IP" "$NAME"
