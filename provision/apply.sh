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
# nothing yet on a brand-new desk (the file is created below): empty, not an error
envget() { if [ -f /etc/nc/env ]; then sed -n "s/^$1=//p" /etc/nc/env; fi; }

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
    object-server) echo /etc/systemd/system/nc-object-server.service /etc/systemd/system/nc-object-server.service.d/*.conf /etc/nc/object-server.env /opt/dbbasic-object-server/.git/HEAD /opt/dbbasic-object-server/.git/refs/heads/main ;;
    object-daemon) echo /etc/systemd/system/nc-object-daemon.service /etc/systemd/system/nc-object-daemon.service.d/*.conf /etc/nc/object-server.env /opt/dbbasic-object-server/.git/refs/heads/main ;;
    voice)      echo /etc/systemd/system/nc-voice.service /etc/systemd/system/nc-voice.service.d/*.conf /usr/local/bin/nc-voice /etc/nc/object-server.env /opt/piper/voices/en_US-lessac-medium.onnx.json ;;
  esac
}
snapshot() { for s in xorg desktop audio rendezvous host object-server object-daemon voice; do echo "$s $(cat $(deps $s) 2>/dev/null | md5sum | cut -c1-12)"; done; }
BEFORE=$(snapshot)

echo ">> memory: swap, so a spike slows the desk down instead of freezing it"
# Cloud images come without swap. With 4 GB and none, running out of memory
# did not kill anything: the kernel evicted and re-read program files until
# ssh, the desktop and the web page all stopped answering (2026-09-25; a
# Flutter build next to five video streams). Swap as large as the memory, at
# most 4 GB, on the computer's own disk (not the desk volume).
if ! swapon --show=NAME --noheadings | grep -q .; then
  MEM=$(awk '/MemTotal/{print int($2/1024)}' /proc/meminfo); SZ=$(( MEM < 4096 ? MEM : 4096 ))
  [ -f /swapfile ] || { fallocate -l ${SZ}M /swapfile && chmod 600 /swapfile && mkswap -q /swapfile; }
  swapon /swapfile
fi
grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
# prefer keeping programs in memory over file cache, but use swap before freezing
printf 'vm.swappiness=20\n' > /etc/sysctl.d/60-nc-memory.conf; sysctl -q -p /etc/sysctl.d/60-nc-memory.conf

echo ">> packages"
$APT update -q
# before any install: package upgrades must never restart the desk's own
# services (needrestart would, closing every window of the session)
install -Dm644 files/etc/needrestart/conf.d/nc.conf /etc/needrestart/conf.d/nc.conf
grep -vE '^\s*#|^\s*$' packages.txt | xargs $APT install -y -q

echo ">> memory: earlyoom stops the biggest runaway before the desk locks up"
# Never the desk's own services, the desktop or ssh; builds and browser tabs first.
cat > /etc/default/earlyoom <<'EOF'
# network-computer (provision/apply.sh): act at 5% memory and 10% swap left
EARLYOOM_ARGS="-m 5 -s 10 -r 3600 --avoid ^(nc-host|nc-rendezvous|sshd|Xorg|xfce4-session|xfwm4|xfce4-panel|systemd|pulseaudio|ffmpeg)$ --prefer ^(dart|dartaotruntime|gen_snapshot|flutter|clang|ld|WebKitWebProces|Isolated.Web.Co|Web.Content)$"
EOF
systemctl enable earlyoom >/dev/null 2>&1; systemctl restart earlyoom

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
udevadm control --reload-rules 2>/dev/null || true
udevadm trigger --subsystem-match=block 2>/dev/null || true   # re-evaluate disks (hide the config drive)
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
( cd /opt/network-computer && go build -buildvcs=false -o /usr/local/bin/ ./cmd/... )

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
[ -z "$NC_SIZE" ] || envset NC_SIZE "$NC_SIZE"
[ -z "$NC_PRICE_HOURLY" ] || envset NC_PRICE_HOURLY "$NC_PRICE_HOURLY"
# what the desktop dashboard may show: no secrets (the env file holds passwords)
( . /etc/nc/env; umask 022; printf 'NC_HOST_NAME=%s\nNC_DOMAIN=%s\nNC_PUBLIC_IP=%s\nNC_SIZE=%s\nNC_PRICE_HOURLY=%s\n' \
    "$NC_HOST_NAME" "$NC_DOMAIN" "$NC_PUBLIC_IP" "$NC_SIZE" "$NC_PRICE_HOURLY" > /etc/nc/info )

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
  # the Trash for files on the desk (gio won't create it at the top of a volume)
  DUID=$(id -u "$NC_DESK_USER")
  install -d -o "$NC_DESK_USER" -g "$NC_DESK_USER" -m 0700 "/desk/.Trash-$DUID"
fi
# AI keys: ~/.config/dbbasic/ai.env, where every dbbasic app looks for them
# (docs/DBBASIC-APPS.md); on a desk it lives on the desk through ~/.config.
# /desk/secrets/env, where they used to be, becomes a link to it.
AIENV="$UHOME/.config/dbbasic/ai.env"
OLDSEC=/etc/nc/secrets.env; [ -n "$DESK" ] && OLDSEC=/desk/secrets/env
runuser -u "$NC_DESK_USER" -- mkdir -p "$UHOME/.config/dbbasic"
chmod 700 "$UHOME/.config/dbbasic"
if [ ! -f "$AIENV" ]; then
  if [ -f "$OLDSEC" ] && [ ! -L "$OLDSEC" ]; then
    cp -p "$OLDSEC" "$AIENV" && rm -f "$OLDSEC" && echo "   AI keys moved to ~/.config/dbbasic/ai.env"
  else
    printf '%s\n' "# AI keys for every dbbasic app on this desk: ANTHROPIC_API_KEY=... OPENAI_API_KEY=..." \
      "# optional: DBBASIC_ANTHROPIC_MODEL=... DBBASIC_OPENAI_MODEL=...  (keep this file 0600)" > "$AIENV"
  fi
fi
chown "$NC_DESK_USER:$NC_DESK_USER" "$AIENV"; chmod 600 "$AIENV"
[ -L "$OLDSEC" ] || rm -f "$OLDSEC"
ln -sfn "$(readlink -f "$AIENV")" "$OLDSEC"
# the file manager's "Send to my device" (nc-send); added once, the user's other actions kept
UCA="$UHOME/.config/Thunar/uca.xml"
if ! grep -q 'nc-send' "$UCA" 2>/dev/null; then
  runuser -u "$NC_DESK_USER" -- mkdir -p "$UHOME/.config/Thunar"
  [ -f "$UCA" ] || { cp /etc/xdg/Thunar/uca.xml "$UCA" 2>/dev/null || printf '<?xml version="1.0" encoding="UTF-8"?>\n<actions>\n</actions>\n' > "$UCA"; }
  awk '/<\/actions>/{print "<action><icon>document-send</icon><name>Send to my device</name><submenu></submenu><unique-id>nc-send</unique-id><command>nc-send %F</command><description>Download on the device you are connected from</description><range>*</range><patterns>*</patterns><other-files/><text-files/><image-files/><audio-files/><video-files/></action>"}{print}' "$UCA" > "$UCA.new" && mv "$UCA.new" "$UCA"
  chown "$NC_DESK_USER:$NC_DESK_USER" "$UCA"
fi
# ... and "Print on my device" for documents and pictures
if ! grep -q 'nc-print' "$UCA" 2>/dev/null; then
  awk '/<\/actions>/{print "<action><icon>printer</icon><name>Print on my device</name><submenu></submenu><unique-id>nc-print</unique-id><command>lp -d my-device %F</command><description>Opens the print dialog of the device you are connected from</description><range>*</range><patterns>*.pdf;*.PDF;*.txt;*.png;*.jpg;*.jpeg;*.PNG;*.JPG;*.JPEG</patterns><text-files/><image-files/><other-files/></action>"}{print}' "$UCA" > "$UCA.new" && mv "$UCA.new" "$UCA"
  chown "$NC_DESK_USER:$NC_DESK_USER" "$UCA"
fi

echo ">> camera: the connected device's camera as a webcam here"
# v4l2loopback ships with Ubuntu's kernel modules, but the video4linux core it
# needs (videodev) is in the "extra" modules, which cloud images leave out: those
# for the running kernel, and the metapackage that follows kernel updates
if ! modinfo videodev >/dev/null 2>&1 || ! modinfo v4l2loopback >/dev/null 2>&1; then
  $APT install -y -q "linux-modules-extra-$(uname -r)" linux-image-extra-virtual >/dev/null 2>&1 ||
    $APT install -y -q v4l2loopback-dkms >/dev/null 2>&1 || true
fi
if [ -e /dev/video10 ] && [ "$(cat /sys/class/video4linux/video10/name 2>/dev/null)" != "network-computer camera" ]; then
  modprobe -r v4l2loopback 2>/dev/null || true   # loaded before with other options
fi
modprobe v4l2loopback || echo "   (no v4l2loopback: the camera stays off)"
# the desk user's apps open it; group video for later logins, ownership for the running session
usermod -aG video "$NC_DESK_USER"
# keep_format: this v4l2loopback (exclusive_caps) is otherwise stuck after the
# first writer closes, and every later camera fails to open it
printf 'SUBSYSTEM=="video4linux", ATTR{name}=="network-computer camera", OWNER="%s", GROUP="video", MODE="0660", RUN+="/usr/bin/v4l2-ctl -d $devnode -c keep_format=1"\n' "$NC_DESK_USER" > /etc/udev/rules.d/70-nc-camera.rules
udevadm control --reload; udevadm trigger --subsystem-match=video4linux 2>/dev/null || true

echo ">> printing: \"My device\" prints on the device you are connected from"
# CUPS (local only), a queue whose backend (ncdevice) hands each job as a PDF
# to nc-host, and it is the default printer: File > Print in any app reaches
# the phone's, iPad's or Mac's own print dialog.
chmod 0700 /usr/lib/cups/backend/ncdevice   # root: needs the send socket
systemctl enable --now cups.socket cups.service >/dev/null 2>&1 || true
# (re)applied every run: CUPS keeps its own copy of the PPD
lpadmin -p my-device -E -v ncdevice:/ -P /usr/share/ppd/nc/my-device.ppd \
  -D "My device" -L "the device you are connected from" -o printer-error-policy=abort-job 2>/dev/null
lpadmin -d my-device
echo ">> personal settings (from the profile; applied once, never over the user's own changes)"
PERSON="$(dirname "$0")/person.env"
if [ -f "$PERSON" ]; then
  (
    . "$PERSON"
    if [ -n "$NC_TZ" ] && echo "$NC_TZ" | grep -Eq '^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$' \
         && [ -e "/usr/share/zoneinfo/$NC_TZ" ]; then
      timedatectl set-timezone "$NC_TZ" && echo "   time zone $NC_TZ"
    fi
    if [ -n "$NC_EXTRA_PACKAGES" ]; then
      pk=$(echo "$NC_EXTRA_PACKAGES" | tr ' ,' '\n\n' | grep -E '^[a-z0-9][a-z0-9.+-]+$' | tr '\n' ' ')
      [ -z "$pk" ] || { $APT install -y -q $pk >/dev/null && echo "   extra apps: $pk"; }
    fi
    SEC=$(readlink -f "$AIENV")
    for k in ANTHROPIC_API_KEY OPENAI_API_KEY; do
      eval v=\$NC_$k
      if [ -n "$v" ] && ! grep -q "^$k=." "$SEC"; then
        printf '%s=%s\n' "$k" "$v" >> "$SEC" && echo "   $k added to ~/.config/dbbasic/ai.env"
      fi
    done
    chown "$NC_DESK_USER:$NC_DESK_USER" "$SEC"; chmod 600 "$SEC"
    # the voice style waits for the object server (nc-object-bootstrap, below)
    [ -z "$NC_VOICE_STYLE" ] || printf '%s' "$NC_VOICE_STYLE" > /etc/nc/voice-style.initial
    # a welcome note on their Desktop, once
    MARK="$UHOME/.config/nc/welcomed"
    if [ ! -e "$MARK" ]; then
      WHO=${NC_WELCOME_NAME:-$NC_DESK_USER}
      runuser -u "$NC_DESK_USER" -- mkdir -p "$UHOME/Desktop" "$UHOME/.config/nc"
      cat > "$UHOME/Desktop/Welcome.txt" <<EOT
Welcome to your desk, $WHO.

This computer lives in the cloud; your desk (settings, Documents, Desktop, keys)
is kept even when the computer is replaced.

  Search and launch          Alt+Space (or the magnifier button at the top)
  Talk to it                 the round microphone button, bottom left:
                             "open Firefox", "put Firefox on the left",
                             "what's on my screen?", "remind me at 3 to...",
                             "take a note: ...", "be more brief"
  Copy and paste             Cmd/Ctrl+C and V work between your device and here
  Files                      drop files on the window; right-click a file here and
                             choose "Send to my device" to get it back
  Your apps and notes        "Object Server" on the Desktop

Connect from any browser at ${NC_DOMAIN:+https://$NC_DOMAIN}, or the Network Computer app.
EOT
      chown "$NC_DESK_USER:$NC_DESK_USER" "$UHOME/Desktop/Welcome.txt"
      runuser -u "$NC_DESK_USER" -- touch "$MARK"
      echo "   welcome note for $WHO"
    fi
  )
  rm -f "$PERSON"
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
echo ">> object server (voice, AI; local only, as $NC_DESK_USER)"
OS=/opt/dbbasic-object-server
if [ ! -d $OS/.git ]; then
  git clone -q https://github.com/askrobots/dbbasic-object-server $OS
else
  git -C $OS pull -q
fi
[ -x $OS/.venv/bin/uvicorn ] || python3 -m venv $OS/.venv
$OS/.venv/bin/pip install -q -e "$OS[server]"   # editable: the package list omits some modules
OSDATA=/var/lib/nc-object-server; [ -n "$DESK" ] && OSDATA=/desk/object-server
install -d -o "$NC_DESK_USER" -g "$NC_DESK_USER" -m 0700 "$OSDATA" "$OSDATA/data" "$OSDATA/objects"
OSENV=/etc/nc/object-server.env
if [ -n "$DESK" ]; then
  mkdir -p /desk/nc
  [ -s /desk/nc/object-server.env ] || { [ -f $OSENV ] && [ ! -L $OSENV ] && mv $OSENV /desk/nc/object-server.env; }
  ln -sfn /desk/nc/object-server.env $OSENV
fi
if [ ! -s $OSENV ]; then
  umask 077
  cat > $OSENV <<EOT
# the desk's object server; the admin token opens sessions for nc-voice
DBBASIC_ADMIN_TOKEN=$(head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 32)
EOT
  umask 022
fi
oset() { grep -q "^$1=" $OSENV && sed -i --follow-symlinks "s|^$1=.*|$1=$2|" $OSENV || echo "$1=$2" >> $OSENV; }
oset DBBASIC_DATA_DIR "$OSDATA/data"
oset DBBASIC_OBJECTS_DIR "$OSDATA/objects"
oset DBBASIC_PACKAGES_DIR "$OS/packages"
oset NC_DESK_USER "$NC_DESK_USER"
for f in ENABLE_AI_CHAT ENABLE_TTS ENABLE_STT ENABLE_READER ENABLE_SITE_ROUTES ENABLE_PACKAGE_INSTALLS ENABLE_USER_FILES \
         ENABLE_PASSWORD_LOGIN ENABLE_PERMISSION_ENFORCEMENT ENABLE_WEBDAV; do oset DBBASIC_$f true; done
oset DBBASIC_COOKIE_SECURE false
chmod 600 "$(readlink -f $OSENV)"   # root only: systemd reads it before switching to the user
echo ">> Piper (local text to speech for nc-voice)"
[ -x /opt/piper/bin/python ] || python3 -m venv /opt/piper
/opt/piper/bin/python -c 'import piper' 2>/dev/null || /opt/piper/bin/pip install -q piper-tts
install -d /opt/piper/voices
[ -s /opt/piper/voices/en_US-lessac-medium.onnx ] || ( cd /opt/piper/voices && /opt/piper/bin/python -m piper.download_voices en_US-lessac-medium >/dev/null 2>&1 )
for s in nc-object-server nc-object-daemon nc-voice; do
  install -d /etc/systemd/system/$s.service.d
  printf '[Service]\nUser=%s\nGroup=%s\n' "$NC_DESK_USER" "$NC_DESK_USER" > /etc/systemd/system/$s.service.d/user.conf
done

# services that read the desk wait for it at boot
for s in nc-desktop nc-host nc-rendezvous nc-object-server nc-object-daemon nc-voice nc-object-files; do
  install -d /etc/systemd/system/$s.service.d
  if [ -n "$DESK" ]; then
    printf '[Unit]\nRequiresMountsFor=/desk\n' > /etc/systemd/system/$s.service.d/desk.conf
  else
    rm -f /etc/systemd/system/$s.service.d/desk.conf
  fi
done

echo ">> firewall"
ufw allow 22/tcp >/dev/null
# with a domain the rendezvous is https on 443 and 8765 listens on loopback only
if [ -n "$NC_DOMAIN" ]; then ufw delete allow 8765/tcp >/dev/null 2>&1 || true; else ufw allow 8765/tcp >/dev/null; fi
ufw allow 80/tcp >/dev/null; ufw allow 443/tcp >/dev/null   # Let's Encrypt + https
ufw allow 3478/udp >/dev/null; ufw allow 49152:65535/udp >/dev/null
ufw --force enable >/dev/null
# nothing on a desk needs network discovery; CUPS stays for "My device" and
# listens on localhost only (verify checks 631 is not public)
for u in avahi-daemon.socket avahi-daemon.service cups-browsed.service; do
  systemctl disable --now "$u" >/dev/null 2>&1 || true
done

echo ">> services"
AFTER=$(snapshot)
systemctl daemon-reload
for s in xorg desktop audio rendezvous host object-server object-daemon voice; do
  was=$(echo "$BEFORE" | awk -v s=$s '$1==s{print $2}')
  now=$(echo "$AFTER"  | awk -v s=$s '$1==s{print $2}')
  if [ "$was" != "$now" ] && systemctl is-active --quiet nc-$s; then
    echo "   nc-$s changed, restarting"
    systemctl restart nc-$s
    # the host captures from pulse; give it a fresh start after audio restarts
    [ $s = audio ] && systemctl is-active --quiet nc-host && systemctl restart nc-host
  fi
done
systemctl enable --now nc-xorg nc-desktop nc-audio nc-rendezvous nc-host nc-object-server nc-object-daemon nc-voice
sleep 4
systemctl is-active nc-xorg nc-desktop nc-audio nc-rendezvous nc-host nc-object-server nc-object-daemon nc-voice | paste -sd' ' -
nc-object-bootstrap || echo "!! object server bootstrap failed"
# the dbbasic apps built onto this desk (infra/apps.sh): menu entries, icons, file types
nc-apps install || echo "!! dbbasic apps not installed"
# the object server's files as ~/Objects (needs the key the bootstrap minted)
systemctl enable nc-object-files >/dev/null 2>&1
systemctl restart nc-object-files && nc-object-files status | sed 's/^/   ~\/Objects: /' || echo "!! ~/Objects did not mount"
systemctl enable --now nc-health.timer >/dev/null 2>&1   # restarts anything that stops
# session settings (Alt+Space...) apply at every login; apply them to the running session now
for i in 1 2 3 4 5; do nc-session-setup >/dev/null 2>&1 && break; sleep 2; done
echo
echo ">> verify"
sleep 3
sh "$(dirname "$0")/verify.sh" || echo "!! verify found problems above"
echo
if [ -n "$NC_DOMAIN" ]; then echo "rendezvous: https://${NC_DOMAIN}"; else echo "rendezvous: http://${NC_PUBLIC_IP}:8765"; fi
echo "login and PIN: infra/droplet.sh creds  (stored in /etc/nc/env${DESK:+ -> /desk/nc/env})"
