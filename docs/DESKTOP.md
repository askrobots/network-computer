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
- **The object server as the home for files:** files live in the object server (permissioned,
  versioned, backed up, and not tied to a region), and every desk mounts them and every
  client sees them. The desk volume then holds only settings and secrets, which fits hot
  desking and the object server front door (DESKS.md): sit down anywhere, your files are
  already there, the same permission engine decides what the AI may see.

## 4. A look that streams well

Every pixel that changes costs bandwidth, and fine detail costs sharpness:

- a plain or softly graded background instead of the high-detail contour wallpaper;
- window compositing, shadows and animations off;
- fonts with grayscale antialiasing and slight hinting (no colored subpixel edges, which
  video compression smears), and a clear UI and terminal font;
- measure before and after with the stats panel (bitrate on an idle desktop).

### Idea: the background is a dashboard

Instead of a picture, the desktop background shows the desk's state at a glance:

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
