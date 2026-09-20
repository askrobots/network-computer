package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pion/webrtc/v4/pkg/media"
)

// captureOpts describes one ffmpeg screen pipeline.
type captureOpts struct {
	Display int // index of the screen to capture
	FPS     int
	Width   int // 0 = native
	Height  int
	Bitrate string // e.g. "8M"
	Encoder string // ffmpeg encoder name, "" = platform default
	Extra   string // raw extra ffmpeg args inserted before the output
	Custom  string // full custom ffmpeg argument string; overrides everything
}

// ffmpegArgs builds a low-latency H.264 Annex-B pipeline for the current OS.
// Every branch ends with "-f h264 pipe:1" so the reader is identical.
func ffmpegArgs(o captureOpts) []string {
	if o.Custom != "" {
		return strings.Fields(o.Custom)
	}
	fps := strconv.Itoa(o.FPS)
	var in []string
	enc := o.Encoder
	switch runtime.GOOS {
	case "darwin":
		// avfoundation screen devices come after the cameras; the index is
		// looked up by name at startup in resolveDarwinScreen.
		in = []string{"-f", "avfoundation", "-capture_cursor", "1", "-capture_mouse_clicks", "0",
			"-framerate", fps, "-pixel_format", "uyvy422", "-i", fmt.Sprintf("%d:none", o.Display)}
		if enc == "" {
			enc = "h264_videotoolbox"
		}
	case "linux":
		disp := os.Getenv("DISPLAY")
		if disp == "" {
			disp = ":0"
		}
		in = []string{"-f", "x11grab", "-framerate", fps, "-draw_mouse", "1", "-i", fmt.Sprintf("%s.%d", disp, o.Display)}
		if enc == "" {
			enc = "libx264"
		}
	case "windows":
		in = []string{"-f", "gdigrab", "-framerate", fps, "-draw_mouse", "1", "-i", "desktop"}
		if enc == "" {
			enc = "libx264"
		}
	default:
		log.Fatalf("no default capture for %s; use -ffmpeg-args", runtime.GOOS)
	}

	args := []string{"-hide_banner", "-loglevel", "warning", "-fflags", "nobuffer", "-flags", "low_delay"}
	args = append(args, in...)
	// passthrough: never duplicate or drop frames to hit a nominal rate; the
	// capture device sets the pace and the RTP clock follows real timestamps.
	args = append(args, "-fps_mode", "passthrough")
	if o.Width > 0 && o.Height > 0 {
		args = append(args, "-vf", fmt.Sprintf("scale=%d:%d", o.Width, o.Height))
	}
	args = append(args, "-c:v", enc, "-b:v", o.Bitrate, "-maxrate", o.Bitrate,
		"-g", strconv.Itoa(o.FPS*2), "-bf", "0", "-pix_fmt", "yuv420p")
	switch enc {
	case "h264_videotoolbox":
		args = append(args, "-realtime", "1", "-prio_speed", "1", "-profile:v", "main", "-allow_sw", "1")
	case "libx264":
		args = append(args, "-preset", "ultrafast", "-tune", "zerolatency", "-profile:v", "baseline", "-x264-params", "repeat-headers=1")
	case "h264_vaapi", "h264_nvenc", "h264_qsv", "h264_amf":
		// caller usually needs -Extra for hwupload / device selection
	}
	if o.Extra != "" {
		args = append(args, strings.Fields(o.Extra)...)
	}
	return append(args, "-f", "h264", "pipe:1")
}

// resolveDarwinScreen maps "screen N" to the avfoundation device index by
// parsing ffmpeg's device list, since cameras shift the numbering.
func resolveDarwinScreen(n int) (int, error) {
	out, _ := exec.Command("ffmpeg", "-hide_banner", "-f", "avfoundation", "-list_devices", "true", "-i", "").CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, fmt.Sprintf("Capture screen %d", n)) {
			i := strings.Index(line, "] [")
			if i < 0 {
				continue
			}
			rest := line[i+3:]
			j := strings.Index(rest, "]")
			return strconv.Atoi(rest[:j])
		}
	}
	return 0, fmt.Errorf("screen %d not found in avfoundation device list", n)
}

// streamVideo runs ffmpeg and pushes one access unit per WriteSample until
// ctx is cancelled. It restarts ffmpeg if it dies.
func streamVideo(ctx context.Context, o captureOpts, track sampleWriter, onKeyframe func()) {
	for ctx.Err() == nil {
		if err := runOnce(ctx, o, track, onKeyframe); err != nil && ctx.Err() == nil {
			log.Printf("ffmpeg: %v (restarting in 1s)", err)
			time.Sleep(time.Second)
		}
	}
}

type sampleWriter interface {
	WriteSample(media.Sample) error
}

func runOnce(ctx context.Context, o captureOpts, track sampleWriter, onKeyframe func()) error {
	args := ffmpegArgs(o)
	log.Printf("ffmpeg %s", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer cmd.Wait()

	r := bufio.NewReaderSize(stdout, 4<<20)
	var au []byte // access unit being assembled (Annex-B, may hold SPS/PPS/SEI + slice)
	auHasSlice := false
	last := time.Now()
	frames, bytesOut := 0, 0
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	flush := func() {
		if len(au) == 0 {
			return
		}
		now := time.Now()
		d := now.Sub(last)
		last = now
		if d > time.Second { // first frame or a stall: don't blow up the RTP clock
			d = time.Second / time.Duration(o.FPS)
		}
		if err := track.WriteSample(media.Sample{Data: au, Duration: d}); err != nil {
			log.Printf("write sample: %v", err)
		}
		frames++
		bytesOut += len(au)
		au = nil
		auHasSlice = false
	}

	for nal := range nalUnits(r) {
		if len(nal) == 0 {
			continue
		}
		typ := nal[0] & 0x1f
		isSlice := typ == 1 || typ == 5
		// A new slice after we already hold one means a new frame started.
		if isSlice && auHasSlice && firstMB(nal) {
			flush()
		}
		if typ == 5 && onKeyframe != nil {
			onKeyframe()
		}
		au = append(au, 0, 0, 0, 1)
		au = append(au, nal...)
		if isSlice {
			auHasSlice = true
		}
		select {
		case <-tick.C:
			log.Printf("video: %.0f fps, %.1f Mbit/s", float64(frames)/5, float64(bytesOut)*8/5/1e6)
			frames, bytesOut = 0, 0
		default:
		}
	}
	flush()
	return fmt.Errorf("ffmpeg stdout closed")
}

// firstMB reports whether a slice NAL has first_mb_in_slice == 0, i.e. it is
// the first slice of a picture. ue(v) == 0 is encoded as a leading 1 bit.
func firstMB(nal []byte) bool {
	return len(nal) > 1 && nal[1]&0x80 != 0
}

// nalUnits yields NAL units (without start codes) from an Annex-B stream.
func nalUnits(r *bufio.Reader) func(yield func([]byte) bool) {
	return func(yield func([]byte) bool) {
		var buf []byte
		tmp := make([]byte, 64<<10)
		start := -1 // index in buf where the current NAL payload starts
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				for {
					// look for the next start code after 'start'
					from := 0
					if start >= 0 {
						from = start
					}
					idx := bytes.Index(buf[from:], []byte{0, 0, 1})
					if idx < 0 {
						break
					}
					idx += from
					if start >= 0 {
						end := idx
						if end > 0 && buf[end-1] == 0 { // 4-byte start code
							end--
						}
						if !yield(buf[start:end]) {
							return
						}
					}
					start = idx + 3
					// drop consumed bytes to keep buf small
					if start > 1<<20 {
						buf = append([]byte(nil), buf[start:]...)
						start = 0
					}
				}
			}
			if err != nil {
				if start >= 0 && start < len(buf) {
					yield(buf[start:])
				}
				if err != io.EOF {
					log.Printf("read ffmpeg: %v", err)
				}
				return
			}
		}
	}
}
