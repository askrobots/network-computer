package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/oggreader"
)

// audioArgs captures system audio with ffmpeg and emits Ogg Opus on stdout.
// On macOS there is no system loopback without a virtual device (BlackHole,
// open source) so -audio-device names an avfoundation audio index.
func audioArgs(device string) []string {
	var in []string
	switch runtime.GOOS {
	case "darwin":
		in = []string{"-f", "avfoundation", "-i", "none:" + device}
	case "linux":
		if device == "" {
			device = "default"
		}
		// 10 ms capture fragments (48 kHz, stereo, 16-bit); the default is
		// ~50 ms, which arrives in clumps
		in = []string{"-f", "pulse", "-fragment_size", "1920", "-i", device}
	case "windows":
		in = []string{"-f", "dshow", "-i", "audio=" + device}
	}
	args := []string{"-hide_banner", "-loglevel", "warning", "-fflags", "nobuffer"}
	args = append(args, in...)
	return append(args, "-c:a", "libopus", "-b:a", "128k", "-ar", "48000",
		"-application", "audio", "-fec", "1", "-packet_loss", "10",
		"-frame_duration", "20", "-page_duration", "20000", "-flush_packets", "1", "-f", "opus", "pipe:1")
}

func streamAudio(ctx context.Context, device string, track sampleWriter) {
	for ctx.Err() == nil {
		args := audioArgs(device)
		log.Printf("ffmpeg(audio) %s", strings.Join(args, " "))
		cmd := exec.CommandContext(ctx, "ffmpeg", args...)
		cmd.Stderr = os.Stderr
		stdout, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			log.Printf("audio ffmpeg: %v", err)
			time.Sleep(time.Second)
			continue
		}
		ogg, _, err := oggreader.NewWith(stdout)
		if err != nil {
			log.Printf("ogg: %v", err)
		} else {
			var lastGranule uint64
			// Pace packets on their own clock, one 20 ms frame every 20 ms.
			// Capture arrives in clumps; sent as they came, the receiver saw
			// packets bunch up then pause and kept speeding up and slowing
			// down playback to cover it (a warbly, old-streaming sound).
			var start time.Time
			var sent time.Duration
			for {
				page, hdr, err := ogg.ParseNextPage()
				if err != nil {
					break
				}
				if bytes.HasPrefix(page, []byte("OpusTags")) {
					continue // metadata page, not audio
				}
				samples := hdr.GranulePosition - lastGranule
				lastGranule = hdr.GranulePosition
				d := time.Duration(samples) * time.Second / 48000
				if d <= 0 || d > 200*time.Millisecond {
					d = 20 * time.Millisecond
				}
				if start.IsZero() {
					start = time.Now()
				}
				if wait := time.Until(start.Add(sent)); wait > 0 {
					time.Sleep(wait)
				} else if -wait > 200*time.Millisecond {
					start, sent = time.Now(), 0 // fell far behind: resync rather than burst to catch up
				}
				if err := track.WriteSample(media.Sample{Data: page, Duration: d}); err != nil {
					log.Printf("audio write: %v", err)
				}
				sent += d
			}
		}
		cmd.Wait()
		if ctx.Err() == nil {
			log.Printf("audio ffmpeg exited, restarting")
			time.Sleep(time.Second)
		}
	}
}
