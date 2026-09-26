# Making it a better desktop

Plan, 2026-09-23. The streaming works; this is about the network computer feeling like
*your* computer rather than a remote one. In rough priority order.

## 1. Clipboard, both ways

The most noticeable gap in daily use: copy on the Mac, paste on the desk, and back.

**Status: text works in the web client (2026-09-23).** The toolbar's 📋↑ / 📋↓ send or
fetch the clipboard without a keystroke (so the desk's right-click → Paste works too). Ctrl/⌘+V puts this device's
clipboard on the desk and then pastes; Ctrl/⌘+C or X on the desk comes back to this device.
On a Mac, ⌘ acts as Ctrl on the desk (a checkbox in the 🖥️ panel turns that off). The
host side uses `xclip` on Linux and `pbcopy`/`pbpaste` on macOS; Windows hosts don't have it
yet. Tested with `nc-probe -clip` in both directions, up to 350 KB of multibyte text. Still to
do: images, the Flutter client, Windows hosts.

- **Device to desk (browser):** the page's `paste` event hands over the local clipboard
  without a permission prompt. On Cmd/Ctrl+V the client reads it there, sends it over the
  reliable `control` channel, the host sets the desk clipboard (`xclip`), then forwards the
  paste keystroke. Text first, images next.
- **Desk to device:** the host watches the desk clipboard (X selection owner changes) and
  sends new contents; the page writes them with `navigator.clipboard.write` on the next user
  gesture (Safari requires one).
- **History:** `askrobots/dbbasic-copypaste` (MIT, PyPI) is a clipboard manager with history
  for text and images. It is PyQt5, and Qt's clipboard works on Linux, so it can run on the
  desk too. Later the history can live in the object server so it follows you across desks
  and devices.

## 2. Mac shortcuts

On a Mac client, Cmd+C / Cmd+V / Cmd+Z / Cmd+S... should become Ctrl+ on the desk. Today Cmd
becomes the Linux Super key, and Safari keeps a few Cmd shortcuts for itself. A client-side
mapping (on by default for Mac clients) fixes most of it.

## 3. Files

Three layers, each a step further:

- **Browser client (works, 2026-09-23):** drop files on the window and they land on the
  desk's Desktop (on the desk volume; refused if it would not fit). On the desk, right-click
  a file → **Send to my device** (or `nc-send FILE...`) and a 📥 button appears in the
  browser; click it to save. Each file has its own reliable data channel. Tested with
  `nc-probe -send-file` / `-recv-dir`: 30 MB identical both ways. Folders: zip them first.
- **Native desktop client (Flutter, macOS/Windows/Linux):** a real **shared folder** in both
  directions, which a browser cannot do.
  - Desk to device: the desk's `Documents` appears as a folder on the Mac. macOS mounts
    WebDAV natively (Finder "Connect to Server"), so no kernel extension is needed; the
    client tunnels it over the existing connection.
  - Device to desk: a folder on the Mac appears on the desk (drive redirection), mounted
    there with `rclone`/`davfs2` over the same tunnel.
- **The object server as the home for files (first part works, 2026-09-25):** files live
  in the object server (permissioned, backed up, and not tied to a region), and every
  desk mounts them and every client sees them. The desk now mounts them as `~/Objects`;
  see "The object server as part of the desktop" below. The desk volume then holds only settings and secrets, which fits hot
  desking and the object server front door (DESKS.md): sit down anywhere, your files are
  already there, the same permission engine decides what the AI may see.

## 4. A look that streams well

Every pixel that changes costs bandwidth, and fine detail costs sharpness:

- a plain or softly graded background instead of the high-detail contour wallpaper;
- window compositing, shadows and animations off;
- fonts with grayscale antialiasing and slight hinting (no colored subpixel edges, which
  video compression smears), and a clear UI and terminal font;
- measure before and after with the stats panel (bitrate on an idle desktop).

### The background is a dashboard (works, 2026-09-23)

conky draws it on a plain dark background (set once; any wallpaper can replace it):
this computer's name, domain, public and private IP, region and size; what it costs
($/h, $/day, the month's cap) and what this boot has spent; host, rendezvous, voice,
object server and audio status; API keys by name only; CPU, memory, uptime; desk and
disk space; network in and out. The infra scripts pass the droplet's size and hourly
price to provisioning, which writes them with the other non-secret facts to
`/etc/nc/info` (the env file holds passwords and stays root-only). Numbers update
every 5 s and only that text is redrawn, so an idle desk still streams almost nothing.

### Today: weather, headlines, radio (2026-09-26)

The top of the dashboard is TODAY: the weather where you are (now and tomorrow), the
station playing, and five headlines, one from each feed in turn so one busy feed does
not fill them. A phone-sized screen gets one line (the weather and the station). The
**Today** icon opens the same as a page to tap through (every headline a link), and
the **Radio** icon a list of stations to tap, with Stop and "Find a station".
Voice does all of it: "what's the weather", "weather for Lisbon" (kept as your place),
"read me the headlines", "play some jazz", "stop the radio" (nc-voice's weather, news
and radio actions).

Each part is on by default and switched in a plain file, `~/.config/nc/dashboard.env`
(`WEATHER=on`, `NEWS=on`, `RADIO=on`, `PLACE=`, `UNITS=`); the feeds are
`~/.config/nc/feeds.txt` and the stations `~/.config/nc/stations.txt`, "Name | address"
per line, made from `/etc/nc-dashboard/` the first time. Weather is Open-Meteo (free,
no key: only the place name you chose is sent); radio plays on the desk (VLC), so it is
heard on whatever device is connected; more stations come from radio-browser.info, a
free community directory. Tools: `nc-feeds` (weather and headlines, cached in
`~/.cache/nc/`, fetched in the background so the dashboard never waits) and `nc-radio`.
Later: your own weather station (Tempest, Ambient Weather), and the object server
fetching once for all your devices.

The original idea:

Instead of a picture, the desktop background shows the desk's state at a glance:

- who and where: host name, public and private IP addresses, region, machine size, OS
  (like BGInfo on Windows desktops);
- desk volume used and free (it is small, so this matters), the machine's own disk;
- CPU, memory, uptime;
- what this computer has cost so far and per hour, and when it will auto-stop;
- the connection: direct or relayed, round trip, bitrate;
- recent clipboard and file transfers, and which API keys are set (names only, never values).

It fits "a look that streams well" as long as it changes rarely: redraw every 30 to 60
seconds, flat colors, no animation, so an idle desk still costs almost no bandwidth.
Candidates: conky (draws on the root window, open source, light), or a small script that
renders an image and sets it as the wallpaper. The numbers nc-host already knows
(path, round trip, bitrate) could be written to a file for it to read.

## Search and launch (works, 2026-09-23)

**Alt+Space** on the desk, or the 🔍 button in the client, opens one search box over open
windows, apps and the files in your home (rofi, from Ubuntu's archive; Ulauncher and Albert
are not packaged). Type, Enter: it switches to the window, starts the app, or opens the
file. ⌘Space never reaches the desk (macOS keeps it for Spotlight), hence Alt+Space; the
window menu moved to Shift+Alt+Space. `nc-launch` is the same bar from a script, and voice
will drive the same actions. Later: document contents (Recoll), a calculator, web search.

## Voice (first version works, 2026-09-23)

The round 🎙️ button (bottom left, always visible while connected) is one click per
phrase: click, say one thing, and it stops listening when the phrase ends (or after 8 s
of silence), answers, and turns the microphone back off if it turned it on. "keep
listening" in the panel makes it continuous until clicked again. A panel shows the state (listening, hearing
you, thinking) and every turn: what it heard, what it said, what it did.

- `nc-voice` on the desk listens to the client's microphone (the `nc-mic-in` source),
  cuts it into utterances at pauses, and sends each to the **object server** installed on
  the desk (`nc-object-server`, local only, data on the desk volume): speech to text,
  then the AI (Claude Haiku by default), then text to speech, played on the desk so it
  comes back through the stream. It doesn't hear itself while speaking.
- The AI answers with what to say and a list of desktop actions, which nc-voice does in
  the session: open an app, switch to or close a window, open a file or URL, type text,
  press keys, open the search bar with a query.
- Keys: the desk's `/desk/secrets/env` is loaded into the object server as the user's
  service keys by `nc-object-bootstrap` (never printed).
- Tested with synthesized speech: "What time is it?" answered correctly (about 9 s
  including the spoken reply); "Open the text editor" opened Mousepad (about 6 s).
- **It looks at the screen only when it needs to.** The AI has a `look` action for
  questions about what is inside a window ("what does my note say", "what's this
  error"); the window list gives only titles. Then one screenshot (JPEG, at most 1280
  wide) is attached to that one question, not kept in the history, and deleted from the
  object server right after; the panel shows "👁 looked at the screen" each time. Plain
  questions and commands never send the screen. The model is Claude Sonnet 5: Haiku was
  0.6 s quicker but read the screenshot right 2 times in 5, Sonnet 4 in 4. A screen
  question takes about 6 s; a plain one about 2 s.

Speed (measured per step, after the end of speech, synthesized questions): speech to
text 0.6–1.2 s, AI 0.8–1.2 s, then speech now comes from **Piper** on the desk (open
source, `en_US-lessac-medium`), streamed so the first sentence plays while the rest is
made: 0.4–0.7 s to first sound, against 0.9–2.7 s for OpenAI's speech. Time until the desk
answers: 2–3 s, down from 3–5 s. A soft chime says it heard you; the end-of-speech pause
is 0.7 s; actions run while it speaks, with a few words ("Opening Firefox.").

Local speech to text, measured (2026-09-23, whisper.cpp built on the desk, 4 vCPU
s-4vcpu-8gb, CPU only, 4 threads, a 4.2 s spoken command, time includes loading the
model each run):

| Model | Time | Transcript |
|---|---|---|
| tiny.en | 1.7 s | exact |
| base.en | 2.8 s | exact |
| small.en | 9.0 s | exact |
| OpenAI (in use) | 0.5-1.2 s | |

So local works on 4 cores but is slower than the cloud; on the 2-vCPU size expect about
twice these times. Keeping the model loaded (whisper.cpp's server mode) would save part
of the load time. The clip was clean synthetic speech; real voices in a room are harder,
tiny most of all. Plan: keep OpenAI as the default, offer base.en on 4+ cores as a
privacy option (the voice never leaves the desk). The benchmark is tools/whisper-bench.sh;
whisper.cpp is in /opt/whisper.cpp on the test desk.

Next: local speech to text as that option, a wake word,
reading the screen, the clipboard and files as context, and the object server's own
records and tools.

## The computer controller: voice that does things (2026-09-23)

`nc-desk` is a small open-source command-line controller for the desk, JSON in and out,
used by voice and usable by scripts or an AI agent over SSH:

- windows: list, arrange (left/right/top/bottom half, max, center, min), place, focus, close;
- mouse and keys: click, double/right click, drag, scroll, type, key combos;
- screenshots at a cheap size, with the scale from image to screen pixels;
- files, only inside the home folder: ls, mkdir, move, copy (never overwriting), and
  trash (undoable; the desk volume has its own Trash).

Voice now works in **steps**: it can act, then get the results (a file list, a web page's
text, a screenshot) and continue, up to 8 steps, each shown in the panel. It can arrange
windows, click what it sees (only after a screenshot in the same request: no blind
clicks), handle files, read web pages through the object server's reader (text, not a
screenshot), and search your records.

**Approval, in three levels:** opening, arranging, looking, listing, making folders,
moving and copying (which never overwrite) just run. Clicking, dragging, typing and key
presses get a quick second opinion from another model (Haiku), which sees your words and
exactly what is about to happen and says ok, ask, or no. Trash, and anything the reviewer
is unsure of, is asked out loud ("Should I move to Trash: ~/Desktop/old-notes.txt? Say yes
or no."); no answer means it doesn't happen.

**In the object server:** every turn is logged with source `voice`, every desktop action
as its own row (visible in `/shell`), and costs in `ai_usage`. Its pages (home, Talk,
Shell, Files) are in the Applications menu and on the desktop, and open signed in through
a local helper (`localhost:8009/open?next=/talk`; cookies are shared across ports on
localhost, so the object server needs no change). The object server has no approvals
queue yet (tasks auto-approve after 48 h, wrong for this), so approvals live in voice.

Tested on the desk: two windows side by side; a folder made and a file moved into it;
the Applications menu opened by clicking what it saw, then checked; a file trashed after
"yes"; Hacker News' top story read from the page text in about 4.5 s. Object server tools
attached to every chat call cost about 4 s per call, so voice calls the reader and search
itself, only when needed.

## The object server as part of the desktop (2026-09-23)

- **Its pages are apps.** `nc-webapp NAME URL ICON` (WebKitGTK, open source) opens a
  page in its own window: the app's name, its own taskbar entry, its own cookie store,
  no tabs or address bar; other sites open in the normal browser; the microphone only
  for local pages. Object Server, Talk, Shell, Files, Notes and Tasks are in the
  Applications menu this way, signed in through the local helper.
- **Alt+Space searches your records.** The launcher has a records tab (Ctrl+Tab): type,
  Enter, pick a note or task to open it. Voice can `find` records and take a `note`.
- **Its notifications are desktop notifications.** The object server's daemon
  (`nc-object-daemon`) turns record changes into notifications (app-notify); nc-voice
  shows each new one once, bottom right (top right is where browser tabs are), and
  clicking Open goes to its page.
- **Its files are a folder: `~/Objects` (2026-09-25).** The object server serves your
  files over WebDAV (`/dav/files/`, in the object server itself, so any desktop can
  mount it), and the desk mounts that with davfs2 (`nc-object-files.service`). Every app,
  the terminal, file dialogs and voice see plain files; saving, copying, renaming and
  deleting there changes the records the Files app shows, through the same quota and
  permission checks. It is in the file manager's side pane and on the desktop (Objects).
  - It signs in with an API key minted for the desk user by `nc-object-bootstrap`, kept
    root-only on the desk volume (`/desk/nc/davfs2.secrets`), reused by a new computer and
    minted again only if it stops working.
  - One flat folder: the object server's files have no folders yet, so making one is
    refused, and two files with the same name show as `name (2).ext`.
  - Free space is the object server's files quota (100 MB unless set), not the disk.
  - `lost+found` inside it is davfs2's own (local, for uploads it could not finish).
  - No locks yet, so a Mac's Finder would mount it read-only; davfs2 runs without them.
  - `nc-health` remounts it if it drops; verify checks it.
- Installed packages: theme, views, nav, shell, files, projects, collab, notes, tasks,
  notify.

## The desk follows the device (2026-09-25)

A phone, a tablet and a desktop want different things from the same desk, which
may be why Apple kept iPadOS and macOS apart. The client reports what it is
(the browser by screen size and pointer, the app by size and platform) and
`nc-device`, in the session, lays the desk out for it:

| | Desktop | Tablet | Phone |
|---|---|---|---|
| Panel / dock | yours | 40 / 64 px | 44 / 64 px |
| Window title bars | yours | hdpi (bigger buttons) | xhdpi |
| Text | yours | +2 pt | +3 pt |
| Launcher (Alt+Space, 🔍) | 44% wide | 70%, bigger rows | full screen, big rows |
| Windows | as they are | as they are | maximized, new ones too |

Your desktop settings are saved each time you leave the desktop and restored
when you come back. Voice sees the device ("device: phone (touch)") and can set
a layout ("set this up for my phone"); that choice holds until you connect from
a different kind of device. The dbbasic apps follow the same signal.

## 5. Your session comes back

Windows, terminal sessions (`tmux` on the desk) and Firefox tabs restored after a rebuild,
from session state kept on the desk. Pairs with the desk volume and with auto-stop: a desk
that shuts down when idle must come back the way you left it.

## 6. A curated app set

A good terminal font and theme, an office suite, a PDF viewer, a code editor, an image viewer,
Audacity (installed), and the dbbasic Flutter apps once built for Linux (BUILDBOX.md).
Declared in `provision/`, checked in `verify.sh`.

## 7. Across devices

- Desktop notifications forwarded to the phone.
- Print from the desk: a PDF arrives on the device you are using.
- Web traffic optionally out through your home connection over a private tunnel, since some
  sites (YouTube, Reddit) block cloud data-center addresses.

## Order

1. Clipboard (text, then images); Mac shortcut mapping.
2. Browser file drop and download.
3. Stream-friendly look (quick, measurable).
4. Session restore.
5. Native client shared folder; then files in the object server, mounted on every desk.

## Printing: "My device"

The desk has no printer of its own and should not need one: the printer is wherever you are.
**My device** is the desk's default printer (CUPS, listening on localhost only):

1. An app prints (File → Print), or `lp -d my-device FILE`, the file manager's right-click
   **Print on my device**, or voice's `print` action.
2. CUPS turns the job into a PDF (the queue's PPD, `/usr/share/ppd/nc/my-device.ppd`, asks
   for one) and runs the `ncdevice` backend (`/usr/lib/cups/backend/ncdevice`, 0700 root).
3. The backend PUTs it to nc-host's send socket with `print=1`; the host sends it on the file
   channel with `"print": true` in the header.
4. The client prints it: the iPhone/iPad/Mac app opens the system print dialog (AirPrint,
   the macOS print panel) and discards the PDF afterwards; the browser prints it from a
   hidden frame (Chrome, Firefox) or shows a 🖨 button that opens it (Safari, where a
   frame cannot print a PDF and a pop-up needs a click). A client from before this just
   saves the PDF like any sent file.

Nobody connected: the PDF is saved to `~/Documents/Printed/` and a notification says so;
the job does not vanish. `cups-browsed` and avahi stay off: the desk does not look for
network printers.

## Camera: your device's camera as the desk's webcam

The microphone already reaches the desk as a microphone; the camera does the same as a webcam.

1. Every client opens a second video transceiver (sending) at connect, **empty**. 📷 attaches
   the camera to it (`replaceTrack`), off detaches it and stops the camera, so the camera light
   is on only while it is in use. On a phone the button waits in ⋯ until the camera is on, then
   stays in the bar in red; ⋯ → Switch camera for front/back.
2. nc-host (`-camera-device /dev/video10`) decodes the incoming track (H.264, VP8, VP9 or AV1)
   with ffmpeg into a v4l2loopback device, letterboxed to 1280×720 so turning a phone sideways
   does not change the device's format. It asks for a keyframe when it starts. When frames stop
   for 2 s it lets the device go. One camera at a time: the newest one wins.
3. On the desk it is **network-computer camera** (`/dev/video10`, owned by the desk user). With
   `exclusive_caps`, browsers list it only while a device is sending: turn 📷 on first, then pick
   it in the call.

Provisioning: `linux-modules-extra` (cloud images leave out `videodev`, which v4l2loopback
needs) plus `linux-image-extra-virtual` so kernel updates keep it; `/etc/modprobe.d/nc-camera.conf`,
`/etc/modules-load.d/nc-camera.conf`, a udev rule for ownership. Test without a camera:
`nc-probe -camera-test 2s` sends a test pattern; `ffmpeg -f v4l2 -i /dev/video10 -frames:v 1 x.jpg`
as the desk user reads it back.

The round trip (your camera to the desk, the call's picture back to you in the desk's video)
adds some delay: fine for calls, not a mirror. Connecting other people (chat, invites) comes
later, with remote support ([REMOTE-SUPPORT.md](REMOTE-SUPPORT.md)).

