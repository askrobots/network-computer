#!/bin/sh
# Turns a fresh Ubuntu 24.04 box into a test rig: nc-rendezvous with a public IP,
# and a headless X desktop streamed by nc-host, both as systemd services.
# Run as root. Set NC_PUBLIC_IP to the box's public address.
set -e
export DEBIAN_FRONTEND=noninteractive
NC_PUBLIC_IP=${NC_PUBLIC_IP:-$(curl -s -4 ifconfig.me)}
GO_VERSION=1.27.1

apt-get update -q
apt-get install -y -q ffmpeg xserver-xorg-core xserver-xorg-video-dummy xserver-xorg-input-evdev \
  xfce4 xfce4-terminal xterm dbus-x11 git curl ufw pulseaudio pulseaudio-utils xdg-utils xdotool

# Firefox as a real deb from Mozilla (Ubuntu's package is a snap stub that misbehaves as root)
install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://packages.mozilla.org/apt/repo-signing-key.gpg -o /etc/apt/keyrings/packages.mozilla.org.asc
echo "deb [signed-by=/etc/apt/keyrings/packages.mozilla.org.asc] https://packages.mozilla.org/apt mozilla main" > /etc/apt/sources.list.d/mozilla.list
printf "Package: *\nPin: origin packages.mozilla.org\nPin-Priority: 1000\n" > /etc/apt/preferences.d/mozilla
apt-get update -q && apt-get install -y -q firefox

# audio: pulseaudio as a service with a virtual sink; every client finds it via client.conf
printf "default-server = unix:/run/nc-pulse/native\nautospawn = no\n" > /etc/pulse/client.conf
rm -f /etc/pulse/client.conf.d/01-enable-autospawn.conf
cat > /etc/systemd/system/nc-audio.service <<'EOT'
[Unit]
Description=pulseaudio with a virtual sink for network-computer
[Service]
Environment=HOME=/root
RuntimeDirectory=nc-pulse
RuntimeDirectoryMode=0755
ExecStart=/usr/bin/pulseaudio --daemonize=no --exit-idle-time=-1 --disallow-exit --load="module-native-protocol-unix auth-anonymous=1 socket=/run/nc-pulse/native"
ExecStartPost=/bin/sh -c "sleep 2; pactl load-module module-null-sink sink_name=nc sink_properties=device.description=network-computer; pactl set-default-sink nc"
Restart=always
[Install]
WantedBy=multi-user.target
EOT

# Go toolchain (Ubuntu's is too old)
if ! /usr/local/go/bin/go version 2>/dev/null | grep -q "$GO_VERSION"; then
  curl -sL "https://go.dev/dl/go${GO_VERSION}.linux-$(dpkg --print-architecture).tar.gz" | tar -C /usr/local -xz
fi
export PATH=$PATH:/usr/local/go/bin

# build from source
if [ ! -d /opt/network-computer ]; then
  git clone -q https://github.com/askrobots/network-computer /opt/network-computer
else
  git -C /opt/network-computer pull -q
fi
cd /opt/network-computer && go build -o /usr/local/bin/ ./cmd/...

# secrets, generated once
mkdir -p /etc/nc
if [ ! -f /etc/nc/env ]; then
  cat > /etc/nc/env <<EOT
NC_PASSWORD=$(head -c 12 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 16)
NC_PIN=$(shuf -i 100000-999999 -n 1)
NC_PUBLIC_IP=$NC_PUBLIC_IP
EOT
  chmod 600 /etc/nc/env
fi

# headless 1080p X server on :0 using the dummy driver
mkdir -p /etc/X11/xorg.conf.d
cat > /etc/X11/xorg.conf.d/10-dummy.conf <<'EOT'
Section "Device"
    Identifier "dummy"
    Driver "dummy"
    VideoRam 256000
EndSection
Section "Monitor"
    Identifier "mon"
    HorizSync 28.0-80.0
    VertRefresh 48.0-75.0
    Modeline "1920x1080" 172.80 1920 2040 2248 2576 1080 1081 1084 1118
EndSection
Section "Screen"
    Identifier "scr"
    Device "dummy"
    Monitor "mon"
    DefaultDepth 24
    SubSection "Display"
        Depth 24
        Modes "1920x1080"
    EndSubSection
EndSection
EOT

# uinput access for the service (it runs as root here anyway)
echo 'KERNEL=="uinput", MODE="0660", GROUP="input"' > /etc/udev/rules.d/99-nc-uinput.rules
modprobe uinput || true
echo uinput > /etc/modules-load.d/uinput.conf

cat > /etc/systemd/system/nc-xorg.service <<'EOT'
[Unit]
Description=headless Xorg for network-computer
[Service]
ExecStart=/usr/bin/Xorg :0 -config /etc/X11/xorg.conf.d/10-dummy.conf -noreset -nolisten tcp
Restart=always
[Install]
WantedBy=multi-user.target
EOT

cat > /etc/systemd/system/nc-desktop.service <<'EOT'
[Unit]
Description=xfce desktop on the headless Xorg
After=nc-xorg.service
Requires=nc-xorg.service
[Service]
Environment=DISPLAY=:0
ExecStartPre=/bin/sleep 2
ExecStart=/usr/bin/dbus-launch --exit-with-session startxfce4
# no screen blanking: a blank X screen streams as black video
ExecStartPost=/bin/sh -c "sleep 3; xset s off s noblank"
Restart=always
[Install]
WantedBy=multi-user.target
EOT

cat > /etc/systemd/system/nc-rendezvous.service <<'EOT'
[Unit]
Description=network-computer rendezvous
After=network-online.target
[Service]
EnvironmentFile=/etc/nc/env
ExecStart=/usr/local/bin/nc-rendezvous -http :8765 -turn :3478 -public-ip ${NC_PUBLIC_IP}
Restart=always
[Install]
WantedBy=multi-user.target
EOT

cat > /etc/systemd/system/nc-host.service <<'EOT'
[Unit]
Description=network-computer host (headless desktop)
After=nc-desktop.service nc-rendezvous.service nc-audio.service
Requires=nc-desktop.service
[Service]
EnvironmentFile=/etc/nc/env
Environment=DISPLAY=:0
ExecStartPre=/bin/sleep 3
ExecStart=/usr/local/bin/nc-host -rendezvous http://127.0.0.1:8765 -name cloudbox -size 1280x720 -fps 30 -bitrate 4M -encoder libx264 -audio-device nc.monitor
Restart=always
[Install]
WantedBy=multi-user.target
EOT

# firewall: ssh, rendezvous http, stun/turn, relay ports
ufw allow 22/tcp >/dev/null
ufw allow 8765/tcp >/dev/null
ufw allow 3478/udp >/dev/null
ufw allow 49152:65535/udp >/dev/null
ufw --force enable >/dev/null

systemctl daemon-reload
systemctl enable --now nc-xorg nc-desktop nc-audio nc-rendezvous nc-host
sleep 5
systemctl --no-pager --lines=3 status nc-rendezvous nc-host | grep -E 'Active|PIN|registered' || true
echo
echo "rendezvous: http://$NC_PUBLIC_IP:8765"
cat /etc/nc/env
