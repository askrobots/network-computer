#!/bin/sh
# Create a Vultr instance in Dallas (dfw) and provision it.
# Needs: vultr-cli (brew install vultr-cli) and VULTR_API_KEY set,
# or ~/.vultr-cli.yaml. Get a key at https://my.vultr.com/settings/#settingsapi
# Usage: infra/create-vultr.sh [name] [region] [plan]
set -e
NAME=${1:-nc-dallas}; REGION=${2:-dfw}; PLAN=${3:-vc2-2c-4gb}
DIR=$(dirname "$0")
command -v vultr-cli >/dev/null || { echo "install vultr-cli: brew install vultr-cli"; exit 1; }

# newest Ubuntu 24.04 image id, and an SSH key id (upload your key if none)
OS=$(vultr-cli os list | awk '/Ubuntu 24.04/{print $1; exit}')
[ -n "$OS" ] || { echo "could not find Ubuntu 24.04 OS id"; exit 1; }
KEY=$(vultr-cli ssh-key list | awk 'NR==2{print $1}')
[ -n "$KEY" ] || { echo "no SSH key in Vultr. Add one:"; echo "  vultr-cli ssh-key create --name mac --key \"\$(cat ~/.ssh/id_ed25519.pub)\""; exit 1; }

echo "creating $NAME in $REGION ($PLAN)..."
ID=$(vultr-cli instance create --region "$REGION" --plan "$PLAN" --os "$OS" --host "$NAME" --ssh-keys "$KEY" | awk '/^ID/{print $2}')
echo "instance $ID, waiting for IP..."
IP=""
for i in $(seq 1 40); do
  IP=$(vultr-cli instance get "$ID" | awk '/^MAIN IP/{print $3}')
  [ -n "$IP" ] && [ "$IP" != "0.0.0.0" ] && break; sleep 5
done
echo "ip: $IP"
sh "$DIR/provision-host.sh" "$IP" "$NAME"
