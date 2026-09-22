package tlspin

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPinning(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	sum := sha256.Sum256(srv.Certificate().Raw)
	good := hex.EncodeToString(sum[:])

	if _, err := Client(good).Get(srv.URL); err != nil {
		t.Fatalf("correct pin rejected: %v", err)
	}
	// uppercase with colons, as people copy it
	colons := ""
	for i := 0; i < len(good); i += 2 {
		if i > 0 {
			colons += ":"
		}
		colons += good[i : i+2]
	}
	if _, err := Client("SHA256 " + colons).Get(srv.URL); err != nil {
		t.Fatalf("normalised pin rejected: %v", err)
	}
	bad := "00" + good[2:]
	if _, err := Client(bad).Get(srv.URL); err == nil {
		t.Fatal("wrong pin accepted")
	}
	// unpinned client must still refuse a self-signed cert
	if _, err := Client("").Get(srv.URL); err == nil {
		t.Fatal("unpinned client accepted a self-signed cert")
	}
}
