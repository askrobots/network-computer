# The dbbasic apps on a desk

The dbbasic office apps (Writer, Spreadsheet, Slides, Draw, Shell, Webmaster,
Porter) are Flutter apps for macOS and Linux. They are not open source yet, so
desks get their built binaries, never their source. This page is the part that
is public: where the apps look for shared settings, and how a desk gets them.

## Shared settings: `~/.config/dbbasic/`

One place every dbbasic app looks, so a key set up once turns on AI in every
app that has it, on a desk, a Mac or a Linux laptop.

```
~/.config/dbbasic/ai.env
```

```sh
# AI keys for every dbbasic app (keep this file chmod 600)
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
# optional: use another model without a new build
DBBASIC_ANTHROPIC_MODEL=claude-sonnet-5
DBBASIC_OPENAI_MODEL=gpt-5-mini
```

- `$XDG_CONFIG_HOME/dbbasic/` when that is set; `%APPDATA%\dbbasic\` on Windows.
- One `NAME=value` per line, `#` comments, optional quotes or `export`.
- An environment variable of the same name wins over the file.
- No key anywhere: the app's AI features stay out of the way, and everything
  else works as before.
- A sandboxed macOS app reads it through a read-only entitlement for
  `~/.config/dbbasic/` (`com.apple.security.temporary-exception.files.home-relative-path.read-only`).
- An app never sends text that looks like it holds a password, key or card
  number to an AI service.

On a desk this file lives on the desk volume (`~/.config` is kept there), so it
survives a new computer. Provisioning moved the older `/desk/secrets/env` into
it and left a link; a person's profile keys (`NC_ANTHROPIC_API_KEY`...) are
written there, and the desk's object server loads them as the user's service
keys (so voice uses the same keys).

What uses it so far: Writer suggests a file name when you save an untitled
document (its first heading when it has one, otherwise a 2 to 4 word name from
the AI). The other AI apps (Shell, Draw, Scroll) still read their own places and
move over as they are touched.

## Getting the apps onto a desk

```sh
infra/apps.sh toolchain              # once: Flutter + build tools on the desk
infra/apps.sh build writer shell     # build and install (see: infra/apps.sh list)
```

- The source is copied to the desk's build user (`/var/lib/ncbuild`, the
  computer's own disk, never the desk volume) only to build, with `flutter build
  linux`. The built bundle is installed on the desk in `/desk/apps/NAME/` with a
  manifest (`nc-app.env`), so a new computer gets the apps back: provisioning
  runs `nc-apps install`, which makes each one a menu entry, an icon, a command
  (`dbbasic-NAME`) and the default for its file types (a `.dbw` in `~/Objects`
  or `~/Documents` opens in Writer on a double click).
- Builds run on the desk itself, so the first one is slow on a small desk.
  Package installs never restart the desk's own services
  (`/etc/needrestart/conf.d/nc.conf`): a build does not close your windows.
- Each app gets its own Linux identity (application id) at build time: Slides
  and Spreadsheet began as copies of Writer and would otherwise share its id,
  and GTK would hand a second app to the first one's window.
- Writer, Spreadsheet and Slides edit in a web view. `flutter_inappwebview` has
  no Linux build Ubuntu can use (its beta needs WPE WebKit, which Ubuntu does not
  package), so Linux builds swap in `zikzak_inappwebview`, a fork with the same
  API on WebKitGTK 4.1, in the build copy only; the repos and macOS builds keep
  theirs. This needs Flutter 3.38.6 or newer on the build machine.
