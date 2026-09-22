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
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

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
	sendInput := flag.Bool("input", false, "send a few test input events over the data channel")
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
	pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	dc, _ := pc.CreateDataChannel("input", nil)

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
		first                             time.Time
	}
	st := &stats{}
	start := time.Now()
	done := make(chan struct{})
	pc.OnTrack(func(track *webrtc.TrackRemote, r *webrtc.RTPReceiver) {
		log.Printf("track: %s %s", track.Kind(), track.Codec().MimeType)
		if track.Kind() != webrtc.RTPCodecTypeVideo {
			go func() {
				for {
					pkt, _, err := track.ReadRTP()
					if err != nil {
						return
					}
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
	send(proto.Message{Type: "offer", To: *hostName, SDP: offer.SDP, PIN: *pin})
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
