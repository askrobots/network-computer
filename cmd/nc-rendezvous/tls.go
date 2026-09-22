package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// selfSignedCert loads, or on first run creates, a self-signed certificate in
// dir. It is persisted so its fingerprint stays the same across restarts:
// clients pin that fingerprint instead of trusting a certificate authority,
// which gives real TLS without owning a domain.
func selfSignedCert(dir string, names []string) (certFile, keyFile, fingerprint string, err error) {
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if _, statErr := os.Stat(certFile); statErr != nil {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			return
		}
		if err = writeSelfSigned(certFile, keyFile, names); err != nil {
			return
		}
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return
	}
	sum := sha256.Sum256(pair.Certificate[0])
	return certFile, keyFile, hex.EncodeToString(sum[:]), nil
}

func writeSelfSigned(certFile, keyFile string, names []string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "nc-rendezvous"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return err
	}
	return os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
}

// defaultStateDir is where the rendezvous keeps its cert and ACME cache.
// Under systemd running as root there may be no $HOME, hence the fallback.
func defaultStateDir() string {
	if d, err := os.UserHomeDir(); err == nil && d != "" && d != "/" {
		return filepath.Join(d, ".local", "state", "nc-rendezvous")
	}
	return "/var/lib/nc-rendezvous"
}
