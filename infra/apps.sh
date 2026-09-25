#!/bin/sh
# Build the dbbasic apps for Linux on a desk and install them there.
#
#   infra/apps.sh toolchain            Flutter + build tools on the desk (once)
#   infra/apps.sh build NAME...        build and install (writer spreadsheet ...)
#   infra/apps.sh list                 the apps this knows
#
# The apps are Flutter and not open source: their source is copied to the
# desk's build user (/var/lib/ncbuild, the computer's own disk, never the
# desk volume) only to build. What is installed is the built bundle, on the
# desk in /desk/apps/NAME/ with a manifest, so a new computer gets the apps
# back (nc-apps install, run by provisioning). Sources are found under
# $DBBASIC_APPS_SRC (default /Volumes/T9). Uses the same desk as droplet.sh.
set -e
cd "$(dirname "$0")"
SRC=${DBBASIC_APPS_SRC:-/Volumes/T9}
# the current stable on the desk (the apps' webview_all needs >= 3.35)
FLUTTER_VERSION=3.47.5

# name|source dir|title|linux app id|binary|categories|extensions|mime|comment
APPS='writer|dbbasic-writer-native|DBBASIC Writer|com.dbbasic.writer|dbbasic_writer|Office;WordProcessor;|dbw|application/x-dbbasic-writer|Write documents
spreadsheet|dbbasic-spreadsheet-native|DBBASIC Spreadsheet|com.dbbasic.spreadsheet|dbbasic_spreadsheet|Office;Spreadsheet;|dbs|application/x-dbbasic-sheet|Spreadsheets
slides|dbbasic-slides-native|DBBASIC Slides|com.dbbasic.slides|dbbasic_slides|Office;Presentation;|dbp|application/x-dbbasic-slides|Presentations
draw|dbbasic-draw|DBBASIC Draw|com.dbbasic.draw|dbbasic_draw|Graphics;2DGraphics;|||Draw and make images
shell|dbbasicshell|DBBASIC Shell|com.dbbasic.shell|basicshell|System;TerminalEmulator;|||A terminal with AI
webmaster|dbbasic-webmaster|DBBASIC Webmaster|com.dbbasic.webmaster|dbbasic_webmaster|Development;WebDevelopment;|||Build websites
porter|dbbasic-porter/porter_app|DBBASIC Porter|com.dbbasic.porter|porter|Utility;|||Convert between formats
cabinet|dbbasic-cabinet|DBBASIC Cabinet|com.dbbasic.cabinet|dbbasic_cabinet|Office;Scanning;OCR;|||The paperless office: scan, read, file'

row() { echo "$APPS" | awk -F'|' -v n="$1" '$1==n'; }
IP=$(doctl compute droplet list --format Name,PublicIPv4 --no-header 2>/dev/null | awk -v n="${NC_HOST_NAME:-nc}" '$1==n{print $2}')

case "$1" in
  list) echo "$APPS" | awk -F'|' '{printf "%-12s %-24s %s\n", $1, $3, $2}' ;;
  toolchain)
    [ -n "$IP" ] || { echo "no desk is up"; exit 1; }
    ssh root@"$IP" FLUTTER_VERSION=$FLUTTER_VERSION sh -s <<'EOF'
set -e
export DEBIAN_FRONTEND=noninteractive
apt-get -o DPkg::Lock::Timeout=600 install -y -q clang cmake ninja-build pkg-config libgtk-3-dev liblzma-dev \
  libstdc++-14-dev xz-utils unzip rsync libwebkit2gtk-4.1-dev >/dev/null
id ncbuild >/dev/null 2>&1 || useradd -r -m -d /var/lib/ncbuild -s /bin/bash ncbuild
have=$(cat /opt/flutter/version 2>/dev/null || runuser -u ncbuild -- /opt/flutter/bin/flutter --version 2>/dev/null | awk 'NR==1{print $2}')
if [ "$have" != "$FLUTTER_VERSION" ]; then
  rm -rf /opt/flutter
  curl -sSfL "https://storage.googleapis.com/flutter_infra_release/releases/stable/linux/flutter_linux_${FLUTTER_VERSION}-stable.tar.xz" -o /tmp/flutter.tar.xz
  tar -xJf /tmp/flutter.tar.xz -C /opt && rm /tmp/flutter.tar.xz
  chown -R ncbuild:ncbuild /opt/flutter
fi
runuser -u ncbuild -- sh -c 'export PATH=/opt/flutter/bin:$PATH; git config --global --add safe.directory /opt/flutter
  flutter config --no-analytics --enable-linux-desktop >/dev/null; flutter precache --linux >/dev/null; flutter --version | head -1'
EOF
    ;;
  build)
    shift; [ $# -gt 0 ] || { echo "usage: apps.sh build NAME..."; exit 2; }
    [ -n "$IP" ] || { echo "no desk is up"; exit 1; }
    for name in "$@"; do
      r=$(row "$name"); [ -n "$r" ] || { echo "unknown app: $name"; exit 2; }
      IFS='|' read -r n dir title appid bin cats exts mime comment <<EOF
$r
EOF
      [ -f "$SRC/$dir/pubspec.yaml" ] || { echo "no source at $SRC/$dir"; exit 1; }
      echo ">> $title: copying the source to the build user"
      # the same layout as $SRC, so path dependencies (../dbbasic-app-kit) resolve
      ssh root@"$IP" "install -d -o ncbuild -g ncbuild /var/lib/ncbuild/src/$dir /var/lib/ncbuild/src/dbbasic-app-kit"
      X="--exclude .git/ --exclude build/ --exclude .dart_tool/ --exclude Pods/ --exclude .DS_Store --exclude .env --exclude *.env"
      [ -f "$SRC/dbbasic-app-kit/pubspec.yaml" ] &&
        rsync -a --delete $X "$SRC/dbbasic-app-kit/" root@"$IP":/var/lib/ncbuild/src/dbbasic-app-kit/
      rsync -a --delete $X "$SRC/$dir/" root@"$IP":/var/lib/ncbuild/src/$dir/
      echo ">> $title: building (flutter build linux)"
      ssh root@"$IP" N="$n" DIR="$dir" APPID="$appid" BIN="$bin" TITLE="\"$title\"" CATS="\"$cats\"" EXTS="\"$exts\"" \
        MIME="\"$mime\"" COMMENT="\"$comment\"" sh -s <<'EOF'
set -e
S=/var/lib/ncbuild/src/$DIR; chown -R ncbuild:ncbuild /var/lib/ncbuild/src; cd "$S"
[ -d linux ] || runuser -u ncbuild -- sh -c 'PATH=/opt/flutter/bin:$PATH flutter create --platforms=linux . >/dev/null'
# its own identity on Linux: apps copied from Writer still carry Writer's,
# and GTK would hand a second app with the same id to the first one's window
sed -i "s|^set(BINARY_NAME .*|set(BINARY_NAME \"$BIN\")|; s|^set(APPLICATION_ID .*|set(APPLICATION_ID \"$APPID\")|" linux/CMakeLists.txt
# and its name on the window, not the Dart project name
sed -i -E "s/(gtk_header_bar_set_title\(header_bar, |gtk_window_set_title\(window, )\"[^\"]*\"/\1\"$TITLE\"/" linux/runner/my_application.cc
# the whole icon font: an incremental build kept the first build's cut-down
# font, so icons added later drew as blanks (2026-09-25)
runuser -u ncbuild -- sh -c 'export PATH=/opt/flutter/bin:$PATH; flutter pub get >/dev/null && flutter build linux --release --no-tree-shake-icons 2>&1 | tail -15'
B=$S/build/linux/x64/release/bundle
[ -x "$B/$BIN" ] || { echo "!! build produced no $BIN"; exit 1; }
ROOT=/desk/apps; [ -d /desk/nc ] || ROOT=/opt/dbbasic
install -d "$ROOT/$N"; rsync -a --delete "$B/" "$ROOT/$N/"
icon=$(ls "$S"/macos/Runner/Assets.xcassets/AppIcon.appiconset/*512*.png 2>/dev/null | head -1)
[ -n "$icon" ] && cp "$icon" "$ROOT/$N/icon.png"
cat > "$ROOT/$N/nc-app.env" <<M
NAME=$N
TITLE="$TITLE"
BINARY=$BIN
COMMENT="$COMMENT"
CATEGORIES="$CATS"
EXTENSIONS="$EXTS"
MIME="$MIME"
ICON=icon.png
M
chown -R root:root "$ROOT/$N"; chmod -R a+rX "$ROOT/$N"
nc-apps install
echo "   installed $(du -sh "$ROOT/$N" | cut -f1) in $ROOT/$N"
EOF
    done ;;
  *) echo "usage: apps.sh toolchain | build NAME... | list"; exit 2 ;;
esac
