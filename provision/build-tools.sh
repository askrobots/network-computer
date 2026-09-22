#!/bin/sh
# Turn a provisioned host into a Linux build + test box for the dbbasic projects.
# Idempotent. Run after apply.sh (or standalone on any Ubuntu 24.04 box).
# Adds: Flutter (+ Linux desktop build deps), Go, Node, Python, and Xvfb so GUI
# apps can be tested headlessly. The streamed desktop (nc-host) can also run them
# visually.
set -e
export DEBIAN_FRONTEND=noninteractive

echo ">> Flutter Linux desktop build deps"
apt-get update -q
apt-get install -y -q \
  clang cmake ninja-build pkg-config libgtk-3-dev liblzma-dev \
  xvfb x11-utils \
  nodejs npm \
  python3 python3-pip python3-venv \
  unzip curl git

echo ">> Go (if not already present from apply.sh)"
command -v go >/dev/null || { echo "run provision/apply.sh first for Go, or install go"; }

echo ">> Flutter SDK -> /opt/flutter"
if [ ! -d /opt/flutter ]; then
  git clone --depth 1 -b stable https://github.com/flutter/flutter.git /opt/flutter
fi
export PATH="/opt/flutter/bin:$PATH"
grep -q '/opt/flutter/bin' /etc/profile.d/flutter.sh 2>/dev/null || \
  echo 'export PATH="/opt/flutter/bin:$PATH"' > /etc/profile.d/flutter.sh
git config --global --add safe.directory /opt/flutter
flutter config --enable-linux-desktop >/dev/null 2>&1 || true
flutter --version | head -1
echo
echo "build box ready. Use scripts/build-linux.sh from a project checkout, or"
echo "run one app headlessly:  xvfb-run -a flutter test"
