package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// pairing lets a client type the PIN once. After a correct PIN the host hands
// back a token signed with a secret kept on disk; the client stores it and
// presents it instead of the PIN from then on. The secret persists so pairings
// survive restarts; rotating it (-reset-pairings) revokes every pairing.
type pairing struct {
	host   string
	secret []byte
}

const pairTTL = 90 * 24 * time.Hour

func loadPairing(host, dir string, reset bool) (*pairing, error) {
	path := filepath.Join(dir, "pair.key")
	if !reset {
		if b, err := os.ReadFile(path); err == nil {
			if sec, err := hex.DecodeString(strings.TrimSpace(string(b))); err == nil && len(sec) >= 32 {
				return &pairing{host: host, secret: sec}, nil
			}
		}
	}
	sec := make([]byte, 32)
	if _, err := rand.Read(sec); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(sec)+"\n"), 0o600); err != nil {
		return nil, err
	}
	return &pairing{host: host, secret: sec}, nil
}

func (p *pairing) sign(payload string) string {
	m := hmac.New(sha256.New, p.secret)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// mint issues a token bound to this host name, valid for pairTTL.
func (p *pairing) mint() string {
	payload := fmt.Sprintf("pair|%s|%d", p.host, time.Now().Add(pairTTL).Unix())
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + p.sign(payload)
}

func (p *pairing) valid(tok string) bool {
	dot := strings.LastIndex(tok, ".")
	if dot < 0 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok[:dot])
	if err != nil {
		return false
	}
	payload := string(raw)
	if subtle.ConstantTimeCompare([]byte(p.sign(payload)), []byte(tok[dot+1:])) != 1 {
		return false
	}
	f := strings.Split(payload, "|")
	if len(f) != 3 || f[0] != "pair" || f[1] != p.host {
		return false
	}
	exp, err := strconv.ParseInt(f[2], 10, 64)
	return err == nil && time.Now().Unix() <= exp
}

func defaultHostStateDir() string {
	if d, err := os.UserHomeDir(); err == nil && d != "" && d != "/" {
		return filepath.Join(d, ".local", "state", "nc-host")
	}
	return "/var/lib/nc-host"
}
