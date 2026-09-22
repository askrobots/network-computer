# Build box spec

A dedicated, ephemeral Linux box to build and test the dbbasic Flutter apps, then
shut down. Separate profile from the streaming host: that one stays small and up;
this one is big, does a batch, and gets destroyed.

## Flutter apps to build/test

Surveyed 2026-09-21. Priority favors apps that already have a Linux target and real
tests. "add linux" = needs `flutter create --platforms=linux .` first (the driver
does this automatically).

| # | App (dir on /Volumes/T9) | Linux target | Tests | Priority |
|---|---|---|---|---|
| 1 | dbbasicshell | ready | 19 | 1 |
| 2 | dbbasic-porter/porter_app | ready | 21 | 1 |
| 3 | dbbasic-webmaster | ready | 10 | 1 |
| 4 | dbbasic-slides-native | ready | 1 | 2 |
| 5 | dbbasic-slate | ready | 1 | 2 |
| 6 | dbbasic-spreadsheet-native | ready | 1 | 2 |
| 7 | dbbasic-writer-native | ready | 1 | 2 |
| 8 | dbbasic-dove | add linux | 6 | 3 |
| 9 | dbbasic-scroll | add linux | 1 | 3 |
| 10 | dbbasic-draw | add linux | 1 | 3 |
| 11 | dbbasic-tape | add linux | 1 | 3 |
| — | network-computer-flutter | ready | 1 | (ours, already builds) |

Non-Flutter, build separately (same driver handles them): network-computer (Go),
dbbasic-browser (Node), dbbasic / content / website / video (Python).

## Size: bigger and shorter beats small and long

Flutter Linux builds are CPU-bound and parallelize across cores, and DigitalOcean
bills by the second. So a box with 4× the cores that runs in ~1/4 the time costs
about the same in dollars, finishes far sooner, and lets you destroy it sooner —
exactly the goal.

| Size | vCPU | RAM | Disk | $/hr | Role |
|---|---|---|---|---|---|
| s-2vcpu-4gb | 2 | 4 GB | 80 GB | $0.036 | the streaming host (keep small) |
| **s-8vcpu-16gb** | **8** | **16 GB** | **320 GB** | **$0.143** | **recommended build box** |
| c2-4vcpu-8gb | 4 | 8 GB | 100 GB | $0.140 | CPU-optimized alt (fewer, faster cores) |

Recommendation: **s-8vcpu-16gb**. Eight cores build many apps in parallel, 16 GB
keeps the Dart analyzer and linkers comfortable, and 320 GB easily holds the Flutter
SDK, pub cache and every app's build artifacts (a dozen Flutter Linux builds is tens
of GB). A full build+test sweep of all apps should land well under an hour, so one
run costs roughly **$0.10–0.20** and then you destroy it.

Rule of thumb: the 2 vCPU box saves ~$0.10/hr but takes several times longer and
keeps you waiting; not worth it for a batch. Keep the small box for streaming, spin
the big box only for build runs.

## Lifecycle

Ephemeral. Create, provision, build everything, collect artifacts, destroy.

```sh
# 1. create a big box (create-droplet.sh takes the size as its third arg)
infra/create-droplet.sh nc-build nyc3 s-8vcpu-16gb

# 2. install the build toolchain on it
IP=$(doctl compute droplet list --format Name,PublicIPv4 --no-header | awk '/nc-build/{print $2}')
scp provision/build-tools.sh root@$IP:/root/ && ssh root@$IP 'sh /root/build-tools.sh'

# 3. copy the projects up and build each (or clone from their repos)
#    scripts/build-linux.sh <dir> builds+tests one project by type.
#    A sweep script that iterates all of them is the next thing to write.

# 4. pull artifacts (build/linux/x64/release/bundle per Flutter app), then:
doctl compute droplet delete nc-build --force
```

## What's left to build

- A **sweep script** that clones/copies the app list, runs `build-linux.sh` on each,
  and writes a pass/fail report — the lightweight CI over all projects.
- `build:` / `test:` fields in each app's `DBBASIC.md` so the sweep is
  self-describing (see docs/BUILD.md).
- Decide artifact destination: leave `.tar.gz` bundles on the box to scp down, or
  push to a release/bucket.

## Region

Same story as the streaming host: NYC is DigitalOcean's closest US region to Houston.
Latency does not matter for a build box, so region is only about transfer speed of
pulling artifacts down — NYC is fine, or match wherever the artifacts are headed.
