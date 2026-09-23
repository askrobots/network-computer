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
# Host name: explicit NC_HOST_NAME, else what this box already recorded, else a
# default. Recording it means a later re-provision can never rename the host
# (which would strand clients and invalidate their pairing tokens).
if [ -z "$NC_HOST_NAME" ] && [ -f /etc/nc/env ]; then
  NC_HOST_NAME=$(sed -n 's/^NC_HOST_NAME=//p' /etc/nc/env)
fi
NC_HOST_NAME=${NC_HOST_NAME:-nc}
# Optional public hostname for a real (Let's Encrypt) certificate. Browsers only
# grant the microphone to secure pages, so this is what makes browser mic work.
if [ -z "$NC_DOMAIN" ] && [ -f /etc/nc/env ]; then
  NC_DOMAIN=$(sed -n 's/^NC_DOMAIN=//p' /etc/nc/env)
fi

# What each service depends on. apply.sh restarts a running service only when
# one of these changed, so re-provisioning takes effect without killing the
# desktop session for nothing.
deps() {
  case $1 in
    xorg)       echo /etc/systemd/system/nc-xorg.service /etc/X11/xorg.conf.d/10-dummy.conf ;;
    desktop)    echo /etc/systemd/system/nc-desktop.service ;;
    audio)      echo /etc/systemd/system/nc-audio.service /usr/local/bin/nc-audio-setup /etc/pulse/client.conf /etc/pulse/daemon.conf.d/nc.conf ;;
    rendezvous) echo /etc/systemd/system/nc-rendezvous.service /usr/local/bin/nc-rendezvous /etc/nc/env ;;
    host)       echo /etc/systemd/system/nc-host.service /usr/local/bin/nc-host /etc/nc/env ;;
  esac
}
snapshot() { for s in xorg desktop audio rendezvous host; do echo "$s $(cat $(deps $s) 2>/dev/null | md5sum | cut -c1-12)"; done; }
BEFORE=$(snapshot)

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
cp -a files/usr/. /usr/
# Ubuntu ships an override that re-enables per-user pulseaudio autospawn; we run
# one system pulse (nc-audio) and clients must attach to it, not spawn their own.
rm -f /etc/pulse/client.conf.d/01-enable-autospawn.conf
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
NC_HOST_NAME=${NC_HOST_NAME}
NC_DOMAIN=${NC_DOMAIN}
EOT
  chmod 600 /etc/nc/env
else
  sed -i "s/^NC_PUBLIC_IP=.*/NC_PUBLIC_IP=${NC_PUBLIC_IP}/" /etc/nc/env
  if grep -q '^NC_DOMAIN=' /etc/nc/env; then
    sed -i "s/^NC_DOMAIN=.*/NC_DOMAIN=${NC_DOMAIN}/" /etc/nc/env
  else
    echo "NC_DOMAIN=${NC_DOMAIN}" >> /etc/nc/env
  fi
  if grep -q '^NC_HOST_NAME=' /etc/nc/env; then
    sed -i "s/^NC_HOST_NAME=.*/NC_HOST_NAME=${NC_HOST_NAME}/" /etc/nc/env
  else
    echo "NC_HOST_NAME=${NC_HOST_NAME}" >> /etc/nc/env
  fi
fi

echo ">> firewall"
ufw allow 22/tcp >/dev/null; ufw allow 8765/tcp >/dev/null
ufw allow 80/tcp >/dev/null; ufw allow 443/tcp >/dev/null   # Let's Encrypt + https
ufw allow 3478/udp >/dev/null; ufw allow 49152:65535/udp >/dev/null
ufw --force enable >/dev/null

echo ">> services"
AFTER=$(snapshot)
systemctl daemon-reload
for s in xorg desktop audio rendezvous host; do
  was=$(echo "$BEFORE" | awk -v s=$s '$1==s{print $2}')
  now=$(echo "$AFTER"  | awk -v s=$s '$1==s{print $2}')
  if [ "$was" != "$now" ] && systemctl is-active --quiet nc-$s; then
    echo "   nc-$s changed, restarting"
    systemctl restart nc-$s
    # the host captures from pulse; give it a fresh start after audio restarts
    [ $s = audio ] && systemctl is-active --quiet nc-host && systemctl restart nc-host
  fi
done
systemctl enable --now nc-xorg nc-desktop nc-audio nc-rendezvous nc-host
sleep 4
systemctl is-active nc-xorg nc-desktop nc-audio nc-rendezvous nc-host | paste -sd' ' -
echo
echo ">> verify"
sleep 3
sh "$(dirname "$0")/verify.sh" || echo "!! verify found problems above"
echo
if [ -n "$NC_DOMAIN" ]; then echo "rendezvous: https://${NC_DOMAIN}"; else echo "rendezvous: http://${NC_PUBLIC_IP}:8765"; fi
cat /etc/nc/env
