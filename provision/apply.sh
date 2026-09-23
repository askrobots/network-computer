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
# A fresh cloud image runs its own first-boot package updates; wait for them,
# and make every apt call wait for the dpkg lock instead of failing on it.
if command -v cloud-init >/dev/null 2>&1; then cloud-init status --wait >/dev/null 2>&1 || true; fi
APT="apt-get -o DPkg::Lock::Timeout=600"
GO_VERSION=1.27.1
NC_PUBLIC_IP=${NC_PUBLIC_IP:-$(curl -s -4 ifconfig.me)}
# ---- the desk ---------------------------------------------------------------
# A DigitalOcean volume named desk-* attached to this computer holds everything
# that must outlive it: the desk's identity (login, PIN, domain), the host's
# pairing secret, the rendezvous certificate cache, and the desk user's locker
# (settings, documents, keys). Without one, everything stays local.
DESK=""
DESK_DEV=$(ls /dev/disk/by-id/scsi-0DO_Volume_desk-* 2>/dev/null | head -1)
if [ -n "$DESK_DEV" ]; then
  echo ">> desk ($DESK_DEV)"
  # DigitalOcean may auto-mount a formatted volume under /mnt; keep it only at /desk
  for m in $(findmnt -rn -S "$(readlink -f "$DESK_DEV")" -o TARGET 2>/dev/null); do
    [ "$m" = /desk ] || umount "$m"
  done
  sed -i "\|^$DESK_DEV |d; \|/mnt/desk_|d" /etc/fstab
  echo "$DESK_DEV /desk ext4 defaults,nofail,discard,noatime 0 2" >> /etc/fstab
  mkdir -p /desk
  mountpoint -q /desk || mount /desk
  install -d -m 0755 /desk/nc
  install -d -m 0700 /desk/nc/host /desk/nc/rendezvous
  # identity: /etc/nc/env points at the desk; migrate a local one the first time
  install -d /etc/nc
  if [ -f /etc/nc/env ] && [ ! -L /etc/nc/env ] && [ ! -f /desk/nc/env ]; then mv /etc/nc/env /desk/nc/env; fi
  rm -f /etc/nc/env; ln -s /desk/nc/env /etc/nc/env
  # host pairing secret and rendezvous certificate cache
  for d in host rendezvous; do
    if [ -d /var/lib/nc-$d ] && [ ! -L /var/lib/nc-$d ]; then
      cp -an /var/lib/nc-$d/. /desk/nc/$d/ 2>/dev/null || true; rm -rf /var/lib/nc-$d
    fi
    ln -sfn /desk/nc/$d /var/lib/nc-$d
  done
  DESK=1
fi
# edits to /etc/nc/env must follow the symlink, or sed -i would replace it with
# a plain file and the desk's identity would silently stop being used
envset() {  # envset KEY VALUE
  if grep -q "^$1=" /etc/nc/env 2>/dev/null; then sed -i --follow-symlinks "s|^$1=.*|$1=$2|" /etc/nc/env
  else echo "$1=$2" >> /etc/nc/env; fi
}
envget() { sed -n "s/^$1=//p" /etc/nc/env 2>/dev/null; }

# Host name: explicit NC_HOST_NAME, else what this box already recorded, else a
# default. Recording it means a later re-provision can never rename the host
# (which would strand clients and invalidate their pairing tokens).
[ -n "$NC_HOST_NAME" ] || NC_HOST_NAME=$(envget NC_HOST_NAME)
NC_HOST_NAME=${NC_HOST_NAME:-nc}
# Optional public hostname for a real (Let's Encrypt) certificate. Browsers only
# grant the microphone to secure pages, so this is what makes browser mic work.
[ -n "$NC_DOMAIN" ] || NC_DOMAIN=$(envget NC_DOMAIN)
# The desktop account (not root). Recorded like the others.
[ -n "$NC_DESK_USER" ] || NC_DESK_USER=$(envget NC_DESK_USER)
NC_DESK_USER=${NC_DESK_USER:-user}
# Keyboard layout for the desktop, e.g. us:dvorak. Clients also set it on connect.
[ -n "$NC_KEYBOARD" ] || NC_KEYBOARD=$(envget NC_KEYBOARD)

# What each service depends on. apply.sh restarts a running service only when
# one of these changed, so re-provisioning takes effect without killing the
# desktop session for nothing.
deps() {
  case $1 in
    xorg)       echo /etc/systemd/system/nc-xorg.service /etc/X11/xorg.conf.d/10-dummy.conf ;;
    desktop)    echo /etc/systemd/system/nc-desktop.service /etc/systemd/system/nc-desktop.service.d/*.conf ;;
    audio)      echo /etc/systemd/system/nc-audio.service /usr/local/bin/nc-audio-setup /etc/pulse/client.conf /etc/pulse/daemon.conf.d/nc.conf ;;
    rendezvous) echo /etc/systemd/system/nc-rendezvous.service /etc/systemd/system/nc-rendezvous.service.d/*.conf /usr/local/bin/nc-rendezvous /etc/nc/env ;;
    host)       echo /etc/systemd/system/nc-host.service /etc/systemd/system/nc-host.service.d/*.conf /usr/local/bin/nc-host /etc/nc/env ;;
  esac
}
snapshot() { for s in xorg desktop audio rendezvous host; do echo "$s $(cat $(deps $s) 2>/dev/null | md5sum | cut -c1-12)"; done; }
BEFORE=$(snapshot)

echo ">> packages"
$APT update -q
grep -vE '^\s*#|^\s*$' packages.txt | xargs $APT install -y -q

echo ">> Firefox (real deb from Mozilla, not the snap stub)"
install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://packages.mozilla.org/apt/repo-signing-key.gpg -o /etc/apt/keyrings/packages.mozilla.org.asc
echo "deb [signed-by=/etc/apt/keyrings/packages.mozilla.org.asc] https://packages.mozilla.org/apt mozilla main" > /etc/apt/sources.list.d/mozilla.list
printf 'Package: *\nPin: origin packages.mozilla.org\nPin-Priority: 1000\n' > /etc/apt/preferences.d/mozilla
$APT update -q
$APT install -y -q --allow-downgrades firefox

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

echo ">> identity (generated once; on the desk when there is one)"
install -d /etc/nc
if [ ! -s /etc/nc/env ]; then
  umask 077
  cat > /etc/nc/env <<EOT
NC_PASSWORD=$(head -c 12 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 16)
NC_PIN=$(shuf -i 100000-999999 -n 1)
EOT
  umask 022
fi
chmod 600 /etc/nc/env
envset NC_PUBLIC_IP "$NC_PUBLIC_IP"
envset NC_HOST_NAME "$NC_HOST_NAME"
envset NC_DOMAIN "$NC_DOMAIN"
envset NC_DESK_USER "$NC_DESK_USER"
[ -z "$NC_KEYBOARD" ] || envset NC_KEYBOARD "$NC_KEYBOARD"

echo ">> desktop user ($NC_DESK_USER)"
if ! id "$NC_DESK_USER" >/dev/null 2>&1; then
  # a fixed uid keeps files on the desk owned correctly across rebuilds
  if getent passwd 1000 >/dev/null; then
    echo "!! uid 1000 already belongs to $(getent passwd 1000 | cut -d: -f1)"; exit 1
  fi
  useradd -m -u 1000 -U -s /bin/bash "$NC_DESK_USER"
fi
UHOME=$(getent passwd "$NC_DESK_USER" | cut -d: -f6)
if [ -n "$DESK" ]; then
  # the locker: only these live on the desk; downloads and caches stay local
  install -d -o "$NC_DESK_USER" -g "$NC_DESK_USER" -m 0755 /desk/home
  install -d -o "$NC_DESK_USER" -g "$NC_DESK_USER" -m 0700 /desk/secrets
  for p in Documents Desktop .config .local/share/keyrings; do
    install -d -o "$NC_DESK_USER" -g "$NC_DESK_USER" "/desk/home/$p"
    runuser -u "$NC_DESK_USER" -- mkdir -p "$(dirname "$UHOME/$p")"
    if [ -e "$UHOME/$p" ] && [ ! -L "$UHOME/$p" ]; then
      cp -an "$UHOME/$p/." "/desk/home/$p/" 2>/dev/null || true; rm -rf "$UHOME/$p"
    fi
    ln -sfn "/desk/home/$p" "$UHOME/$p"; chown -h "$NC_DESK_USER:$NC_DESK_USER" "$UHOME/$p"
  done
  chown -R "$NC_DESK_USER:$NC_DESK_USER" /desk/home /desk/secrets
  if [ ! -f /desk/secrets/env ]; then
    install -m 0600 -o "$NC_DESK_USER" -g "$NC_DESK_USER" /dev/null /desk/secrets/env
    echo "# API keys for this desk, e.g. ANTHROPIC_API_KEY=... (lives on the desk, 0600)" > /desk/secrets/env
  fi
fi
# the desktop session runs as the desk user
install -d /etc/systemd/system/nc-desktop.service.d
cat > /etc/systemd/system/nc-desktop.service.d/user.conf <<EOT
[Service]
User=$NC_DESK_USER
Group=$NC_DESK_USER
Environment=HOME=$UHOME
WorkingDirectory=$UHOME
EOT
# services that read the desk wait for it at boot
for s in nc-desktop nc-host nc-rendezvous; do
  install -d /etc/systemd/system/$s.service.d
  if [ -n "$DESK" ]; then
    printf '[Unit]\nRequiresMountsFor=/desk\n' > /etc/systemd/system/$s.service.d/desk.conf
  else
    rm -f /etc/systemd/system/$s.service.d/desk.conf
  fi
done

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
echo "login and PIN: infra/droplet.sh creds  (stored in /etc/nc/env${DESK:+ -> /desk/nc/env})"
