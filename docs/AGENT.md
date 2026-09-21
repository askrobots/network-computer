# Driving a network-computer host as an agent

Because a host is just a Linux (or macOS/Windows) machine running `nc-host`, an
assistant with access to it can operate it two independent ways at once. This is
worth understanding: the same box that streams a desktop to your phone is also a
machine an agent can run.

## Two surfaces

**1. A shell (SSH, or a local terminal).** Full command-line control: inspect the
system, read and write files, install and configure software, manage the `nc-*`
services, pull and rebuild from git. This is how the whole test box in `infra/`
was set up and is kept updated.

**2. The live GUI, over the same channel you use.** The input data channel carries
mouse and keyboard events, so an agent can move the pointer, click, type, use
keyboard shortcuts, and drive any application on the desktop exactly as a person
sitting in front of it would. Paired with a screen grab (`ffmpeg x11grab` on
Linux, or reading the stream), the agent can also *see* the result and react.

## What that adds up to

- Run and supervise long jobs, watch their output, react to what appears.
- Operate GUI-only apps: a browser, a design tool, anything without a CLI.
- Install and configure the environment, then use it.
- Take screenshots to verify its own actions (as in `docs/media/`).
- Do all of this on a machine behind NAT, with no public IP, reached only
  through the rendezvous.

## Demonstrated

During development, the assistant set up the entire test droplet over SSH
(ffmpeg, a headless X desktop, the services, firewall, audio, Firefox), then
drove that desktop's GUI directly: navigated Firefox to this repo, opened a text
editor, typed into it, and screen-grabbed the result to confirm. See
`docs/media/desktop.png`.

## Caution

This is powerful and largely irreversible in the moment. An agent driving a host
can change or destroy its state. Give an agent its own host or user, not one that
matters, prefer least privilege over the `root`-on-a-throwaway-droplet setup used
for testing, and keep the pairing PIN and rendezvous password secret: they are
what stand between the internet and a shell on your machine.
