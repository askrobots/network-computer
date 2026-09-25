package main

import (
	"context"
	"log"
	"sync"

	"github.com/pion/webrtc/v4/pkg/media"
)

// A hub runs one capture and encoder and gives every connected device a copy
// of its samples. The desk has one screen and one sound output: five devices
// used to mean five x11grab+x264 encoders (about 14% of a core each), which
// together with anything else pinned a 2-core desk (2026-09-25).
//
// Video: a device that joins mid-stream starts at the next keyframe (the GOP
// is 2 s), since frames before it cannot be decoded. A new size (a device
// asked the screen to change) restarts the one encoder for everyone, as the
// screen changed for everyone. The encoder stops when the last device leaves.
type hub struct {
	name  string
	gated bool // wait for a keyframe before a new subscriber gets samples (video)
	run   func(ctx context.Context, opts captureOpts, w sampleWriter, onKeyframe func())

	mu      sync.Mutex
	subs    map[*hubSub]struct{}
	opts    captureOpts
	cancel  context.CancelFunc
	done    chan struct{}
	nextKey bool
}

type hubSub struct {
	w       sampleWriter
	waiting bool
}

func newHub(name string, gated bool, run func(ctx context.Context, opts captureOpts, w sampleWriter, onKeyframe func())) *hub {
	return &hub{name: name, gated: gated, run: run, subs: map[*hubSub]struct{}{}}
}

// join adds a device's track; the encoder starts with the first one. The
// returned function removes it again.
func (h *hub) join(w sampleWriter, opts captureOpts) (leave func()) {
	s := &hubSub{w: w, waiting: h.gated}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	if h.cancel == nil {
		h.startLocked(opts)
	}
	n := len(h.subs)
	h.mu.Unlock()
	log.Printf("%s: %d device(s) on one encoder", h.name, n)
	return func() {
		h.mu.Lock()
		delete(h.subs, s)
		empty := len(h.subs) == 0
		h.mu.Unlock()
		if empty {
			h.stop()
		}
	}
}

// restart runs the encoder with new options (a new screen size) for everyone.
func (h *hub) restart(opts captureOpts) {
	h.stop()
	h.mu.Lock()
	for h.cancel != nil { // a device joined meanwhile and started one: replace it
		h.mu.Unlock()
		h.stop()
		h.mu.Lock()
	}
	defer h.mu.Unlock()
	if len(h.subs) == 0 {
		return
	}
	for s := range h.subs {
		s.waiting = h.gated
	}
	log.Printf("%s: restarting at %dx%d, %s", h.name, opts.Width, opts.Height, opts.Bitrate)
	h.startLocked(opts)
}

func (h *hub) startLocked(opts captureOpts) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	h.opts, h.cancel, h.done = opts, cancel, done
	go func() {
		h.run(ctx, opts, h, h.keyframe)
		close(done)
	}()
}

func (h *hub) stop() {
	h.mu.Lock()
	cancel, done := h.cancel, h.done
	h.cancel, h.done = nil, nil
	h.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

// keyframe: the next sample holds a keyframe (the capture loop calls this
// while it assembles that access unit, before writing it).
func (h *hub) keyframe() {
	h.mu.Lock()
	h.nextKey = true
	h.mu.Unlock()
}

// WriteSample gives one sample to every device that can decode it.
func (h *hub) WriteSample(s media.Sample) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := h.nextKey
	h.nextKey = false
	for sub := range h.subs {
		if sub.waiting {
			if !key {
				continue
			}
			sub.waiting = false
		}
		sub.w.WriteSample(s) // one slow or closed device must not stop the rest
	}
	return nil
}
