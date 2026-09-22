// Package tlspin makes HTTP clients that trust exactly one certificate,
// identified by the SHA-256 of its DER encoding. Used with a rendezvous that
// runs a self-signed cert: no certificate authority and no domain, and still
// protected against a man in the middle, because only that one key is accepted.
package tlspin

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Normalize accepts "AB:CD:...", "abcd...", or "sha256 abcd..." and returns
// lowercase hex with no separators.
func Normalize(fp string) string {
	fp = strings.ToLower(strings.TrimSpace(fp))
	fp = strings.TrimPrefix(fp, "sha256")
	fp = strings.TrimLeft(fp, ":/= ")
	return strings.NewReplacer(":", "", " ", "").Replace(fp)
}

// Client returns an http.Client pinned to fp. An empty fp returns a normal
// client that verifies certificates against the system roots.
func Client(fp string) *http.Client {
	fp = Normalize(fp)
	if fp == "" {
		return http.DefaultClient
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		// Chain verification is replaced by the exact-key check below.
		InsecureSkipVerify: true,
		// VerifyConnection runs on every connection, resumed ones included.
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("tlspin: server sent no certificate")
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if got := hex.EncodeToString(sum[:]); got != fp {
				return fmt.Errorf("tlspin: certificate fingerprint mismatch (got %s)", got)
			}
			return nil
		},
	}
	return &http.Client{Transport: t}
}
