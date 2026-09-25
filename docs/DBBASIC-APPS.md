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

## Driving the apps: voice and scripts

Each dbbasic app built on the shared kit (`dbbasic_app_kit`, private) listens on
a control socket, only for the desk user:
`$XDG_RUNTIME_DIR/dbbasic/APP-PID.sock` (folder 0700, socket 0600; on a desk
`/run/nc-desktop/dbbasic/`). One JSON line in, one out. `describe` lists the
app's actions and state; every action says what it can do: `read`, `edit` (the
open document; undo brings it back), `file` (opens or writes a file), or
`destructive` (can lose work).

```sh
nc-desk apps                                              # what is running, what each can do
nc-desk app writer insert_text '{"text": "# Notes\n\nFirst point"}'
nc-desk app writer select_text '{"text": "First point"}'
nc-desk app writer format '{"style": "bold"}'
nc-desk app writer save_as '{"path": "~/Objects/Notes"}'   # .dbw added; never overwrites unasked
```

Voice sees the running apps and their actions in its desktop context and uses
them instead of screenshots and clicks ("write my landlord a thank-you note in
Writer and save it" is three actions: new_document, insert_text, save_as).
Approval follows the effect: read and edit run; file actions get the second-model
review; anything destructive, `overwrite`, or `discard` with unsaved changes is
asked of the user. File paths stay inside home and outside hidden folders.

Writer's actions: get_text, get_selection, insert_text (blank lines = paragraphs,
`# ` headings, `- ` bullets; always escaped), replace_selection, select_text,
format (bold, italic, underline, heading1-3, paragraph, bullets, numbers,
left/center/right/justify), find, suggest_name, new_document, open, save,
save_as.

Spreadsheet's (files `.dbs`): get_cells (a range as rows; formulas on request),
set_cells (`{"A1": "Rent", "B5": "=SUM(B2:B4)"}`), fill (a whole table from a
cell), clear, add_rows, add_columns, new_sheet, open, save, save_as, export_csv.

Slides' (files `.dbp`): list_slides, get_slide, add_slide (title, body with
`- ` bullets, where), set_slide, move_slide, delete_slide (asked first),
show_slide, mode (present/edit), open, save, save_as.

Porter's: formats, convert_text, convert_file (format from the extension; the
output next to the input, never over a file unasked).

Shell's: read_output (redacted, as Shell's own AI context is), run_command
(always asked of the user: a command can do anything), new_tab, switch_tab.

Webmaster's: templates, new_site (from a template, into a new folder),
open_site, list_pages, get_page, add_page and add_text (dictated text becomes
heading, paragraph and list blocks), delete_page (asked), save, export (plain
HTML into a new folder), publish (asked: it goes to the object server), and
ai_edit (paid): AI rewrites a page's words, never its structure, as one undo
step: `site` (write the page for this site, replacing template copy), rewrite,
shorter, friendlier, professional, spelling, or an own instruction. It never
invents facts; it leaves [placeholders]. In the app the same is ✨ on a
block's toolbar and AI in the top bar. It needs only an AI key: brochure
sites need no object server.

Draw's: generate_image (marked paid: it costs money), open, save, save_as,
new_canvas, undo.

Cabinet's (the paperless office): list, search, get, import_file, read_text
(OCR: pdftotext, ocrmypdf, tesseract, installed by provisioning), suggest (AI,
on redacted text; marked paid), set, file. Its library is plain files:
`inbox/` and `filed/<year>/`, each original beside `.cabinet.json` and `.txt`. The kit's web view is its own package
(`dbbasic_editor_web_view`), so only Writer, Spreadsheet and Slides carry WebKit.

## Phone, tablet or desktop

The client says what it is (phone, tablet or desktop; touch or pointer) and
the desk's host writes it to `/run/nc-host/input`. The apps read it through
the kit (`InputMode`): 48 px targets for a finger, and on a phone Spreadsheet
and Slides hide the text-formatting row so the sheet or slide gets the room.
The desk itself follows too (`nc-device`, see DESKTOP.md).

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
- Writer, Spreadsheet and Slides edit in a web view. `flutter_inappwebview`
  has no Linux build Ubuntu can use (its beta needs WPE WebKit, which Ubuntu
  does not package; a fork that draws WebKitGTK offscreen showed a blank page
  on a desk without a GPU). On Linux they use `webview_all`, a real WebKitGTK
  view over the window, behind a small adapter (`EditorWebView`); macOS keeps
  `flutter_inappwebview`. Their Open/Save dialogs on Linux are GTK's own
  (`file_selector`): zenity, which `file_picker` uses there, ignores a
  suggested file name.

## What is on the desk (2026-09-25)

| App | Command | Notes |
|---|---|---|
| Writer | `dbbasic-writer` | `.dbw` opens in it; suggests a file name on save (heading, else AI) |
| Spreadsheet | `dbbasic-spreadsheet` | `.dbs`; voice/script control |
| Slides | `dbbasic-slides` | `.dbp`; voice/script control |
| Shell | `dbbasic-shell` | AI terminal; keys from `ai.env`, Sonnet 5 |
| Webmaster | `dbbasic-webmaster` | as it was; the object server can make it simpler later |
| Porter | `dbbasic-porter` | format converter |
| Draw | `dbbasic-draw` | first Linux build; its OpenAI key is still its own setting |
| Cabinet | `dbbasic-cabinet` | the paperless office: add, read (OCR), suggest (AI), file; library in ~/Documents/Cabinet |

Known gaps: Draw does not read `ai.env` yet; builds reach only the desk `infra/apps.sh` points at (other people's desks
need their own build or a copy of `/desk/apps`).
