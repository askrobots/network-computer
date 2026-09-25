package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4/pkg/media"
)

type countWriter struct {
	mu sync.Mutex
	n  int
}

func (c *countWriter) WriteSample(media.Sample) error { c.mu.Lock(); c.n++; c.mu.Unlock(); return nil }
func (c *countWriter) count() int                     { c.mu.Lock(); defer c.mu.Unlock(); return c.n }

// A fake encoder: a keyframe every 10th sample, until cancelled.
func fakeEncoder(starts *int, mu *sync.Mutex) func(context.Context, captureOpts, sampleWriter, func()) {
	return func(ctx context.Context, _ captureOpts, w sampleWriter, key func()) {
		mu.Lock()
		*starts++
		mu.Unlock()
		for i := 0; ctx.Err() == nil; i++ {
			if i%10 == 0 {
				key()
			}
			w.WriteSample(media.Sample{Data: []byte{byte(i)}})
			time.Sleep(time.Millisecond)
		}
	}
}

func TestHubSharesOneEncoder(t *testing.T) {
	var starts int
	var mu sync.Mutex
	h := newHub("video", true, fakeEncoder(&starts, &mu))
	n := func() int { mu.Lock(); defer mu.Unlock(); return starts }
	a, b := &countWriter{}, &countWriter{}
	leaveA := h.join(a, captureOpts{})
	time.Sleep(30 * time.Millisecond)
	leaveB := h.join(b, captureOpts{})
	time.Sleep(60 * time.Millisecond)
	if n() != 1 {
		t.Fatalf("two devices started %d encoders, want 1", n())
	}
	if a.count() == 0 || b.count() == 0 {
		t.Fatalf("both devices get samples: a=%d b=%d", a.count(), b.count())
	}
	h.restart(captureOpts{Width: 800})
	time.Sleep(30 * time.Millisecond)
	if n() != 2 {
		t.Fatalf("restart: %d starts, want 2", n())
	}
	leaveA()
	leaveB()
	h.mu.Lock()
	running := h.cancel != nil
	h.mu.Unlock()
	if running {
		t.Fatal("the encoder keeps running with nobody left")
	}
}

func TestHubLateJoinerStartsAtKeyframe(t *testing.T) {
	var starts int
	var mu sync.Mutex
	got := []byte{}
	var gmu sync.Mutex
	h := newHub("video", true, fakeEncoder(&starts, &mu))
	first := &countWriter{}
	leave := h.join(first, captureOpts{})
	defer leave()
	time.Sleep(15 * time.Millisecond) // mid-GOP
	late := sampleFunc(func(s media.Sample) { gmu.Lock(); got = append(got, s.Data[0]); gmu.Unlock() })
	leaveLate := h.join(late, captureOpts{})
	time.Sleep(40 * time.Millisecond)
	leaveLate()
	gmu.Lock()
	defer gmu.Unlock()
	if len(got) == 0 || got[0]%10 != 0 {
		t.Fatalf("a late device's first sample is %v, want a keyframe (multiple of 10)", got)
	}
}

type sampleFunc func(media.Sample)

func (f sampleFunc) WriteSample(s media.Sample) error { f(s); return nil }
