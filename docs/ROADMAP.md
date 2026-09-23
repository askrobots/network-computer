# Roadmap / vision

Where this is headed, beyond the working Phase 0. Captured 2026-09-21.

## 1. A closer host (latency)
DigitalOcean's nearest US region to Houston is New York at ~42 ms. Real wins:
- A Dallas box (Vultr/Linode) at ~10-20 ms — scripts already exist in `infra/`.
- An AWS Houston Local Zone, in-metro, single-digit to low-teens ms — the lowest
  option; needs the Local Zone plumbing scripted (`infra/README.md` has the start).
Test these and keep the best.

## 2. The client, especially iPad
The iPhone app is scaffolded but untested on a device. The bigger idea: **iPad as
the sweet spot.** A large screen makes it a genuine Linux desktop, not a phone view.
With a Magic Keyboard it has a trackpad and keys already; external display drives a
monitor. The same libwebrtc client targets both; iPad just gets more room. Priority:
get the client running on a real iPad, tune the desktop for that form factor.

## 3. AI in the loop, with voice
We already drive the host as an agent (`docs/AGENT.md`): shell + live GUI + reading
the screen back. The vision extends it:
- **Talk to the iPad.** Voice in, spoken/acted results out.
- **An assistant with its own API key attached to the host or the client**, listening
  and automating system tasks directly — the same driving we did by hand, but
  autonomous and continuous.
- This is the differentiator: not just a remote screen, but a computer an AI operates
  with you. Security matters here — an always-listening agent with an API key and a
  shell needs a dedicated user, least privilege, and clear on/off control.

## 4. A curated, remote-first Linux desktop
Detailed plan (clipboard, files, shortcuts, streaming-friendly look, session restore):
[DESKTOP.md](DESKTOP.md).
Make the streamed desktop feel better than a local one for this use. Ship it as
declarative prefs in `provision/` (already started with dconf defaults):
- Find and standardize on the best apps for remote use (editor, terminal, browser,
  file manager, media) rather than xfce defaults.
- A full preference set tuned for streaming: fonts, scaling, no blanking, sane
  wrapping, fast theming, keybindings that survive the WebRTC input path.
- Goal: a network-computer desktop that is polished out of the box.

## 5. dbbasic.com ecosystem
Tie this to the many things already on dbbasic.com. The pitch: **the best network
computer experience that has ever existed** — your files, apps, and AI, reachable
from any device, with a real desktop behind it.

## 6. Build and test the Dart/Flutter apps
The Flutter apps built for macOS could be built and tested here too:
- Flutter has a Linux desktop target — build and run them on the remote host.
- Or use the host as a build/test box in CI-like fashion.
Wire this into the provisioning (Flutter SDK) and try one app end to end.

## 7. Desks and hot desking
Disposable computers of any size, a persistent desk (settings, keys, documents, models) split
into volumes by lifecycle, and a controller that stops idle desks and wakes them on connect,
schedule or AI task. What companies, travelers and salespeople need from it, with hourly
costs: [DESKS.md](DESKS.md).

## Near-term order (suggested)
1. Stand up a Dallas or Houston-LZ host, measure the latency win.
2. Get the client onto a real iPad; tune the desktop for it.
3. Prototype voice + AI-with-API-key driving the host.
4. Curate the desktop app/pref set.
5. Build one Flutter app on the host.
