#!/bin/sh
# Create one Ubuntu droplet and provision it from provision/.
# Usage: [NC_DOMAIN=nc.example.com] [NC_HOST_NAME=nc] infra/create-droplet.sh [name] [region] [size]
# The droplet name is the cloud resource; NC_HOST_NAME (default "nc") is what clients see.
#
# With NC_DOMAIN set and that domain's DNS on DigitalOcean, the A record is
# pointed at the new droplet before provisioning, so the host comes up with a
# real Let's Encrypt certificate: one command, https, no fingerprints.
set -e
NAME=${1:-nc}; REGION=${2:-nyc3}; SIZE=${3:-s-2vcpu-4gb}
IMAGE=ubuntu-24-04-x64
ROOT=$(cd "$(dirname "$0")/.." && pwd)

KEY_ID=$(doctl compute ssh-key list --format ID --no-header | paste -sd, -)
[ -n "$KEY_ID" ] || { echo "no SSH key on DigitalOcean; add one with: doctl compute ssh-key import mykey --public-key-file ~/.ssh/id_ed25519.pub"; exit 1; }

echo "creating $NAME ($SIZE, $REGION)..."
doctl compute droplet create "$NAME" --region "$REGION" --size "$SIZE" --image "$IMAGE" \
  --ssh-keys "$KEY_ID" --tag-name nc --wait --format ID,Name,PublicIPv4 --no-header

IP=""
for i in $(seq 1 30); do
  IP=$(doctl compute droplet list --format Name,PublicIPv4 --no-header | awk -v n="$NAME" '$1==n{print $2}')
  [ -n "$IP" ] && break; sleep 3
done
[ -n "$IP" ] || { echo "no public IP yet; run: infra/droplet.sh status"; exit 1; }
echo "droplet $NAME at $IP"
if [ -n "$NC_DOMAIN" ]; then
  # point DNS now: it propagates while the box provisions, well before the
  # rendezvous asks Let's Encrypt for a certificate at the end
  sh "$(dirname "$0")/dns-point.sh" "$NC_DOMAIN" "$IP"
fi
echo "waiting for ssh..."
for i in $(seq 1 30); do ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 root@"$IP" true 2>/dev/null && break; sleep 5; done

NC_DOMAIN=$NC_DOMAIN sh "$(dirname "$0")/provision-host.sh" "$IP" "${NC_HOST_NAME:-nc}"
