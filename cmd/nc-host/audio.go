package main

import (
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
		in = []string{"-f", "pulse", "-i", device}
	case "windows":
		in = []string{"-f", "dshow", "-i", "audio=" + device}
	}
	args := []string{"-hide_banner", "-loglevel", "warning", "-fflags", "nobuffer"}
	args = append(args, in...)
	return append(args, "-c:a", "libopus", "-b:a", "128k", "-application", "lowdelay", "-ar", "48000",
		"-frame_duration", "20", "-page_duration", "20000", "-f", "opus", "pipe:1")
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
			for {
				page, hdr, err := ogg.ParseNextPage()
				if err != nil {
					break
				}
				samples := hdr.GranulePosition - lastGranule
				lastGranule = hdr.GranulePosition
				d := time.Duration(samples) * time.Second / 48000
				if d <= 0 || d > 200*time.Millisecond {
					d = 20 * time.Millisecond
				}
				if err := track.WriteSample(media.Sample{Data: page, Duration: d}); err != nil {
					log.Printf("audio write: %v", err)
				}
			}
		}
		cmd.Wait()
		if ctx.Err() == nil {
			log.Printf("audio ffmpeg exited, restarting")
			time.Sleep(time.Second)
		}
	}
}
