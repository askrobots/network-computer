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
