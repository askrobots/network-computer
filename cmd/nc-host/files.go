package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/pion/webrtc/v4"
)

// File transfer, both ways. Each file travels on its own reliable data
// channel labelled "file": a JSON header {"name","size"}, binary pieces, then
// the text "end". The receiver answers "ok <name>" or "error: <why>".
//   - client to desk: saved into -files-dir (the desk user's Desktop) under a
//     temporary name until complete, owned by that directory's owner.
//   - desk to client: nc-send (the file manager's "Send to my device") PUTs
//     the file into the -send-socket unix socket; the host passes it to every
//     connected client, which offers it as a download.
const (
	filePiece     = 16 << 10
	fileHighWater = 4 << 20 // bytes queued on a channel before waiting
	fileReserve   = 50 << 20
)

type fileHeader struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// receiveFile saves one file sent by the client.
func receiveFile(peer string, dc *webrtc.DataChannel, dir string) {
	var (
		mu         sync.Mutex
		hdr        fileHeader
		f          *os.File
		tmp, final string
		got        int64
		done       bool
	)
	finish := func(reply string) { // with mu held
		if f != nil {
			f.Close()
			os.Remove(tmp)
			f = nil
		}
		done = true
		dc.SendText(reply)
		if strings.HasPrefix(reply, "error") {
			log.Printf("[%s] file %q: %s", peer, hdr.Name, reply)
		}
	}
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case done:
		case f == nil && m.IsString:
			if err := json.Unmarshal(m.Data, &hdr); err != nil || hdr.Size < 0 {
				finish("error: bad header")
				return
			}
			name := safeName(hdr.Name)
			switch {
			case dir == "":
				finish("error: this host does not accept files")
				return
			case name == "":
				finish("error: bad file name")
				return
			}
			if free, err := freeBytes(dir); err == nil && hdr.Size > free-fileReserve {
				finish(fmt.Sprintf("error: not enough space on the desk (%s free)", humanBytes(free)))
				return
			}
			final = uniquePath(dir, name)
			tmp = filepath.Join(dir, "."+filepath.Base(final)+".part")
			var err error
			if f, err = os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644); err != nil {
				f = nil
				finish("error: " + err.Error())
			}
		case f != nil && !m.IsString:
			got += int64(len(m.Data))
			if got > hdr.Size {
				finish("error: more data than announced")
				return
			}
			if _, err := f.Write(m.Data); err != nil {
				finish("error: " + err.Error())
			}
		case f != nil && string(m.Data) == "end":
			if got != hdr.Size {
				finish(fmt.Sprintf("error: got %d of %d bytes", got, hdr.Size))
				return
			}
			err := f.Close()
			f = nil
			if err == nil {
				chownLike(tmp, dir)
				final = uniquePath(dir, filepath.Base(final)) // in case the name was taken meanwhile
				err = os.Rename(tmp, final)
			}
			if err != nil {
				os.Remove(tmp)
				finish("error: " + err.Error())
				return
			}
			log.Printf("[%s] file saved: %s (%s)", peer, final, humanBytes(got))
			finish("ok " + filepath.Base(final))
		}
	})
	dc.OnClose(func() {
		mu.Lock()
		defer mu.Unlock()
		if f != nil { // the client went away mid-file
			f.Close()
			os.Remove(tmp)
			f = nil
		}
	})
}

// sendFile streams a file to one client and waits for its answer.
func sendFile(ctx context.Context, pc *webrtc.PeerConnection, name string, r io.Reader, size int64) error {
	dc, err := pc.CreateDataChannel("file", nil)
	if err != nil {
		return err
	}
	defer dc.Close()
	opened, low := make(chan struct{}), make(chan struct{}, 1)
	reply := make(chan string, 1)
	dc.OnOpen(func() { close(opened) })
	dc.SetBufferedAmountLowThreshold(fileHighWater / 4)
	dc.OnBufferedAmountLow(func() {
		select {
		case low <- struct{}{}:
		default:
		}
	})
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		select {
		case reply <- string(m.Data):
		default:
		}
	})
	select {
	case <-opened:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Second):
		return errors.New("the device did not open a channel")
	}
	b, _ := json.Marshal(fileHeader{Name: name, Size: size})
	if err := dc.SendText(string(b)); err != nil {
		return err
	}
	buf := make([]byte, filePiece)
	for {
		for dc.BufferedAmount() > fileHighWater {
			select {
			case <-low:
			case <-time.After(time.Second):
			case <-ctx.Done():
				return ctx.Err()
			case r := <-reply: // refused before the end
				return errors.New(r)
			}
		}
		n, err := r.Read(buf)
		if n > 0 {
			if err := dc.Send(buf[:n]); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if err := dc.SendText("end"); err != nil {
		return err
	}
	select {
	case r := <-reply:
		if !strings.HasPrefix(r, "ok") {
			return errors.New(r)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Minute): // a client without file support never answers
		return errors.New("the device did not confirm")
	}
}

// serveSend accepts files from nc-send on a unix socket and passes them to
// the connected clients.
func (h *host) serveSend(path string) {
	os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		log.Printf("send socket: %v", err)
		return
	}
	os.Chmod(path, 0o666) // the desk user runs nc-send; it streams the file itself, so it can only send what it can read
	log.Printf("send socket: %s", path)
	mux := http.NewServeMux()
	mux.HandleFunc("/voice/commands", h.handleVoiceCommands)
	mux.HandleFunc("/voice/event", h.handleVoiceEvent)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := safeName(r.URL.Query().Get("name"))
		if r.Method != http.MethodPut || name == "" {
			http.Error(w, "PUT /send?name=FILE with the file as the body", http.StatusBadRequest)
			return
		}
		tmp, err := os.CreateTemp("", "nc-send-*")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer os.Remove(tmp.Name())
		defer tmp.Close()
		size, err := io.Copy(tmp, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var pcs []*webrtc.PeerConnection
		h.mu.Lock()
		for _, s := range h.sessions {
			if s.pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
				pcs = append(pcs, s.pc)
			}
		}
		h.mu.Unlock()
		if len(pcs) == 0 {
			http.Error(w, "no device is connected", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()
		var wg sync.WaitGroup
		var ok atomic.Int32
		for _, pc := range pcs { // in parallel: one slow or old client must not hold up the rest
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := sendFile(ctx, pc, name, io.NewSectionReader(tmp, 0, size), size); err != nil {
					log.Printf("send %s: %v", name, err)
					return
				}
				ok.Add(1)
			}()
		}
		wg.Wait()
		sent := int(ok.Load())
		if sent == 0 {
			http.Error(w, "the device did not take it", http.StatusBadGateway)
			return
		}
		log.Printf("sent %s (%s) to %d device(s)", name, humanBytes(size), sent)
		fmt.Fprintf(w, "sent %s to %d device(s)\n", name, sent)
	})
	http.Serve(l, mux)
}

// safeName keeps only the last element of a client-supplied name and drops
// anything that could escape the directory or hide the file.
func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = name[:200-len(ext)] + ext
		for !utf8ValidEnd(name) {
			name = name[:len(name)-1]
		}
	}
	return name
}

func utf8ValidEnd(s string) bool { return strings.ToValidUTF8(s, "\x00") == s }

// uniquePath returns dir/name, or dir/"name (2).ext" and so on if taken.
func uniquePath(dir, name string) string {
	p := filepath.Join(dir, name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
