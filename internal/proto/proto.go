// Package proto defines the signaling messages exchanged through nc-rendezvous
// and the input events carried on the WebRTC data channel.
package proto

import "encoding/json"

// Message is the envelope for everything that crosses the signaling WebSocket.
// The rendezvous only looks at Type, To and Name; it fills in From.
type Message struct {
	Type string `json:"type"`           // register, hosts, offer, answer, ice, error
	Name string `json:"name,omitempty"` // register: host name
	To   string `json:"to,omitempty"`   // routing target: host name or client id
	From string `json:"from,omitempty"` // filled by the rendezvous

	SDP       string          `json:"sdp,omitempty"`       // offer / answer
	PIN       string          `json:"pin,omitempty"`       // offer: the host PIN the client was given
	Candidate json.RawMessage `json:"candidate,omitempty"` // ice: RTCIceCandidateInit as JSON
	Hosts     []string        `json:"hosts,omitempty"`     // hosts reply
	Error     string          `json:"error,omitempty"`
}

// ICEServer mirrors the browser's RTCIceServer shape so /config can be fed
// straight into a RTCPeerConnection and into Pion.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// Config is what GET /config returns. Mode is "secure" (signaling over TLS)
// or "insecure" (plain HTTP) so clients can show which posture they are in.
type Config struct {
	ICEServers []ICEServer `json:"iceServers"`
	Mode       string      `json:"mode,omitempty"`
}

// InputEvent is one message on the "input" data channel. JSON for the spike;
// a packed binary form comes later. Coordinates are normalised 0..1 across the
// captured display so the client never needs to know the host resolution.
type InputEvent struct {
	T    string  `json:"t"`              // mm, mr, md, mu, wh, kd, ku
	X    float64 `json:"x,omitempty"`    // mm: absolute normalised position
	Y    float64 `json:"y,omitempty"`    //
	DX   float64 `json:"dx,omitempty"`   // mr: relative move in pixels; wh: scroll delta in pixels
	DY   float64 `json:"dy,omitempty"`   //
	B    int     `json:"b,omitempty"`    // md/mu: 0 left, 1 middle, 2 right
	Code string  `json:"code,omitempty"` // kd/ku: KeyboardEvent.code, e.g. "KeyA"
}
