#!/bin/sh
# Point <fqdn> at <ip> with an A record in DigitalOcean DNS (create or update).
# Needs the domain's DNS hosted on DigitalOcean and a doctl token with write
# access to domains. TTL is 60s because a droplet's IP changes on every rebuild.
#
#   infra/dns-point.sh nc.example.com 203.0.113.5
set -e
FQDN=${1:?usage: dns-point.sh <fqdn> <ip>}; IP=${2:?usage: dns-point.sh <fqdn> <ip>}
# the longest DigitalOcean zone that is a suffix of the name
ZONE=$(doctl compute domain list --format Domain --no-header | awk -v f="$FQDN" '
  { z=$1; n=length(z)
    if (length(f) > n && substr(f, length(f)-n) == "." z && n > best) { best=n; zone=z } }
  END { print zone }')
[ -n "$ZONE" ] || {
  echo "no DigitalOcean DNS zone covers $FQDN."
  echo "Either host the domain's DNS on DigitalOcean (see docs/DOMAIN.md), or create the"
  echo "A record $FQDN -> $IP at your DNS provider yourself and re-run with NC_DOMAIN set."
  exit 1
}
REC=${FQDN%."$ZONE"}
ID=$(doctl compute domain records list "$ZONE" --format ID,Type,Name --no-header | awk -v n="$REC" '$2=="A" && $3==n{print $1; exit}')
if [ -n "$ID" ]; then
  doctl compute domain records update "$ZONE" --record-id "$ID" --record-data "$IP" --record-ttl 60 >/dev/null
else
  doctl compute domain records create "$ZONE" --record-type A --record-name "$REC" --record-data "$IP" --record-ttl 60 >/dev/null
fi
echo "$FQDN -> $IP (A record in $ZONE, ttl 60)"
