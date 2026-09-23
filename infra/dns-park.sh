#!/bin/sh
# Park the A record <fqdn> at 127.0.0.1 (ttl 60) if it still points at <ip>.
# Run when a computer is destroyed. Two dangers this avoids:
#  - a record left pointing at a released IP: DigitalOcean may give that IP to
#    someone else, who could then serve (and get a certificate for) the name;
#  - deleting the record instead: resolvers cache "no such name" for the zone's
#    negative TTL (30 minutes on DigitalOcean), so clients that retried while
#    the desk was down could not reach it for up to 30 minutes after "up".
# 127.0.0.1 belongs to nobody else, and a 60 s positive answer is re-checked
# quickly once "up" points the record at the new computer.
#
#   infra/dns-park.sh nc.example.com 203.0.113.5
set -e
FQDN=${1:?usage: dns-park.sh <fqdn> <ip>}; IP=${2:?usage: dns-park.sh <fqdn> <ip>}
ZONE=$(doctl compute domain list --format Domain --no-header | awk -v f="$FQDN" '
  { z=$1; n=length(z)
    if (length(f) > n && substr(f, length(f)-n) == "." z && n > best) { best=n; zone=z } }
  END { print zone }')
[ -n "$ZONE" ] || { echo "no DigitalOcean DNS zone for $FQDN; update its A record at your provider"; exit 0; }
REC=${FQDN%."$ZONE"}
doctl compute domain records list "$ZONE" --format ID,Type,Name,Data --no-header |
  awk -v n="$REC" -v ip="$IP" '$2=="A" && $3==n && $4==ip {print $1}' |
  while read -r id; do
    doctl compute domain records update "$ZONE" --record-id "$id" --record-data 127.0.0.1 --record-ttl 60 >/dev/null
    echo "parked $FQDN at 127.0.0.1 (was $IP)"
  done
