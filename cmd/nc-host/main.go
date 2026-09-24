// nc-host: registers with nc-rendezvous, answers WebRTC offers, streams the
// screen (ffmpeg -> Pion) and injects input from the data channel.
package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/askrobots/network-computer/internal/input"
	"github.com/askrobots/network-computer/internal/proto"
	"github.com/askrobots/network-computer/internal/tlspin"
)

type host struct {
	name                string
	ice                 []webrtc.ICEServer
	capture             captureOpts
	audio               string
	mic                 string
	dryRun              bool
	api                 *webrtc.API
	user, password, pin string
	httpc               *http.Client // pinned when -tls-fingerprint is set
	pairs               *pairing
	display             displayControl
	filesDir            string
	idleFile            string    // "busy N" or "idle-since UNIX", for auto-stop (next to the send socket)
	idleSince           time.Time // when the last client left
	voice               voiceRelay
	injOnce             sync.Once
	inj                 input.Injector

	mu       sync.Mutex
	ws       *websocket.Conn
	sessions map[string]*session
}

type session struct {
	pc     *webrtc.PeerConnection
	cancel context.CancelFunc
	ctl    *webrtc.DataChannel // the client's "control" channel, once open
	fileIn *fileOut            // the client's "file-in" channel (the app), for files to it
}

func main() {
	rz := flag.String("rendezvous", "http://127.0.0.1:8080", "rendezvous base URL (http or https)")
	name := flag.String("name", hostname(), "name to register as")
	display := flag.Int("display", 0, "screen index to capture")
	fps := flag.Int("fps", 60, "capture frame rate")
	size := flag.String("size", "1920x1080", "scale output to WxH ('native' for no scaling). iPhone 12 class target: 1080p60")
	bitrate := flag.String("bitrate", "8M", "video bitrate")
	encoder := flag.String("encoder", "", "ffmpeg video encoder (default: h264_videotoolbox on mac, libx264 elsewhere)")
	extra := flag.String("ffmpeg-extra", "", "extra ffmpeg args inserted before the output")
	custom := flag.String("ffmpeg-args", "", "full ffmpeg argument string; must end with '-f h264 pipe:1'")
	audio := flag.String("audio-device", "", "capture audio from this device (mac: avfoundation index; linux: pulse source; '' = no audio)")
	mic := flag.String("mic-device", "", "play the client's microphone into this device (linux: pulse sink such as nc-mic; mac: output device index, e.g. BlackHole; '' = ignore)")
	user := flag.String("user", envOr("NC_USER", "nc"), "rendezvous basic auth username (env NC_USER)")
	password := flag.String("password", os.Getenv("NC_PASSWORD"), "rendezvous basic auth password (env NC_PASSWORD)")
	pin := flag.String("pin", os.Getenv("NC_PIN"), "PIN a client must present to connect to this host (env NC_PIN); generated and printed if empty")
	stateDir := flag.String("state-dir", defaultHostStateDir(), "where the pairing secret is kept")
	resetPairs := flag.Bool("reset-pairings", false, "rotate the pairing secret, revoking every paired client")
	tlsFP := flag.String("tls-fingerprint", os.Getenv("NC_TLS_FP"), "pin the rendezvous certificate by SHA-256 (env NC_TLS_FP); for a self-signed rendezvous")
	filesDir := flag.String("files-dir", "", "save files dropped on the client window here ('' = refuse them); e.g. the desk user's Desktop")
	sendSocket := flag.String("send-socket", "", "unix socket where nc-send hands over files for the connected client ('' = off)")
	dryRun := flag.Bool("dry-run", false, "log input events instead of injecting them")
	flag.Parse()

	if *pin == "" {
		*pin = randomPIN()
	}
	log.Printf("host PIN: %s   (clients must enter this to connect)", *pin)
	h := &host{name: *name, audio: *audio, mic: *mic, dryRun: *dryRun, sessions: map[string]*session{}, user: *user, password: *password, pin: *pin, filesDir: *filesDir}
	if *sendSocket != "" {
		go h.serveSend(*sendSocket)
		h.idleFile = filepath.Join(filepath.Dir(*sendSocket), "idle")
	}
	h.idleSince = time.Now()
	h.writeIdle()
	h.httpc = tlspin.Client(*tlsFP)
	pairs, err := loadPairing(*name, *stateDir, *resetPairs)
	if err != nil {
		log.Fatalf("pairing secret: %v", err)
	}
	h.pairs = pairs
	if *resetPairs {
		log.Printf("pairing secret rotated: every previously paired client must enter the PIN again")
	}
	h.capture = captureOpts{Display: *display, FPS: *fps, Bitrate: *bitrate, Encoder: *encoder, Extra: *extra, Custom: *custom}
	if *size != "" && *size != "native" {
		fmt.Sscanf(*size, "%dx%d", &h.capture.Width, &h.capture.Height)
	}
	if runtime.GOOS == "darwin" && *custom == "" {
		idx, err := resolveDarwinScreen(*display)
		if err != nil {
			log.Fatal(err)
		}
		h.capture.Display = idx
	}

	// ICE servers come from the rendezvous so host and client agree.
	var cfg proto.Config
	req, _ := http.NewRequest("GET", *rz+"/config", nil)
	req.SetBasicAuth(h.user, h.password)
	if resp, err := h.httpc.Do(req); err == nil {
		if resp.StatusCode == http.StatusUnauthorized {
			log.Fatalf("rendezvous rejected the password (use -password or NC_PASSWORD)")
		}
		json.NewDecoder(resp.Body).Decode(&cfg)
		resp.Body.Close()
	} else {
		log.Fatalf("GET %s/config: %v", *rz, err)
	}
	for _, s := range cfg.ICEServers {
		h.ice = append(h.ice, webrtc.ICEServer{URLs: s.URLs, Username: s.Username, Credential: s.Credential})
	}
	log.Printf("ice servers: %+v", cfg.ICEServers)

	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		log.Fatal(err)
	}
	h.api = webrtc.NewAPI(webrtc.WithMediaEngine(m))

	wsURL := strings.Replace(strings.Replace(*rz, "https://", "wss://", 1), "http://", "ws://", 1) + "/ws"
	for {
		if err := h.runSignaling(context.Background(), wsURL); err != nil {
			log.Printf("signaling: %v (reconnecting in 3s)", err)
		}
		time.Sleep(3 * time.Second)
	}
}

func (h *host) runSignaling(ctx context.Context, url string) error {
	hdr := http.Header{}
	hdr.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(h.user+":"+h.password)))
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: hdr, HTTPClient: h.httpc})
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)
	h.mu.Lock()
	h.ws = conn
	h.mu.Unlock()
	if err := h.send(ctx, proto.Message{Type: "register", Name: h.name}); err != nil {
		return err
	}
	log.Printf("registered as %q at %s", h.name, url)
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var m proto.Message
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		switch m.Type {
		case "offer":
			go h.handleOffer(ctx, m)
		case "ice":
			h.mu.Lock()
			s := h.sessions[m.From]
			h.mu.Unlock()
			if s != nil {
				var c webrtc.ICECandidateInit
				if json.Unmarshal(m.Candidate, &c) == nil {
					s.pc.AddICECandidate(c)
				}
			}
		case "error":
			log.Printf("rendezvous error: %s", m.Error)
		}
	}
}

func (h *host) send(ctx context.Context, m proto.Message) error {
	b, _ := json.Marshal(m)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ws == nil {
		return fmt.Errorf("no signaling connection")
	}
	return h.ws.Write(ctx, websocket.MessageText, b)
}

func (h *host) handleOffer(ctx context.Context, m proto.Message) {
	peer := m.From
	pinOK := m.PIN != "" && subtle.ConstantTimeCompare([]byte(m.PIN), []byte(h.pin)) == 1
	pairOK := m.Pair != "" && h.pairs.valid(m.Pair)
	if !pinOK && !pairOK {
		why := "wrong PIN"
		if m.Pair != "" && m.PIN == "" {
			why = "pairing expired or revoked: enter the PIN"
		}
		log.Printf("[%s] offer rejected: %s", peer, why)
		h.send(ctx, proto.Message{Type: "error", To: peer, Error: why})
		return
	}
	if pairOK {
		log.Printf("[%s] offer received, paired client", peer)
	} else {
		log.Printf("[%s] offer received, PIN ok", peer)
	}

	pc, err := h.api.NewPeerConnection(webrtc.Configuration{ICEServers: h.ice})
	if err != nil {
		log.Printf("[%s] pc: %v", peer, err)
		return
	}
	sctx, cancel := context.WithCancel(ctx)
	s := &session{pc: pc, cancel: cancel}
	h.mu.Lock()
	if old := h.sessions[peer]; old != nil {
		old.cancel()
		old.pc.Close()
	}
	h.sessions[peer] = s
	h.mu.Unlock()

	closeSession := func() {
		cancel()
		pc.Close()
		h.mu.Lock()
		if h.sessions[peer] == s {
			delete(h.sessions, peer)
		}
		nobody := len(h.sessions) == 0
		h.mu.Unlock()
		if nobody {
			h.voice.command(false, false) // no one to listen to
		}
		h.writeIdle()
	}

	video, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "nc")
	if err != nil {
		log.Printf("track: %v", err)
		closeSession()
		return
	}
	vsender, err := pc.AddTrack(video)
	if err != nil {
		log.Printf("addtrack: %v", err)
		closeSession()
		return
	}
	// Drain RTCP (PLI/NACK). We cannot ask ffmpeg for a keyframe on demand,
	// so PLIs are only logged; GOP is 2s so recovery is bounded anyway.
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := vsender.Read(buf); err != nil {
				return
			}
		}
	}()

	// The client's microphone arrives as an incoming audio track.
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if track.Kind() == webrtc.RTPCodecTypeAudio {
			go playMic(sctx, track, h.mic)
			return
		}
		go drain(track)
	})

	var audioTrack *webrtc.TrackLocalStaticSample
	if h.audio != "" {
		audioTrack, _ = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "nc")
		if asender, err := pc.AddTrack(audioTrack); err == nil {
			go func() {
				buf := make([]byte, 1500)
				for {
					if _, _, err := asender.Read(buf); err != nil {
						return
					}
				}
			}()
		}
	}

	restartCapture := make(chan captureOpts, 1)
	inj := h.injector()
	clip := newClipSync()
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Printf("[%s] data channel %q", peer, dc.Label())
		if dc.Label() == "file" {
			receiveFile(peer, dc, h.filesDir)
			return
		}
		if dc.Label() == "file-in" { // the app receives files on a channel it opened
			o := newFileOut(dc)
			h.mu.Lock()
			s.fileIn = o
			h.mu.Unlock()
			return
		}
		if dc.Label() == "control" {
			clip.dc = dc
			h.mu.Lock()
			s.ctl = dc
			h.mu.Unlock()
			dc.OnOpen(func() { go clip.run(sctx) })
		}
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			var ev proto.InputEvent
			if err := json.Unmarshal(msg.Data, &ev); err != nil {
				return
			}
			switch ev.T {
			case "keyboard":
				go setKeyboard(ev.Layout)
				return
			case "clip":
				clip.receive(ev) // not in a goroutine: the paste keystroke that follows must find it
				return
			case "clipget":
				clip.get()
				return
			case "clipnow":
				go clip.now()
				return
			case "launch":
				go launch()
				return
			case "tz":
				go setTimezone(ev.Text)
				return
			case "voice":
				if !h.voice.command(ev.On, ev.Once) {
					b, _ := json.Marshal(proto.InputEvent{T: "voice", Kind: "error", Text: "voice is not running on this desk"})
					dc.SendText(string(b))
				}
				return
			}
			if ev.T == "display" {
				go func() {
					req := normalize(ev)
					if h.display.apply(req) {
						opts := h.capture
						opts.Width, opts.Height = req.W, req.H
						opts.Bitrate = bitrateFor(h.capture.Bitrate, req.W, req.H)
						select {
						case restartCapture <- opts:
						default: // a restart is already queued; the newer size follows
						}
					}
				}()
				return
			}
			if (ev.T == "md" || ev.T == "mu") && ev.At {
				inj.Handle(proto.InputEvent{T: "mm", X: ev.X, Y: ev.Y}) // click where it was pressed
			}
			inj.Handle(ev)
		})
	})

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		b, _ := json.Marshal(c.ToJSON())
		h.send(ctx, proto.Message{Type: "ice", To: peer, Candidate: b})
	})
	pc.OnICEConnectionStateChange(func(st webrtc.ICEConnectionState) {
		log.Printf("[%s] ice: %s", peer, st)
	})
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		log.Printf("[%s] pc: %s", peer, st)
		switch st {
		case webrtc.PeerConnectionStateConnected:
			go logSelectedPair(peer, pc)
			h.writeIdle()
			for _, c := range vsender.GetParameters().Codecs { // what the client agreed to decode
				log.Printf("[%s] video codec: %s %s (pt %d)", peer, c.MimeType, c.SDPFmtpLine, c.PayloadType)
			}
			go func() {
				opts := h.capture
				if last := h.display.current(); last.W > 0 {
					opts.Width, opts.Height = last.W, last.H
					opts.Bitrate = bitrateFor(h.capture.Bitrate, last.W, last.H)
				}
				for {
					cctx, ccancel := context.WithCancel(sctx)
					done := make(chan struct{})
					go func(o captureOpts) { streamVideo(cctx, o, video, nil); close(done) }(opts)
					select {
					case <-sctx.Done():
						ccancel()
						<-done
						return
					case opts = <-restartCapture:
						ccancel()
						<-done
						log.Printf("[%s] capture restarting at %dx%d, %s", peer, opts.Width, opts.Height, opts.Bitrate)
					}
				}
			}()
			if audioTrack != nil {
				go streamAudio(sctx, h.audio, audioTrack)
			}
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateDisconnected:
			inj.ReleaseAll()
			closeSession()
		}
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: m.SDP}); err != nil {
		log.Printf("[%s] set remote: %v", peer, err)
		closeSession()
		return
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Printf("[%s] answer: %v", peer, err)
		closeSession()
		return
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		log.Printf("[%s] set local: %v", peer, err)
		closeSession()
		return
	}
	// every successful offer gets a fresh pairing token, so the PIN is typed once
	h.send(ctx, proto.Message{Type: "answer", To: peer, SDP: answer.SDP, Pair: h.pairs.mint()})
}

func logSelectedPair(peer string, pc *webrtc.PeerConnection) {
	time.Sleep(500 * time.Millisecond)
	for _, t := range pc.GetTransceivers() {
		if t.Sender() == nil || t.Sender().Transport() == nil {
			continue
		}
		p, err := t.Sender().Transport().ICETransport().GetSelectedCandidatePair()
		if err == nil && p != nil {
			log.Printf("[%s] path: local %s %s:%d  <->  remote %s %s:%d", peer,
				p.Local.Typ, p.Local.Address, p.Local.Port, p.Remote.Typ, p.Remote.Address, p.Remote.Port)
		}
		return
	}
}

func randomPIN() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return fmt.Sprintf("%06d", n.Int64())
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func hostname() string {
	n, _ := os.Hostname()
	if i := strings.Index(n, "."); i > 0 {
		n = n[:i]
	}
	return n
}

var _ = media.Sample{}

// injector creates the virtual keyboard and mouse once per host process and
// reuses them for every session: devices created per connection were never
// closed, and each new keyboard came up with the default layout.
func (h *host) injector() input.Injector {
	h.injOnce.Do(func() {
		inj, err := input.New(h.capture.Display, h.dryRun)
		if err != nil {
			log.Printf("input: %v (falling back to dry-run)", err)
			inj, _ = input.New(h.capture.Display, true)
		}
		h.inj = inj
		runInputHook()
	})
	return h.inj
}

// writeIdle records whether anyone is connected, and since when nobody is:
// the Mac's auto-stop (infra/droplet.sh autostop) reads it over ssh.
func (h *host) writeIdle() {
	if h.idleFile == "" {
		return
	}
	h.mu.Lock()
	n := 0
	for _, s := range h.sessions {
		if s.pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
			n++
		}
	}
	if n > 0 {
		h.idleSince = time.Time{}
	} else if h.idleSince.IsZero() {
		h.idleSince = time.Now()
	}
	line := fmt.Sprintf("busy %d\n", n)
	if n == 0 {
		line = fmt.Sprintf("idle-since %d\n", h.idleSince.Unix())
	}
	h.mu.Unlock()
	os.WriteFile(h.idleFile, []byte(line), 0o644)
}
