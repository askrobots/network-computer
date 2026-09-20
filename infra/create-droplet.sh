#!/bin/sh
# Creates one Ubuntu droplet for testing and runs setup-linux.sh on it.
# Usage: infra/create-droplet.sh [name] [region] [size]
# Needs: doctl logged in, an SSH key registered with DigitalOcean.
set -e
NAME=${1:-nc-test}
REGION=${2:-nyc3}
SIZE=${3:-s-2vcpu-4gb}     # software x264 at 720p30 needs 2 vCPU; s-1vcpu-1gb is fine for rendezvous only
IMAGE=ubuntu-24-04-x64

KEY_ID=$(doctl compute ssh-key list --format ID --no-header | paste -sd, -)
[ -n "$KEY_ID" ] || { echo "no SSH key on DigitalOcean; add one: doctl compute ssh-key import mykey --public-key-file ~/.ssh/id_ed25519.pub"; exit 1; }

echo "creating $NAME ($SIZE, $REGION)..."
doctl compute droplet create "$NAME" --region "$REGION" --size "$SIZE" --image "$IMAGE" \
  --ssh-keys "$KEY_ID" --tag-name nc --wait --format ID,Name,PublicIPv4 --no-header

IP=""
for i in $(seq 1 30); do
  IP=$(doctl compute droplet list --format Name,PublicIPv4 --no-header | awk -v n="$NAME" '$1==n{print $2}')
  [ -n "$IP" ] && break; sleep 3
done
[ -n "$IP" ] || { echo "no public IP yet; run: infra/droplet.sh status"; exit 1; }
echo "droplet $NAME at $IP; waiting for ssh..."
for i in $(seq 1 30); do ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 root@"$IP" true 2>/dev/null && break; sleep 5; done

scp -q "$(dirname "$0")/setup-linux.sh" root@"$IP":/root/
ssh root@"$IP" "NC_PUBLIC_IP=$IP sh /root/setup-linux.sh"
echo
echo "done. rendezvous: http://$IP:8765   password and host PIN are in: ssh root@$IP cat /etc/nc/env"
