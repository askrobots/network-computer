// nc-rendezvous: the one public piece. Signaling over WebSocket, STUN and TURN
// on one UDP port, and the browser test client. Single static binary.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/turn/v4"
	"golang.org/x/crypto/acme/autocert"

	"github.com/askrobots/network-computer/internal/proto"
)

//go:embed web
var webFS embed.FS

type client struct {
	id   string
	name string // set when registered as a host
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *client) send(ctx context.Context, m proto.Message) error {
	b, _ := json.Marshal(m)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, b)
}

type hub struct {
	mu      sync.RWMutex
	clients map[string]*client // by id
	hosts   map[string]*client // by name
	seq     atomic.Int64
}

func newHub() *hub {
	return &hub{clients: map[string]*client{}, hosts: map[string]*client{}}
}

func (h *hub) add(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c.id] = c
}

func (h *hub) remove(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c.id)
	if c.name != "" && h.hosts[c.name] == c {
		delete(h.hosts, c.name)
	}
}

func (h *hub) register(c *client, name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c.name = name
	h.hosts[name] = c
}

func (h *hub) lookup(to string) *client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if c, ok := h.hosts[to]; ok {
		return c
	}
	return h.clients[to]
}

func (h *hub) hostNames() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.hosts))
	for n := range h.hosts {
		out = append(out, n)
	}
	return out
}

func (h *hub) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"}, // spike: any origin
	})
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}
	c := &client{id: fmt.Sprintf("c%d", h.seq.Add(1)), conn: conn}
	h.add(c)
	defer func() {
		h.remove(c)
		conn.Close(websocket.StatusNormalClosure, "bye")
		log.Printf("[%s] disconnected (host=%q)", c.id, c.name)
	}()
	log.Printf("[%s] connected from %s", c.id, r.RemoteAddr)

	ctx := r.Context()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var m proto.Message
		if err := json.Unmarshal(data, &m); err != nil {
			c.send(ctx, proto.Message{Type: "error", Error: "bad json"})
			continue
		}
		m.From = c.id
		switch m.Type {
		case "register":
			if m.Name == "" {
				c.send(ctx, proto.Message{Type: "error", Error: "register needs name"})
				continue
			}
			h.register(c, m.Name)
			log.Printf("[%s] registered host %q", c.id, m.Name)
			c.send(ctx, proto.Message{Type: "registered", Name: m.Name})
		case "hosts":
			c.send(ctx, proto.Message{Type: "hosts", Hosts: h.hostNames()})
		case "offer", "answer", "ice", "error":
			if m.Type == "error" && m.To == "" {
				continue
			}
			dst := h.lookup(m.To)
			if dst == nil {
				c.send(ctx, proto.Message{Type: "error", Error: "no such peer: " + m.To})
				continue
			}
			if m.Type != "ice" {
				log.Printf("[%s] %s -> %s", c.id, m.Type, m.To)
			}
			if err := dst.send(ctx, m); err != nil {
				c.send(ctx, proto.Message{Type: "error", Error: "peer gone: " + m.To})
			}
		default:
			c.send(ctx, proto.Message{Type: "error", Error: "unknown type " + m.Type})
		}
	}
}

// startTURN runs STUN and TURN on one UDP socket using pion/turn. Relay access
// uses short-lived TURN REST credentials (username "<expiry>:<id>", password an
// HMAC of it under a secret only this process knows), minted per /config
// request. A leaked credential stops working when it expires.
func startTURN(addr, publicIP, realm, secret string) (*turn.Server, error) {
	pc, err := net.ListenPacket("udp4", addr)
	if err != nil {
		return nil, err
	}
	return turn.NewServer(turn.ServerConfig{
		Realm:       realm,
		AuthHandler: turn.LongTermTURNRESTAuthHandler(secret, nil),
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: pc,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP(publicIP),
				Address:      "0.0.0.0",
			},
		}},
	})
}

func main() {
	httpAddr := flag.String("http", ":8080", "listen address (signaling + web client)")
	turnAddr := flag.String("turn", ":3478", "STUN/TURN UDP listen address ('' to disable)")
	publicIP := flag.String("public-ip", "", "public IP of this machine, advertised for TURN relay and in /config")
	publicHost := flag.String("public-host", "", "public hostname for /config ice urls (defaults to -public-ip)")
	realm := flag.String("realm", "nc", "TURN realm")
	turnSecret := flag.String("turn-secret", os.Getenv("NC_TURN_SECRET"), "secret for minting per-session TURN credentials (env NC_TURN_SECRET); random if empty")
	mode := flag.String("mode", "auto", "auto | secure | insecure. secure forces TLS (self-signed if no cert is given); insecure forces plain HTTP")
	tlsSelf := flag.Bool("tls-self", false, "serve TLS with a persisted self-signed cert; clients pin its fingerprint")
	certFile := flag.String("tls-cert", "", "TLS cert file (optional)")
	keyFile := flag.String("tls-key", "", "TLS key file (optional)")
	stateDir := flag.String("state-dir", defaultStateDir(), "where the self-signed cert and ACME cache live")
	authUser := flag.String("user", envOr("NC_USER", "nc"), "username (env NC_USER)")
	authPass := flag.String("password", os.Getenv("NC_PASSWORD"), "password (env NC_PASSWORD); generated and printed if empty")
	acmeDomain := flag.String("acme-domain", os.Getenv("NC_DOMAIN"), "get a Let's Encrypt cert for this domain, listening on :443 and :80 (env NC_DOMAIN)")
	localAddr := flag.String("local", "", "with TLS on, also serve plain HTTP on this loopback address (e.g. 127.0.0.1:8765) for an nc-host on the same machine")
	flag.Parse()

	if *authPass == "" {
		*authPass = randomToken(12)
		log.Printf("no -password given, generated one: %s   (pass it to nc-host and nc-probe as -password, or set NC_PASSWORD)", *authPass)
	}
	if *turnSecret == "" {
		*turnSecret = randomToken(24)
	}

	switch *mode {
	case "auto":
	case "secure":
		if *acmeDomain == "" && *certFile == "" {
			*tlsSelf = true
		}
	case "insecure":
		if *acmeDomain != "" || *certFile != "" || *tlsSelf {
			log.Fatal("-mode insecure conflicts with -acme-domain / -tls-cert / -tls-self")
		}
	default:
		log.Fatalf("-mode must be auto, secure or insecure, not %q", *mode)
	}

	if *publicIP == "" {
		*publicIP = guessLocalIP()
		log.Printf("no -public-ip given, guessing %s (fine on a LAN, wrong on a VPS behind NAT)", *publicIP)
	}
	if *publicHost == "" {
		*publicHost = *publicIP
	}
	_, turnPort, _ := net.SplitHostPort(*turnAddr)

	fingerprint := ""
	if *tlsSelf {
		var err error
		*certFile, *keyFile, fingerprint, err = selfSignedCert(filepath.Join(*stateDir, "tls"),
			[]string{*publicHost, *publicIP, guessLocalIP(), "127.0.0.1", "localhost"})
		if err != nil {
			log.Fatalf("self-signed cert: %v", err)
		}
	}
	secure := *acmeDomain != "" || *certFile != ""

	h := newHub()

	if *turnAddr != "" {
		srv, err := startTURN(*turnAddr, *publicIP, *realm, *turnSecret)
		if err != nil {
			log.Fatalf("turn: %v", err)
		}
		defer srv.Close()
		log.Printf("STUN/TURN on udp %s, relay address %s, per-session credentials", *turnAddr, *publicIP)
	}

	auth := newAuthority(*authUser, *authPass)
	postureName := "insecure"
	if secure {
		postureName = "secure"
	}

	// configFor builds /config for one caller, with fresh relay credentials.
	configFor := func() proto.Config {
		cfg := proto.Config{Mode: postureName}
		if *turnAddr == "" {
			return cfg
		}
		hp := net.JoinHostPort(*publicHost, turnPort)
		user, pass, err := turn.GenerateLongTermTURNRESTCredentials(*turnSecret, randomToken(8), tokenTTL)
		if err != nil {
			log.Printf("turn creds: %v", err)
			return cfg
		}
		cfg.ICEServers = []proto.ICEServer{
			{URLs: []string{"stun:" + hp}},
			{URLs: []string{"turn:" + hp + "?transport=udp"}, Username: user, Credential: pass},
		}
		return cfg
	}

	sub, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()

	// POST /auth {user,password} -> {token}. The only place credentials go.
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "content-type,authorization")
		if r.Method == http.MethodOptions {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var body struct{ User, Password string }
		json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body)
		if !auth.checkCreds(body.User, body.Password) {
			http.Error(w, `{"error":"bad credentials"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token":     auth.mint(),
			"expiresIn": int(tokenTTL.Seconds()),
			"mode":      postureName,
		})
	})

	mux.Handle("/ws", auth.guard(http.HandlerFunc(h.serveWS)))
	mux.Handle("/config", auth.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-store") // relay creds are per request
		json.NewEncoder(w).Encode(configFor())
	})))
	mux.Handle("/hosts", auth.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(h.hostNames())
	})))
	// the page itself is public: no browser login dialog, the app's form asks once
	mux.Handle("/", http.FileServer(http.FS(sub)))

	srv := &http.Server{Addr: *httpAddr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	// A plain listener for processes on this machine (the co-located nc-host)
	// when the public side is TLS. Loopback only: it never leaves the box.
	startLocal := func() {
		if *localAddr == "" {
			return
		}
		host, _, err := net.SplitHostPort(*localAddr)
		if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
			log.Fatalf("-local must be a loopback address like 127.0.0.1:8765, not %q", *localAddr)
		}
		log.Printf("local plain HTTP on %s (loopback only)", *localAddr)
		go func() {
			ls := &http.Server{Addr: *localAddr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
			log.Fatal(ls.ListenAndServe())
		}()
	}

	switch {
	case *acmeDomain != "":
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(*acmeDomain),
			Cache:      autocert.DirCache(filepath.Join(*stateDir, "acme")),
		}
		srv.Addr = ":443"
		// m.TLSConfig advertises acme-tls/1, which the tls-alpn-01 challenge
		// needs; a hand-built config without it fails certificate issuance.
		srv.TLSConfig = m.TLSConfig()
		srv.TLSConfig.MinVersion = tls.VersionTLS12
		go http.ListenAndServe(":80", m.HTTPHandler(nil))
		log.Printf("secure mode: HTTPS on :443 for %s (ACME)", *acmeDomain)
		startLocal()
		log.Fatal(srv.ListenAndServeTLS("", ""))
	case *certFile != "":
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		startLocal()
		log.Printf("secure mode: HTTPS on %s  (web client: https://%s%s/)", *httpAddr, *publicHost, portSuffix(*httpAddr))
		if fingerprint != "" {
			log.Printf("self-signed cert. Pin this fingerprint in clients (-tls-fingerprint / NC_TLS_FP):")
			log.Printf("  sha256 %s", fingerprint)
			log.Printf("browsers will ask once to accept the certificate")
		}
		log.Fatal(srv.ListenAndServeTLS(*certFile, *keyFile))
	default:
		log.Printf("insecure mode: plain HTTP on %s  (web client: http://%s%s/). Signaling is unencrypted; use -mode secure off a trusted LAN.",
			*httpAddr, *publicHost, portSuffix(*httpAddr))
		log.Fatal(srv.ListenAndServe())
	}
}

// authority authenticates callers. It deliberately never sends a
// WWW-Authenticate header, so a browser shows no native login dialog: the
// client's own form is the single place credentials are entered. Three ways in:
// a Bearer token from POST /auth, HTTP Basic (for headless nc-host/nc-probe),
// or ?token= on the WebSocket, which browsers cannot give headers to.
type authority struct {
	user, pass string
	secret     []byte
}

const tokenTTL = 12 * time.Hour

func newAuthority(user, pass string) *authority {
	secret := make([]byte, 32)
	rand.Read(secret)
	return &authority{user: user, pass: pass, secret: secret}
}

func (a *authority) checkCreds(u, p string) bool {
	return subtle.ConstantTimeCompare([]byte(u), []byte(a.user)) == 1 &&
		subtle.ConstantTimeCompare([]byte(p), []byte(a.pass)) == 1
}

func (a *authority) sign(payload []byte) string {
	m := hmac.New(sha256.New, a.secret)
	m.Write(payload)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (a *authority) mint() string {
	payload := []byte(fmt.Sprintf("%s|%d", a.user, time.Now().Add(tokenTTL).Unix()))
	return base64.RawURLEncoding.EncodeToString(payload) + "." + a.sign(payload)
}

func (a *authority) validToken(tok string) bool {
	dot := strings.LastIndex(tok, ".")
	if dot < 0 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(tok[:dot])
	if err != nil {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(a.sign(payload)), []byte(tok[dot+1:])) != 1 {
		return false
	}
	i := strings.LastIndex(string(payload), "|")
	if i < 0 {
		return false
	}
	exp, err := strconv.ParseInt(string(payload)[i+1:], 10, 64)
	return err == nil && time.Now().Unix() <= exp
}

func (a *authority) ok(r *http.Request) bool {
	if h := r.Header.Get("Authorization"); h != "" {
		if strings.HasPrefix(h, "Bearer ") {
			return a.validToken(strings.TrimPrefix(h, "Bearer "))
		}
		if u, p, isBasic := r.BasicAuth(); isBasic {
			return a.checkCreds(u, p)
		}
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return a.validToken(t)
	}
	return false
}

// guard protects a handler. 401 with no WWW-Authenticate: no browser dialog.
func (a *authority) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.ok(r) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func randomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func portSuffix(addr string) string {
	_, p, _ := net.SplitHostPort(addr)
	if p == "80" || p == "" {
		return ""
	}
	return ":" + p
}

func guessLocalIP() string {
	conn, err := net.Dial("udp", "10.255.255.255:1")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	ip := conn.LocalAddr().(*net.UDPAddr).IP.String()
	if strings.Contains(ip, ":") {
		return "127.0.0.1"
	}
	return ip
}
