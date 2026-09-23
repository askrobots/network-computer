#!/bin/sh
# Provision any reachable Ubuntu 24.04 box (any provider, or bare metal) by IP.
# Copies provision/ and runs apply.sh. This is the provider-agnostic core that
# every create-*.sh script calls once its instance is up.
#
#   [NC_DOMAIN=nc.example.com] infra/provision-host.sh <ip> [host-name] [ssh-user]
#
# NC_DOMAIN turns on a Let's Encrypt certificate; its A record must already
# point at <ip> (infra/dns-point.sh does that for DigitalOcean DNS).
set -e
IP=$1; NAME=${2:-cloudbox}; USER=${3:-root}
[ -n "$IP" ] || { echo "usage: provision-host.sh <ip> [host-name] [ssh-user]"; exit 1; }
ROOT=$(cd "$(dirname "$0")/.." && pwd)

echo "waiting for ssh on $IP..."
for i in $(seq 1 40); do
  ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 "$USER@$IP" true 2>/dev/null && break
  sleep 5
done

echo "copying provision/ and running apply.sh..."
ssh "$USER@$IP" 'mkdir -p /root/provision'
scp -q -r "$ROOT/provision/." "$USER@$IP":/root/provision/
# sudo only if not already root
PFX=""; [ "$USER" = root ] || PFX="sudo "
ssh "$USER@$IP" "NC_PUBLIC_IP=$IP NC_HOST_NAME=$NAME NC_DOMAIN=$NC_DOMAIN ${PFX}sh /root/provision/apply.sh"
echo
if [ -n "$NC_DOMAIN" ]; then URL="https://$NC_DOMAIN"; else URL="http://$IP:8765"; fi
echo "done. rendezvous: $URL   secrets: ssh $USER@$IP ${PFX}cat /etc/nc/env"
