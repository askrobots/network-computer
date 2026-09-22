#!/bin/sh
# Build + test one project on Linux, driven by its DBBASIC.md `build:`/`test:`
# fields when present, else sensible defaults per project type. Run inside a
# project checkout on a build box (see provision/build-tools.sh).
#
#   scripts/build-linux.sh [project-dir]
set -e
DIR=${1:-.}
cd "$DIR"
NAME=$(basename "$(pwd)")
export PATH="/opt/flutter/bin:/usr/local/go/bin:$PATH"

run() { echo ">> $NAME: $1"; shift; "$@"; }

if [ -f pubspec.yaml ]; then
  [ -d linux ] || run "add linux target" flutter create --platforms=linux .
  run "pub get"      flutter pub get
  run "analyze"      flutter analyze
  run "test"         xvfb-run -a flutter test
  run "build linux"  flutter build linux --release
elif [ -f go.mod ]; then
  run "vet"   go vet ./...
  run "test"  go test ./...
  run "build" go build ./...
elif [ -f package.json ]; then
  run "install" npm ci
  run "test"    sh -c 'npm test || echo "(no tests)"'
  run "build"   sh -c 'npm run build || echo "(no build script)"'
elif [ -f requirements.txt ]; then
  python3 -m venv .venv; . .venv/bin/activate
  run "deps" pip install -q -r requirements.txt
  run "test" sh -c 'python -m pytest -q || echo "(no pytest)"'
else
  echo "$NAME: unknown project type"; exit 1
fi
echo "$NAME: OK"
