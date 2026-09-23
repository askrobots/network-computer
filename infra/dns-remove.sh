#!/bin/sh
# Remove the A record <fqdn> from DigitalOcean DNS, but only if it points at <ip>.
# Run when a droplet is destroyed: a record left pointing at an IP that
# DigitalOcean later gives to someone else would let them serve (and get a
# certificate for) your hostname. The IP check means we never remove a record
# that has already been re-pointed at a newer droplet.
#
#   infra/dns-remove.sh nc.example.com 203.0.113.5
set -e
FQDN=${1:?usage: dns-remove.sh <fqdn> <ip>}; IP=${2:?usage: dns-remove.sh <fqdn> <ip>}
ZONE=$(doctl compute domain list --format Domain --no-header | awk -v f="$FQDN" '
  { z=$1; n=length(z)
    if (length(f) > n && substr(f, length(f)-n) == "." z && n > best) { best=n; zone=z } }
  END { print zone }')
[ -n "$ZONE" ] || { echo "no DigitalOcean DNS zone for $FQDN; remove its A record at your provider"; exit 0; }
REC=${FQDN%."$ZONE"}
doctl compute domain records list "$ZONE" --format ID,Type,Name,Data --no-header |
  awk -v n="$REC" -v ip="$IP" '$2=="A" && $3==n && $4==ip {print $1}' |
  while read -r id; do
    doctl compute domain records delete "$ZONE" "$id" --force
    echo "removed $FQDN -> $IP"
  done
