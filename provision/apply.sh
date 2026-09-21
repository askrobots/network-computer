#!/bin/sh
# Provision (or re-provision) a network-computer host on Ubuntu 24.04.
# Idempotent: safe to run repeatedly. Run as root.
#
#   NC_PUBLIC_IP=<ip> NC_HOST_NAME=cloudbox provision/apply.sh
#
# Config lives as real files under provision/files/ and is copied into /.
# Edit those files, commit, and re-run to converge a box to the repo state.
set -e
cd "$(dirname "$0")"
export DEBIAN_FRONTEND=noninteractive
GO_VERSION=1.27.1
NC_PUBLIC_IP=${NC_PUBLIC_IP:-$(curl -s -4 ifconfig.me)}
NC_HOST_NAME=${NC_HOST_NAME:-cloudbox}

echo ">> packages"
apt-get update -q
grep -vE '^\s*#|^\s*$' packages.txt | xargs apt-get install -y -q

echo ">> Firefox (real deb from Mozilla, not the snap stub)"
install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://packages.mozilla.org/apt/repo-signing-key.gpg -o /etc/apt/keyrings/packages.mozilla.org.asc
echo "deb [signed-by=/etc/apt/keyrings/packages.mozilla.org.asc] https://packages.mozilla.org/apt mozilla main" > /etc/apt/sources.list.d/mozilla.list
printf 'Package: *\nPin: origin packages.mozilla.org\nPin-Priority: 1000\n' > /etc/apt/preferences.d/mozilla
apt-get update -q
apt-get install -y -q --allow-downgrades firefox

echo ">> config files"
cp -a files/etc/. /etc/
dconf update || true
sysctl -p /etc/sysctl.d/99-nc-noipv6.conf >/dev/null 2>&1 || true
modprobe uinput || true
echo uinput > /etc/modules-load.d/uinput.conf

echo ">> Go toolchain"
if ! /usr/local/go/bin/go version 2>/dev/null | grep -q "$GO_VERSION"; then
  curl -sL "https://go.dev/dl/go${GO_VERSION}.linux-$(dpkg --print-architecture).tar.gz" | tar -C /usr/local -xz
fi
export PATH=$PATH:/usr/local/go/bin

echo ">> build binaries"
if [ ! -d /opt/network-computer ]; then
  git clone -q https://github.com/askrobots/network-computer /opt/network-computer
else
  git -C /opt/network-computer pull -q
fi
( cd /opt/network-computer && go build -o /usr/local/bin/ ./cmd/... )

echo ">> secrets (generated once, kept in /etc/nc/env)"
if [ ! -f /etc/nc/env ]; then
  install -d /etc/nc
  cat > /etc/nc/env <<EOT
NC_PASSWORD=$(head -c 12 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 16)
NC_PIN=$(shuf -i 100000-999999 -n 1)
NC_PUBLIC_IP=${NC_PUBLIC_IP}
EOT
  chmod 600 /etc/nc/env
else
  sed -i "s/^NC_PUBLIC_IP=.*/NC_PUBLIC_IP=${NC_PUBLIC_IP}/" /etc/nc/env
fi
# host name is baked into the unit; swap it if NC_HOST_NAME differs
sed -i "s/-name cloudbox/-name ${NC_HOST_NAME}/" /etc/systemd/system/nc-host.service

echo ">> firewall"
ufw allow 22/tcp >/dev/null; ufw allow 8765/tcp >/dev/null
ufw allow 3478/udp >/dev/null; ufw allow 49152:65535/udp >/dev/null
ufw --force enable >/dev/null

echo ">> services"
systemctl daemon-reload
systemctl enable --now nc-xorg nc-desktop nc-audio nc-rendezvous nc-host
sleep 4
systemctl is-active nc-xorg nc-desktop nc-audio nc-rendezvous nc-host | paste -sd' ' -
echo
echo "rendezvous: http://${NC_PUBLIC_IP}:8765"
cat /etc/nc/env
