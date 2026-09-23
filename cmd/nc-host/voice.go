package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/askrobots/network-computer/internal/proto"
)

// Voice relay between the client's 🎙️ button and nc-voice on the desk, over
// the host's local socket (the one nc-send uses):
//   - GET  /voice/commands: nc-voice's long-lived stream of {"cmd":"on"|"off"}
//     lines (and a "ping" every 30 s);
//   - POST /voice/event {"kind","text"}: what nc-voice heard, said or did,
//     passed to every connected client as {"t":"voice","kind","text"}.
type voiceRelay struct {
	mu  sync.Mutex
	sub chan string // nil while nc-voice is not connected
}

// command tells nc-voice to start or stop listening; false if it isn't running.
func (v *voiceRelay) command(on bool) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.sub == nil {
		return false
	}
	cmd := `{"cmd":"off"}`
	if on {
		cmd = `{"cmd":"on"}`
	}
	select {
	case v.sub <- cmd:
	default:
	}
	return true
}

func (h *host) handleVoiceCommands(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no streaming", http.StatusInternalServerError)
		return
	}
	ch := make(chan string, 8)
	h.voice.mu.Lock()
	h.voice.sub = ch
	h.voice.mu.Unlock()
	defer func() {
		h.voice.mu.Lock()
		if h.voice.sub == ch {
			h.voice.sub = nil
		}
		h.voice.mu.Unlock()
	}()
	log.Printf("voice: nc-voice connected")
	w.Header().Set("Content-Type", "application/x-ndjson")
	fmt.Fprintln(w, `{"cmd":"hello"}`)
	fl.Flush()
	for {
		select {
		case c := <-ch:
			fmt.Fprintln(w, c)
		case <-time.After(30 * time.Second):
			fmt.Fprintln(w, `{"cmd":"ping"}`)
		case <-r.Context().Done():
			log.Printf("voice: nc-voice disconnected")
			return
		}
		fl.Flush()
	}
}

func (h *host) handleVoiceEvent(w http.ResponseWriter, r *http.Request) {
	var ev struct{ Kind, Text string }
	if r.Method != http.MethodPost || json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&ev) != nil || ev.Kind == "" {
		http.Error(w, `POST {"kind":..., "text":...}`, http.StatusBadRequest)
		return
	}
	h.broadcastCtl(proto.InputEvent{T: "voice", Kind: ev.Kind, Text: ev.Text})
	w.WriteHeader(http.StatusNoContent)
}

// broadcastCtl sends a message on every connected client's control channel.
func (h *host) broadcastCtl(ev proto.InputEvent) {
	b, _ := json.Marshal(ev)
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.sessions {
		if s.ctl != nil {
			s.ctl.SendText(string(b))
		}
	}
}
