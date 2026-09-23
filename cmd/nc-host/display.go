package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/askrobots/network-computer/internal/proto"
)

// Clients can ask for a screen size and UI scale. On Linux the host resizes
// its X screen and sets the desktop's scale (nc-display, from provisioning);
// every OS then restarts capture at the new size, so what the client sees is
// pixel for pixel instead of scaled.

const (
	minW, minH = 640, 360
	maxW, maxH = 2560, 1600
)

type displayReq struct {
	W, H int
	S    float64
}

// normalize clamps a request to what the host will do: sizes within bounds,
// width a multiple of 8 (display mode timings need it), height even (4:2:0
// video needs it), scale in 5% steps.
func normalize(e proto.InputEvent) displayReq {
	clamp := func(v, lo, hi int) int { return int(math.Max(float64(lo), math.Min(float64(hi), float64(v)))) }
	w, h := clamp(e.W, minW, maxW)/8*8, clamp(e.H, minH, maxH)/2*2
	s := e.S
	if s <= 0 {
		s = 1
	}
	s = math.Round(math.Max(1, math.Min(3, s))*20) / 20
	return displayReq{w, h, s}
}

// bitrateFor scales the configured bitrate (tuned for 1280x720) by pixel count.
func bitrateFor(base string, w, h int) string {
	b := parseBitrate(base)
	if b <= 0 {
		b = 4_000_000
	}
	scaled := float64(b) * float64(w*h) / (1280 * 720)
	scaled = math.Max(1_500_000, math.Min(16_000_000, scaled))
	return fmt.Sprintf("%dk", int(scaled/1000))
}

func parseBitrate(s string) int {
	s = strings.TrimSpace(strings.ToLower(s))
	mult := 1
	switch {
	case strings.HasSuffix(s, "m"):
		mult, s = 1_000_000, strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "k"):
		mult, s = 1_000, strings.TrimSuffix(s, "k")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int(f * float64(mult))
}

// displayControl applies requests one at a time and skips repeats. The X
// screen is shared, so the most recent request from any client wins.
type displayControl struct {
	mu   sync.Mutex
	last displayReq
}

func (d *displayControl) apply(req displayReq) (changed bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if req == d.last {
		return false
	}
	if runtime.GOOS == "linux" {
		out, err := exec.CommandContext(context.Background(), "nc-display",
			strconv.Itoa(req.W), strconv.Itoa(req.H), strconv.FormatFloat(req.S, 'f', 2, 64)).CombinedOutput()
		if err != nil {
			log.Printf("display: nc-display %dx%d @%.2f failed: %v %s", req.W, req.H, req.S, err, strings.TrimSpace(string(out)))
			return false
		}
	}
	log.Printf("display: %dx%d at %.0f%% scale", req.W, req.H, req.S*100)
	d.last = req
	return true
}

func (d *displayControl) current() displayReq {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.last
}
