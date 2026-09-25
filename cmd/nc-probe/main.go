// nc-probe: headless client. Connects to a host through the rendezvous the
// same way the phone will, receives the video track, and reports which ICE
// path was chosen and how much video arrives. Use it to answer the NAT
// questions from any network without a phone or a browser.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/ivfreader"

	"github.com/askrobots/network-computer/internal/oggopus"
	"github.com/askrobots/network-computer/internal/proto"
	"github.com/askrobots/network-computer/internal/tlspin"
)

func main() {
	rz := flag.String("rendezvous", "http://127.0.0.1:8765", "rendezvous base URL")
	hostName := flag.String("host", "", "host name to connect to (default: first registered)")
	dur := flag.Duration("duration", 10*time.Second, "how long to receive before exiting")
	relayOnly := flag.Bool("relay", false, "force TURN relay (tests the fallback path)")
	user := flag.String("user", envOr("NC_USER", "nc"), "rendezvous basic auth username")
	password := flag.String("password", os.Getenv("NC_PASSWORD"), "rendezvous basic auth password (env NC_PASSWORD)")
	pin := flag.String("pin", os.Getenv("NC_PIN"), "host PIN (env NC_PIN)")
	tlsFP := flag.String("tls-fingerprint", os.Getenv("NC_TLS_FP"), "pin the rendezvous certificate by SHA-256 (env NC_TLS_FP)")
	pair := flag.String("pair", os.Getenv("NC_PAIR"), "pairing token from an earlier connection, used instead of the PIN (env NC_PAIR)")
	sendInput := flag.Bool("input", false, "send a few test input events over the data channel")
	tz := flag.String("tz", "", "tell the host this client's time zone, e.g. America/New_York")
	clickAt := flag.String("click", "", "click at X,Y (0..1 across the screen) with no move first, as a click whose move arrived late")
	keyboard := flag.String("keyboard", "", "tell the host this client's keyboard layout over the control channel, e.g. us:dvorak")
	display := flag.String("display", "", "ask the host for a screen size and scale over the control channel, e.g. 1600x900@1.5")
	clip := flag.String("clip", "", "put this text (or @file's contents) on the host clipboard, then log what the host sends back; \"-\" only watches, \"?\" asks for the host clipboard as it is")
	sendPath := flag.String("send-file", "", "send this file to the host the way a drop on the web client does")
	recvDir := flag.String("recv-dir", "", "accept files the host sends (nc-send) and save them here")
	camTest := flag.Duration("camera-test", 0, "after this long, send a test pattern as the probe's camera (as the app attaches its camera on demand), to test camera passthrough")
	micTone := flag.Int("mic-tone", 0, "send a sine tone of this frequency (Hz) as the probe's microphone, to test mic passthrough")
	flag.Parse()

	httpc := tlspin.Client(*tlsFP)
	authGet := func(url string) *http.Response {
		req, _ := http.NewRequest("GET", url, nil)
		req.SetBasicAuth(*user, *password)
		resp, err := httpc.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		if resp.StatusCode == http.StatusUnauthorized {
			log.Fatal("rendezvous rejected the password (use -password or NC_PASSWORD)")
		}
		return resp
	}
	var cfg proto.Config
	resp := authGet(*rz + "/config")
	json.NewDecoder(resp.Body).Decode(&cfg)
	resp.Body.Close()

	if *hostName == "" {
		var hosts []string
		resp := authGet(*rz + "/hosts")
		json.NewDecoder(resp.Body).Decode(&hosts)
		resp.Body.Close()
		if len(hosts) == 0 {
			log.Fatal("no hosts registered")
		}
		*hostName = hosts[0]
	}

	var ice []webrtc.ICEServer
	for _, s := range cfg.ICEServers {
		ice = append(ice, webrtc.ICEServer{URLs: s.URLs, Username: s.Username, Credential: s.Credential})
	}
	rtcCfg := webrtc.Configuration{ICEServers: ice}
	if *relayOnly {
		rtcCfg.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}

	ctx, cancel := context.WithTimeout(context.Background(), *dur+15*time.Second)
	defer cancel()

	wsURL := strings.Replace(strings.Replace(*rz, "https://", "wss://", 1), "http://", "ws://", 1) + "/ws"
	hdr := http.Header{}
	hdr.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(*user+":"+*password)))
	ws, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: hdr, HTTPClient: httpc})
	if err != nil {
		log.Fatal(err)
	}
	defer ws.CloseNow()
	ws.SetReadLimit(4 << 20)
	send := func(m proto.Message) {
		b, _ := json.Marshal(m)
		ws.Write(ctx, websocket.MessageText, b)
	}

	pc, err := webrtc.NewPeerConnection(rtcCfg)
	if err != nil {
		log.Fatal(err)
	}
	defer pc.Close()
	pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	var micTrack *webrtc.TrackLocalStaticSample
	if *micTone > 0 {
		// sendrecv audio: we hear the desktop and send our "microphone"
		micTrack, _ = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "mic", "probe")
		if _, err := pc.AddTrack(micTrack); err != nil {
			log.Fatalf("mic track: %v", err)
		}
	} else {
		pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	}
	// the camera: its own sending video transceiver, empty until "turned on"
	var camTrack *webrtc.TrackLocalStaticSample
	if *camTest > 0 {
		camTrack, _ = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "camera", "probe-camera")
		if _, err := pc.AddTransceiverFromTrack(camTrack, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
			log.Fatalf("camera track: %v", err)
		}
	}
	dc, _ := pc.CreateDataChannel("input", nil)
	if *keyboard != "" {
		kc, _ := pc.CreateDataChannel("control", nil)
		kc.OnOpen(func() {
			b, _ := json.Marshal(proto.InputEvent{T: "keyboard", Layout: *keyboard})
			kc.Send(b)
			log.Printf("told the host: keyboard %s", *keyboard)
		})
	}
	if *display != "" {
		var w, hgt int
		scale := 1.0
		if _, err := fmt.Sscanf(strings.Replace(*display, "@", " ", 1), "%dx%d %f", &w, &hgt, &scale); err != nil {
			if _, err := fmt.Sscanf(*display, "%dx%d", &w, &hgt); err != nil {
				log.Fatalf("-display wants WxH or WxH@scale, got %q", *display)
			}
		}
		ctl, _ := pc.CreateDataChannel("control", nil) // reliable, like the web client's
		ctl.OnOpen(func() {
			b, _ := json.Marshal(proto.InputEvent{T: "display", W: w, H: hgt, S: scale})
			ctl.Send(b)
			log.Printf("asked for screen %dx%d at %.0f%%", w, hgt, scale*100)
		})
	}

	if *clip != "" {
		watchClipboard(pc, *clip)
	}
	if *sendPath != "" {
		probeSendFile(pc, *sendPath)
	}
	if *recvDir != "" {
		probeRecvFiles(pc, *recvDir)
	}

	connected := make(chan struct{})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("pc: %s", s)
		if s == webrtc.PeerConnectionStateConnected {
			close(connected)
		}
		if s == webrtc.PeerConnectionStateFailed {
			log.Fatal("connection failed")
		}
	})
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			b, _ := json.Marshal(c.ToJSON())
			send(proto.Message{Type: "ice", To: *hostName, Candidate: b})
		}
	})

	type stats struct {
		packets, bytes, frames, keyframes int
		audioPackets, audioBytes          int
		audioGaps                         []time.Duration // time between audio packets
		audioTSDelta                      map[uint32]int  // RTP timestamp step -> count (960 = one 20 ms frame)
		first                             time.Time
	}
	st := &stats{}
	start := time.Now()
	done := make(chan struct{})
	pc.OnTrack(func(track *webrtc.TrackRemote, r *webrtc.RTPReceiver) {
		log.Printf("track: %s %s", track.Kind(), track.Codec().MimeType)
		if track.Kind() != webrtc.RTPCodecTypeVideo {
			go func() {
				var last time.Time
				var lastTS uint32
				st.audioTSDelta = map[uint32]int{}
				for {
					pkt, _, err := track.ReadRTP()
					if err != nil {
						return
					}
					now := time.Now()
					if !last.IsZero() {
						st.audioGaps = append(st.audioGaps, now.Sub(last))
					}
					last = now
					if st.audioPackets > 0 {
						st.audioTSDelta[pkt.Timestamp-lastTS]++
					}
					lastTS = pkt.Timestamp
					st.audioPackets++
					st.audioBytes += len(pkt.Payload)
				}
			}()
			return
		}
		go func() {
			defer close(done)
			for {
				pkt, _, err := track.ReadRTP()
				if err != nil {
					return
				}
				if st.first.IsZero() {
					st.first = time.Now()
					log.Printf("first video packet after %s", time.Since(start).Round(time.Millisecond))
				}
				st.packets++
				st.bytes += len(pkt.Payload)
				if pkt.Marker {
					st.frames++
				}
				if isKeyframe(pkt) {
					st.keyframes++
				}
			}
		}()
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		log.Fatal(err)
	}
	pc.SetLocalDescription(offer)
	start = time.Now()
	send(proto.Message{Type: "offer", To: *hostName, SDP: offer.SDP, PIN: *pin, Pair: *pair})
	log.Printf("offer sent to %q via %s", *hostName, wsURL)

	go func() {
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var m proto.Message
			json.Unmarshal(data, &m)
			switch m.Type {
			case "answer":
				if m.Pair != "" {
					fmt.Printf("pairing token: %s\n", m.Pair)
				}
				pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: m.SDP})
			case "ice":
				var c webrtc.ICECandidateInit
				if json.Unmarshal(m.Candidate, &c) == nil {
					pc.AddICECandidate(c)
				}
			case "error":
				log.Fatalf("rendezvous: %s", m.Error)
			}
		}
	}()

	select {
	case <-connected:
	case <-time.After(15 * time.Second):
		log.Fatal("timeout waiting for connection")
	}
	log.Printf("connected after %s", time.Since(start).Round(time.Millisecond))
	time.Sleep(300 * time.Millisecond)
	if p, err := pc.SCTP().Transport().ICETransport().GetSelectedCandidatePair(); err == nil && p != nil {
		kind := "direct"
		if p.Local.Typ == webrtc.ICECandidateTypeRelay || p.Remote.Typ == webrtc.ICECandidateTypeRelay {
			kind = "RELAYED"
		}
		fmt.Printf("path: %s  local %s %s:%d  <->  remote %s %s:%d\n", kind, p.Local.Typ, p.Local.Address, p.Local.Port, p.Remote.Typ, p.Remote.Address, p.Remote.Port)
	}

	if micTrack != nil {
		go sendTone(ctx, micTrack, *micTone)
		log.Printf("sending a %d Hz tone as the microphone", *micTone)
	}
	if camTrack != nil {
		go func() {
			select {
			case <-time.After(*camTest):
				sendCamera(ctx, camTrack)
			case <-ctx.Done():
			}
		}()
	}

	if *tz != "" {
		tc, _ := pc.CreateDataChannel("control", nil)
		tc.OnOpen(func() {
			b, _ := json.Marshal(proto.InputEvent{T: "tz", Text: *tz})
			tc.Send(b)
			log.Printf("told the host: time zone %s", *tz)
		})
	}
	if *clickAt != "" {
		var x, y float64
		if _, err := fmt.Sscanf(*clickAt, "%f,%f", &x, &y); err != nil {
			log.Fatalf("-click wants X,Y, got %q", *clickAt)
		}
		go func() {
			<-time.After(time.Second)
			for _, t := range []string{"md", "mu"} {
				b, _ := json.Marshal(proto.InputEvent{T: t, B: 0, X: x, Y: y, At: true})
				dc.Send(b)
			}
			log.Printf("clicked at %.3f,%.3f", x, y)
		}()
	}

	if *sendInput {
		go func() {
			<-time.After(time.Second)
			for i := 0; i < 5 && dc.ReadyState() == webrtc.DataChannelStateOpen; i++ {
				b, _ := json.Marshal(proto.InputEvent{T: "mm", X: 0.5 + float64(i)/50, Y: 0.5})
				dc.Send(b)
				time.Sleep(50 * time.Millisecond)
			}
			for _, e := range []proto.InputEvent{{T: "md", B: 0}, {T: "mu", B: 0}, {T: "kd", Code: "ShiftLeft"}, {T: "ku", Code: "ShiftLeft"}} {
				b, _ := json.Marshal(e)
				dc.Send(b)
			}
			log.Printf("sent test input events")
		}()
	}

	time.Sleep(*dur)
	el := time.Since(st.first).Seconds()
	if st.first.IsZero() {
		fmt.Println("NO VIDEO RECEIVED")
		os.Exit(1)
	}
	fmt.Printf("video: %d packets, %d frames (%d keyframes), %.1f fps, %.1f Mbit/s over %.1fs\n",
		st.packets, st.frames, st.keyframes, float64(st.frames)/el, float64(st.bytes)*8/el/1e6, el)
	if st.audioPackets > 0 {
		fmt.Printf("audio: %d packets, %.0f kbit/s (opus, ~50 packets/s expected)\n", st.audioPackets, float64(st.audioBytes)*8/el/1e3)
		if g := st.audioGaps; len(g) > 10 {
			sort.Slice(g, func(i, j int) bool { return g[i] < g[j] })
			pct := func(p float64) time.Duration { return g[int(p*float64(len(g)-1))] }
			bursts := 0
			for _, d := range g {
				if d < 5*time.Millisecond {
					bursts++
				}
			}
			fmt.Printf("audio timing: gaps p10 %v, p50 %v, p90 %v, p99 %v, max %v; %.0f%% arrive <5ms apart (bursts). 20ms each = smooth\n",
				pct(.10).Round(100*time.Microsecond), pct(.5).Round(100*time.Microsecond), pct(.9).Round(100*time.Microsecond),
				pct(.99).Round(100*time.Microsecond), g[len(g)-1].Round(100*time.Microsecond), 100*float64(bursts)/float64(len(g)))
			fmt.Printf("audio timestamp steps (960 = one 20 ms frame per packet): %v\n", st.audioTSDelta)
		}
	} else {
		fmt.Println("audio: none received (host started without -audio-device, or nothing playing)")
	}
}

// isKeyframe checks the H.264 payload for an IDR NAL (single NAL, STAP-A, or FU-A start).
func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func isKeyframe(p *rtp.Packet) bool {
	pl := p.Payload
	if len(pl) < 2 {
		return false
	}
	switch t := pl[0] & 0x1f; {
	case t == 5:
		return true
	case t == 24: // STAP-A
		i := 1
		for i+2 < len(pl) {
			sz := int(pl[i])<<8 | int(pl[i+1])
			i += 2
			if i < len(pl) && pl[i]&0x1f == 5 {
				return true
			}
			i += sz
		}
	case t == 28: // FU-A
		return pl[1]&0x80 != 0 && pl[1]&0x1f == 5
	}
	return false
}

// sendTone encodes a sine wave to Opus with ffmpeg and sends it as a
// microphone track, paced by the Ogg granule positions.
func sendTone(ctx context.Context, track *webrtc.TrackLocalStaticSample, hz int) {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-re",
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:sample_rate=48000", hz), "-ac", "2",
		"-c:a", "libopus", "-b:a", "64k", "-frame_duration", "20", "-page_duration", "20000",
		"-f", "opus", "pipe:1")
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		log.Printf("tone ffmpeg: %v", err)
		return
	}
	defer cmd.Wait()
	ogg := oggopus.NewReader(out)
	next := time.Now()
	for {
		pkt, err := ogg.Next()
		if err != nil {
			return
		}
		d := oggopus.Duration(pkt)
		if d <= 0 {
			d = 20 * time.Millisecond
		}
		time.Sleep(time.Until(next))
		next = next.Add(d)
		if err := track.WriteSample(media.Sample{Data: pkt, Duration: d}); err != nil {
			return
		}
	}
}

// watchClipboard exercises clipboard sync on its own control channel: it sends
// text the way the web client pastes (in pieces), asks for the host's clipboard
// the way a copy key does, and logs every clipboard the host sends.
func watchClipboard(pc *webrtc.PeerConnection, arg string) {
	text := arg
	if strings.HasPrefix(arg, "@") {
		b, err := os.ReadFile(arg[1:])
		if err != nil {
			log.Fatalf("-clip: %v", err)
		}
		text = string(b)
	}
	ask := "clipget"
	switch arg {
	case "-":
		text = ""
	case "?":
		text, ask = "", "clipnow"
	}
	ctl, _ := pc.CreateDataChannel("control", nil)
	send := func(ev proto.InputEvent) { b, _ := json.Marshal(ev); ctl.Send(b) }
	ctl.OnOpen(func() {
		if text != "" {
			rest := text
			for rest != "" {
				n := len(rest)
				if n > 8192 {
					n = 8192
					for n > 0 && !utf8.RuneStart(rest[n]) {
						n--
					}
				}
				send(proto.InputEvent{T: "clip", Text: rest[:n], More: n < len(rest)})
				rest = rest[n:]
			}
			log.Printf("clipboard: sent %d bytes to the host", len(text))
		}
		send(proto.InputEvent{T: ask})
	})
	var in strings.Builder
	ctl.OnMessage(func(m webrtc.DataChannelMessage) {
		var ev proto.InputEvent
		if json.Unmarshal(m.Data, &ev) != nil {
			return
		}
		switch ev.T {
		case "clipnone":
			log.Printf("clipboard: host reports nothing new copied")
		case "clip":
			in.WriteString(ev.Text)
			if ev.More {
				return
			}
			t := in.String()
			in.Reset()
			preview := t
			if len(preview) > 60 {
				preview = preview[:60] + "..."
			}
			log.Printf("clipboard: host sent %d bytes: %q", len(t), preview)
		}
	})
}

// probeSendFile sends a file on its own "file" channel: header, pieces, "end".
func probeSendFile(pc *webrtc.PeerConnection, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("-send-file: %v", err)
	}
	fc, _ := pc.CreateDataChannel("file", nil)
	fc.OnOpen(func() {
		start := time.Now()
		b, _ := json.Marshal(map[string]any{"name": filepath.Base(path), "size": len(data)})
		fc.SendText(string(b))
		for i := 0; i < len(data); i += 16 << 10 {
			for fc.BufferedAmount() > 4<<20 {
				time.Sleep(5 * time.Millisecond)
			}
			fc.Send(data[i:min(i+16<<10, len(data))])
		}
		fc.SendText("end")
		log.Printf("file: sent %d bytes in %v", len(data), time.Since(start).Round(time.Millisecond))
	})
	fc.OnMessage(func(m webrtc.DataChannelMessage) { log.Printf("file: host says %q", m.Data) })
}

// probeRecvFiles saves files the host pushes, answering like the web client.
func probeRecvFiles(pc *webrtc.PeerConnection, dir string) {
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "file" {
			return
		}
		var name string
		var size int64
		var buf []byte
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			switch {
			case m.IsString && name == "":
				var h struct {
					Name string `json:"name"`
					Size int64  `json:"size"`
				}
				json.Unmarshal(m.Data, &h)
				name, size = filepath.Base(h.Name), h.Size
			case m.IsString && string(m.Data) == "end":
				if int64(len(buf)) != size {
					dc.SendText(fmt.Sprintf("error: got %d of %d", len(buf), size))
					return
				}
				os.WriteFile(filepath.Join(dir, name), buf, 0o644)
				dc.SendText("ok " + name)
				log.Printf("file: received %s (%d bytes)", name, len(buf))
			case !m.IsString:
				buf = append(buf, m.Data...)
			}
		})
	})
}

// sendCamera sends a VP8 test pattern (with a clock, so frames visibly move)
// as the probe's camera, at 30 fps in real time.
func sendCamera(ctx context.Context, track *webrtc.TrackLocalStaticSample) {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-re",
		"-f", "lavfi", "-i", "testsrc2=size=640x480:rate=30",
		"-c:v", "libvpx", "-deadline", "realtime", "-b:v", "1M", "-g", "30", "-f", "ivf", "pipe:1")
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		log.Printf("camera ffmpeg: %v", err)
		return
	}
	defer cmd.Wait()
	ivf, _, err := ivfreader.NewWith(out)
	if err != nil {
		log.Printf("camera ivf: %v", err)
		return
	}
	log.Printf("camera: sending a test pattern")
	n := 0
	for {
		frame, _, err := ivf.ParseNextFrame()
		if err != nil {
			log.Printf("camera: sent %d frames", n)
			return
		}
		if err := track.WriteSample(media.Sample{Data: frame, Duration: time.Second / 30}); err != nil {
			return
		}
		n++
	}
}
