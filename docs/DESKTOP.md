# Making it a better desktop

Plan, 2026-09-23. The streaming works; this is about the network computer feeling like
*your* computer rather than a remote one. In rough priority order.

## 1. Clipboard, both ways

The most noticeable gap in daily use: copy on the Mac, paste on the desk, and back.

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

- **Browser client:** drag a file onto the window and it lands in `Documents`; downloads on
  the desk can be sent back to the device. Chunked over the `control` channel.
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
