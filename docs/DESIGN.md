# Design rules

How everything on a desk, and every dbbasic app, should behave. These come from
using the desk from a Mac, an iPad and a phone, and from the voice assistant
driving it. Each rule says why.

## 1. Less room, less shown

A phone, a tablet and a desktop get **the same things in order of importance**,
not the same layout squeezed or cut off. When there is less room:

- **Show the most important first**, in a size that can be read on that
  screen; the rest waits behind a tap ("more", a tab, a detail page) — it is
  never simply clipped off the edge or pushed below where anyone looks.
- **Summarize before you drop**: five services become "services ok" (or only
  the ones that are failing); a cost table becomes "$0.50 spent".
- **Decide by the space you have**, not by the device's name: the desk's
  screen size can change mid-session (a phone connects), so layouts follow it
  live.

Where it shows today: the desktop dashboard has three layouts, chosen by the
screen size every few seconds (`nc-dashboard`: full, compact, mini); the
launcher is full-screen with big rows on a phone; Spreadsheet and Slides drop
their text-formatting row on a phone so the sheet or slide gets the room;
Cabinet goes from list-and-detail side by side to one page at a time.

*Why:* the dashboard was simply gone on a 920×424 phone screen: a fixed
layout taller than the screen, so nothing showed at all.

## 2. Touch is a first-class way in

- Targets a finger can hit: **48 px** (Material's minimum) in touch mode, 36
  for a mouse (`Density` in the app kit).
- **No hover-only, no right-click-only controls.** A touch-and-hold gives a
  right click on the desk, but a visible "⋯" beats a hidden menu.
- **One thing at a time on a phone**: windows maximized, detail pages instead
  of side panes, tabs at the bottom where a thumb is.
- The desk knows what you are using (`/run/nc-host/input`: phone, tablet or
  desktop; touch or pointer) and so do the apps (`InputMode`); the desk lays
  itself out for it (`nc-device`) and puts your own desktop back afterwards.

## 3. Everything is also an action

Anything a person can do in an app should be an action on its control socket
(`AppControl` in the app kit: JSON in, JSON out, an honest effect: read, edit,
file, paid, destructive), so the voice assistant and scripts can do it without
screenshots and clicks. Voice's approvals follow the effect.

## 4. AI suggests, people decide

- AI fills things in (a file name, a document's date and sender, a letter);
  a person, or voice with them, applies it. Nothing is filed, sent, published
  or paid for on a guess alone.
- What goes to an AI is redacted first (`redactForAi`: card and account
  numbers to their last four digits, passwords and keys removed); text that is
  only credentials is never sent.
- Keys live in one place, `~/.config/dbbasic/ai.env`, and are never printed.

## 5. Plain files, readable without us

Libraries are ordinary files with small JSON beside them (Cabinet's
`.cabinet.json` + `.txt`), in folders a person, `grep` or another AI can read.
On a desk they live on the desk volume, or in `~/Objects` (the object server).

## 6. Say what really happened

An action that failed says so, with what would help ("no welcome.txt in
~/Desktop; there: Welcome.txt, Objects, ..."), rather than reporting success
and leaving a dialog behind. *Why:* voice once "opened" `welcome.txt` when the
file was `Welcome.txt`; the desktop showed "failed to open" and voice said it
had worked.

## 7. Know where it's going (an output check)

*Not built yet (2026-09-25); for every app that makes something to print, send or publish.*

A document is made for somewhere: a letter-size page, an email read on a phone, a slide on
a projector, a web page, a label. The app should know the destination and check against it
**before** it goes, the way a spelling checker checks words:

- **Fits**: a table or a drawing wider than the page, text past the margin, a slide's text
  running off it. Offer the fix: shrink to fit, landscape, wrap, or **tile** it across pages
  with overlap marks.
- **Readable there**: 7 pt text on paper, 12 px on a projector, light grey on white, a
  picture too small to read on a phone, a scanned page that is only an image.
- **Styled at all**: a plain wall of text with no headings sent as a report; mixed fonts
  pasted in from three places.
- **Right language and form**: a document in English with a Spanish date, a letter using
  A4 going to the US, a spreadsheet with the wrong decimal comma for the recipient, a
  greeting to the wrong name.
- **Nothing that should not leave**: comments, tracked changes, hidden sheets, a password
  in the text (the same `looksSensitive` rule the AI uses).

Each check is a rule anyone can run without AI; an AI pass can add judgment (is this
readable, is the tone right for this recipient). Findings are suggestions with one-tap
fixes, never silent changes: the person decides (rule 4). And, as everywhere, each check
and fix is an action (rule 3), so voice can "make it fit on one page".

Where it belongs: Writer, Spreadsheet and Slides (print, PDF, email), Draw (print and
export sizes), Webmaster (publish: pages checked at phone width), Cabinet (outgoing
copies). It sits naturally in the app kit, next to `AppControl`: a destination
(page size and margins, screen width, medium) plus a list of checks each app fills in.
Printing through "My device" is where it pays first: the destination is a real sheet of
paper.

## 8. Nothing to lose, nothing to name

*From using the desk, 2026-09-25: a frozen desk lost unsaved work, and every
app then asked "restore?" at start.*

A document saves itself. A new one, as soon as there is something to it, gets a
file of its own, named by its heading or the AI ("Quarterly report for Acme",
not "Untitled 3"), in the place this desk or computer keeps documents
(`~/.config/dbbasic/places.env`: one setting for every app, each app in a folder
of its own name). A note says where, with Rename. After that it keeps saving to its
file; closing it asks nothing. Crash recovery is only the safety net for the
seconds in between, and it is offered in the app's own words, never a browser
dialog.

Save, Save As and a name you choose still work: the AI's name is a suggestion
(rule 4), never for text that looks like credentials.

Where it shows today: Writer (auto-save and AI names), Webmaster (auto-save once
a site has a folder). Next: Spreadsheet, Slides, Draw, Cabinet.

