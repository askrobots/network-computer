# Building and testing the dbbasic projects on Linux

macOS distribution builds already exist. This is the plan to also build and test
on Linux, reproducibly, without containers.

## The idea

The network-computer host is already a provisioned Linux box we can create on any
provider and drive as a GUI desktop. Make it double as the **build + test box** for
the other dbbasic projects. Same provisioning pattern, one more script.

```
provision/apply.sh          base host (from before)
provision/build-tools.sh    + Flutter, Linux desktop deps, Go, Node, Python, Xvfb
scripts/build-linux.sh      build+test one project (Flutter/Go/Node/Python)
```

## The projects (survey, 2026-09-21)

Mostly Flutter. 7 already carry a `linux/` target, 4 are macOS-only and need it
added (`flutter create --platforms=linux .`, which `build-linux.sh` does
automatically). Real test suites live in dbbasicshell, porter, webmaster, dove.
Non-Flutter: network-computer (Go), dbbasic-browser (Node), and several Python
services (dbbasic, content, website, video).

## How to build one project

On a build box (after `provision/build-tools.sh`):

```sh
scripts/build-linux.sh /path/to/dbbasic-writer-native
```

It picks the toolchain from the project files: Flutter apps get
`pub get → analyze → test (headless via Xvfb) → build linux`; Go/Node/Python get
their equivalents.

## Headless vs visual testing

- **Headless (CI-style):** `xvfb-run -a flutter test` runs widget/unit tests with
  no display. Fast, scriptable, good for a build loop.
- **Visual (real GUI):** launch the built app on the streamed desktop (nc-host) and
  interact or screenshot it — the same way we drove Firefox and Mousepad. This is
  integration testing you can actually watch, and an AI can drive.

## Making it self-describing (proposed)

Extend each project's `DBBASIC.md` with a `build:` / `test:` block so the box can
build any project uniformly without special-casing:

```yaml
dbbasic:
  build:
    linux: flutter build linux --release
  test:
    linux: xvfb-run -a flutter test
```

`build-linux.sh` uses these when present and falls back to the per-type defaults
above. That turns "build all my projects on Linux" into: sweep for DBBASIC.md,
run each one's declared commands, report pass/fail — a lightweight CI with no
heavy tooling.

## Suggested order

1. Stand up a build box: `provision/apply.sh` then `provision/build-tools.sh`.
2. Build the 7 Flutter apps that already have a Linux target; fix what breaks.
3. Add Linux targets to the 4 macOS-only apps and build them.
4. Wire the Go/Node/Python projects.
5. Add `build:`/`test:` to each DBBASIC.md and script the sweep.
