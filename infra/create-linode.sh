#!/bin/sh
# Create a Linode (Akamai) in Dallas (us-central) and provision it.
# Needs: linode-cli (brew install linode-cli), configured with a token
# (linode-cli configure). Token: https://cloud.linode.com/profile/tokens
# Usage: infra/create-linode.sh [name] [region] [type]
set -e
NAME=${1:-nc-dallas}; REGION=${2:-us-central}; TYPE=${3:-g6-standard-2}
DIR=$(dirname "$0")
command -v linode-cli >/dev/null || { echo "install linode-cli: brew install linode-cli && linode-cli configure"; exit 1; }
KEY=$(cat ~/.ssh/id_ed25519.pub)

echo "creating $NAME in $REGION ($TYPE)..."
IP=$(linode-cli linodes create --label "$NAME" --region "$REGION" --type "$TYPE" \
  --image linode/ubuntu24.04 --root_pass "$(head -c16 /dev/urandom | base64)" \
  --authorized_keys "$KEY" --text --no-headers --format ipv4)
echo "ip: $IP"
sh "$DIR/provision-host.sh" "$IP" "$NAME"
