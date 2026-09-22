package main

import (
	"context"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

// micArgs plays an Ogg Opus stream from stdin into an output device, so the
// client's microphone becomes audio on this machine. On Linux the device is a
// pulse sink (nc-mic) whose monitor is remapped into a source every desktop
// app sees as a microphone. On macOS it is an output device index, normally a
// loopback such as BlackHole that apps then pick as their input.
func micArgs(device string) []string {
	in := []string{"-hide_banner", "-loglevel", "warning",
		"-fflags", "nobuffer", "-flags", "low_delay", "-probesize", "32", "-analyzeduration", "0",
		"-f", "ogg", "-i", "pipe:0"}
	switch runtime.GOOS {
	case "linux":
		return append(in, "-f", "pulse", "-buffer_duration", "40", "-device", device, "network-computer microphone")
	case "darwin":
		return append(in, "-f", "audiotoolbox", "-audio_device_index", device, "-")
	}
	return nil
}

// playMic copies one incoming Opus track into the mic device until the track
// ends or the session closes. Without a device it just drains the track.
func playMic(ctx context.Context, track *webrtc.TrackRemote, device string) {
	args := micArgs(device)
	if device == "" || args == nil {
		if device != "" {
			log.Printf("mic: no microphone output on %s yet; dropping client audio", runtime.GOOS)
		}
		drain(track)
		return
	}
	log.Printf("mic: client microphone -> %s", device)
	log.Printf("ffmpeg(mic) %s", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		log.Printf("mic ffmpeg: %v", err)
		drain(track)
		return
	}
	ogg, err := oggwriter.NewWith(stdin, 48000, 2)
	if err != nil {
		log.Printf("mic ogg: %v", err)
		stdin.Close()
		cmd.Wait()
		drain(track)
		return
	}
	packets := 0
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			break
		}
		if err := ogg.WriteRTP(pkt); err != nil {
			if err != io.ErrClosedPipe {
				log.Printf("mic write: %v", err)
			}
			break
		}
		if packets++; packets == 1 {
			log.Printf("mic: first audio packet from client")
		}
	}
	ogg.Close()
	cmd.Wait()
	log.Printf("mic: client microphone ended after %d packets", packets)
}

func drain(track *webrtc.TrackRemote) {
	for {
		if _, _, err := track.ReadRTP(); err != nil {
			return
		}
	}
}
