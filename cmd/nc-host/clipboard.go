package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pion/webrtc/v4"

	"github.com/askrobots/network-computer/internal/proto"
)

// Clipboard sync, text only, on the reliable "control" channel:
//   - client to desk: {"t":"clip","text":...} pieces, then the paste keystroke.
//     Messages on one channel are handled in order, so the desk clipboard is
//     set before the keystroke arrives.
//   - desk to client: the desk clipboard is watched while a client is
//     connected and new text is sent the same way. {"t":"clipget"} (the client
//     just pressed copy) watches closely for a second and answers
//     {"t":"clipnone"} if nothing new was copied. {"t":"clipnow"} (a button)
//     asks for whatever the desk clipboard holds, new or not.
const (
	clipPiece = 8 << 10 // bytes of text per message, well under SCTP limits even when JSON-escaped
	clipMax   = 4 << 20
	clipIdle  = 500 * time.Millisecond
	clipFast  = 50 * time.Millisecond
	clipWait  = time.Second
)

type clipSync struct {
	mu    sync.Mutex
	last  string // what the desk clipboard holds as far as both sides know
	known bool   // last has been set, by the first look or by the client
	dc    *webrtc.DataChannel
	out   sync.Mutex // one text's pieces at a time
	in    strings.Builder
	poke  chan struct{}
}

func newClipSync() *clipSync { return &clipSync{poke: make(chan struct{}, 1)} }

// receive handles a piece of client clipboard text; on the last piece it sets
// the desk clipboard.
func (c *clipSync) receive(ev proto.InputEvent) {
	if c.in.Len()+len(ev.Text) > clipMax {
		log.Printf("clipboard: client text over %d bytes, dropped", clipMax)
		c.in.Reset()
		return
	}
	c.in.WriteString(ev.Text)
	if ev.More {
		return
	}
	text := c.in.String()
	c.in.Reset()
	c.mu.Lock()
	c.last, c.known = text, true
	c.mu.Unlock()
	if err := clipWrite(text); err != nil {
		log.Printf("clipboard: set: %v", err)
		return
	}
	log.Printf("clipboard: client pasted %d bytes", len(text))
}

// get is the client asking for what the user is about to copy.
func (c *clipSync) get() {
	select {
	case c.poke <- struct{}{}:
	default:
	}
}

// now sends the desk clipboard as it is, for the client's "get" button.
func (c *clipSync) now() {
	t, err := clipRead()
	if err != nil || t == "" || len(t) > clipMax {
		c.sendEvent(proto.InputEvent{T: "clipnone"})
		return
	}
	c.mu.Lock()
	c.last = t
	c.mu.Unlock()
	log.Printf("clipboard: client asked, sent %d bytes", len(t))
	c.send(t)
}

func (c *clipSync) run(ctx context.Context) {
	if !clipSupported() {
		return
	}
	if t, err := clipRead(); err == nil {
		c.mu.Lock()
		if !c.known { // what was there before this session is not news (unless the client already pasted)
			c.last, c.known = t, true
		}
		c.mu.Unlock()
	}
	var waitUntil time.Time
	for {
		every := clipIdle
		if !waitUntil.IsZero() {
			every = clipFast
		}
		select {
		case <-ctx.Done():
			return
		case <-c.poke:
			waitUntil = time.Now().Add(clipWait)
			continue
		case <-time.After(every):
		}
		t, err := clipRead()
		c.mu.Lock()
		changed := err == nil && t != "" && t != c.last && len(t) <= clipMax
		if changed {
			c.last = t
		}
		c.mu.Unlock()
		switch {
		case changed:
			log.Printf("clipboard: desk copied %d bytes, sent to the client", len(t))
			c.send(t)
			waitUntil = time.Time{}
		case !waitUntil.IsZero() && time.Now().After(waitUntil):
			c.sendEvent(proto.InputEvent{T: "clipnone"})
			waitUntil = time.Time{}
		}
	}
}

func (c *clipSync) send(text string) {
	c.out.Lock()
	defer c.out.Unlock()
	for {
		n := len(text)
		if n > clipPiece {
			n = clipPiece
			for n > 0 && !utf8.RuneStart(text[n]) {
				n--
			}
		}
		if !c.sendEvent(proto.InputEvent{T: "clip", Text: text[:n], More: n < len(text)}) {
			return
		}
		text = text[n:]
		if text == "" {
			return
		}
	}
}

func (c *clipSync) sendEvent(ev proto.InputEvent) bool {
	b, _ := json.Marshal(ev)
	if err := c.dc.SendText(string(b)); err != nil {
		log.Printf("clipboard: send: %v", err)
		return false
	}
	return true
}

// The desk's clipboard. Linux uses xclip on the session's X display (the
// service sets DISPLAY); macOS uses pbcopy/pbpaste. Other hosts: not yet.
func clipSupported() bool {
	switch runtime.GOOS {
	case "linux":
		_, err := exec.LookPath("xclip")
		return err == nil
	case "darwin":
		return true
	}
	return false
}

func clipRead() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-o", "-t", "UTF8_STRING")
	case "darwin":
		cmd = exec.CommandContext(ctx, "pbpaste", "-Prefer", "txt")
	default:
		return "", errors.New("no clipboard on " + runtime.GOOS)
	}
	out, err := cmd.Output() // fails when the clipboard holds no text (an image, or nothing)
	return string(out), err
}

func clipWrite(text string) error {
	switch runtime.GOOS {
	case "linux":
		// xclip keeps running in the background to serve the text; its output
		// must not be a pipe, or Run would wait for it to exit.
		cmd := exec.Command("xclip", "-selection", "clipboard", "-i")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			return err
		}
		// it takes ownership after forking: wait until the desk sees the text
		for deadline := time.Now().Add(500 * time.Millisecond); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if t, err := clipRead(); err == nil && t == text {
				return nil
			}
		}
		return errors.New("xclip did not take the clipboard")
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = bytes.NewBufferString(text)
		return cmd.Run()
	}
	return errors.New("no clipboard on " + runtime.GOOS)
}
