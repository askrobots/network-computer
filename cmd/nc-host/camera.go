package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/h264writer"
	"github.com/pion/webrtc/v4/pkg/media/ivfwriter"
)

// The client's camera as a webcam on this machine. The client opens a second
// video transceiver (sending) at connect with no camera attached, and attaches
// one when the person taps 📷, so the camera light is only on while it is in
// use. Its frames are decoded by ffmpeg into a v4l2loopback device
// ("network-computer camera", -camera-device), which Firefox, Jitsi, Zoom or
// any other app on the desk picks like a real webcam.
//
// Only one camera at a time: the newest one to start sending takes the device.
// Frames stop (camera off, the app in the background): after a short wait
// ffmpeg is closed and the device released; the next frame starts it again.
const (
	camWidth, camHeight = 1280, 720
	camIdle             = 2 * time.Second
)

// camArgs decodes a stream of the given format from stdin into the device,
// letterboxed to a fixed size so a phone turning sideways does not change the
// device's format under the apps using it.
func camArgs(format, device string) []string {
	vf := "scale=1280:720:force_original_aspect_ratio=decrease,pad=1280:720:(ow-iw)/2:(oh-ih)/2,format=yuv420p"
	return []string{"-hide_banner", "-loglevel", "error",
		"-fflags", "nobuffer", "-flags", "low_delay", "-probesize", "32", "-analyzeduration", "0",
		"-f", format, "-i", "pipe:0",
		"-vf", vf, "-r", "30", "-f", "v4l2", device}
}

// camWriter depacketizes RTP into a stream ffmpeg can read.
type camWriter interface {
	WriteRTP(*rtp.Packet) error
	Close() error
}

func camFormat(mime string) string {
	switch strings.ToLower(mime) {
	case strings.ToLower(webrtc.MimeTypeH264):
		return "h264"
	case strings.ToLower(webrtc.MimeTypeVP8), strings.ToLower(webrtc.MimeTypeVP9), strings.ToLower(webrtc.MimeTypeAV1):
		return "ivf"
	}
	return ""
}

func newCamWriter(mime string, w io.Writer) (camWriter, error) {
	if camFormat(mime) == "h264" {
		return h264writer.NewWith(w), nil
	}
	return ivfwriter.NewWith(w, ivfwriter.WithCodec(mime))
}

// camOwner lets only the newest camera write to the device.
type camOwner struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	id     int
}

func (c *camOwner) take(cancel context.CancelFunc) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.id++
	c.cancel = cancel
	return c.id
}

func (c *camOwner) current() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.id
}

func (c *camOwner) release(id int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.id == id {
		c.cancel = nil
	}
}

// playCamera copies one incoming video track into the camera device until the
// session ends. Without a device it just drains the track.
func (h *host) playCamera(ctx context.Context, pc *webrtc.PeerConnection, track *webrtc.TrackRemote) {
	if h.camera == "" {
		drain(track)
		return
	}
	mime := track.Codec().MimeType
	log.Printf("camera: client camera track (%s)", mime)
	for ctx.Err() == nil {
		// wait for the camera to start (or start again)
		track.SetReadDeadline(time.Time{})
		first, _, err := track.ReadRTP()
		if err != nil {
			log.Printf("camera: track ended (%v)", err)
			return
		}
		if len(first.Payload) == 0 {
			continue
		}
		err = h.cameraRun(ctx, pc, track, mime, first)
		if errors.Is(err, errPreempted) {
			// another device's camera has the webcam: ignore this one until it
			// stops sending, or it would take the device straight back
			log.Printf("camera: another device's camera took over")
			for {
				track.SetReadDeadline(time.Now().Add(camIdle))
				if _, _, err := track.ReadRTP(); err != nil {
					if isTimeout(err) {
						break
					}
					return
				}
			}
			continue
		}
		if err != nil {
			log.Printf("camera: %v", err)
			if strings.Contains(err.Error(), "unsupported") {
				drain(track)
				return
			}
			time.Sleep(time.Second)
		}
	}
}

var errPreempted = errors.New("preempted")

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

// cameraRun runs ffmpeg for one stretch of frames: until they stop for
// camIdle, the session ends, or a newer camera takes over.
func (h *host) cameraRun(ctx context.Context, pc *webrtc.PeerConnection, track *webrtc.TrackRemote, mime string, first *rtp.Packet) error {
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	id := h.cam.take(cancel)
	defer h.cam.release(id)

	format := camFormat(mime)
	if format == "" {
		return errors.New("unsupported camera codec " + mime)
	}
	// ffmpeg first: the IVF writer writes its header into the pipe at once,
	// which blocks until someone reads it
	pr, pw := io.Pipe()
	args := camArgs(format, h.camera)
	cmd := exec.CommandContext(rctx, "ffmpeg", args...)
	cmd.Stdin = pr
	cmd.Stderr = os.Stderr
	log.Printf("ffmpeg(camera) %s", strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		return err
	}
	w, err := newCamWriter(mime, pw)
	if err != nil {
		pw.Close()
		cmd.Wait()
		return err
	}
	log.Printf("camera: on -> %s (%s)", h.camera, mime)
	done := make(chan struct{})
	go func() { cmd.Wait(); pr.CloseWithError(io.EOF); close(done) }()

	// a keyframe now, and again a few times while ffmpeg starts: the camera
	// may have begun mid-stream and nothing decodes before one
	pli := func() {
		pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
	}
	pli()
	go func() {
		for i := 0; i < 3; i++ {
			select {
			case <-time.After(time.Second):
				pli()
			case <-rctx.Done():
				return
			}
		}
	}()

	frames := 0
	pkt := first
	for {
		if len(pkt.Payload) > 0 {
			if err := w.WriteRTP(pkt); err != nil {
				break // ffmpeg is gone
			}
			if pkt.Marker {
				frames++
			}
		}
		if rctx.Err() != nil {
			break
		}
		track.SetReadDeadline(time.Now().Add(camIdle))
		if pkt, _, err = track.ReadRTP(); err != nil {
			break // stopped sending, or the session closed
		}
	}
	w.Close()
	pw.Close()
	cancel()
	<-done
	log.Printf("camera: off after %d frames", frames)
	if ctx.Err() == nil && h.cam.current() != id {
		return errPreempted
	}
	return nil
}
