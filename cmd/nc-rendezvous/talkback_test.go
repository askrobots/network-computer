package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTalkbackProxy(t *testing.T) {
	var sawPath, sawCookie, sawAuth string
	tb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath, sawCookie, sawAuth = r.URL.Path, r.Header.Get("Cookie"), r.Header.Get("Authorization")
		io.WriteString(w, "ok")
	}))
	defer tb.Close()
	h, err := talkbackProxy(tb.URL)
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, path string) int {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Cookie", "nc_session=secret")
		r.Header.Set("Authorization", "Bearer pass")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if c := do("GET", "/talkback/api/voice/next?after=5"); c != 200 || sawPath != "/api/voice/next" {
		t.Fatalf("next: %d %q", c, sawPath)
	}
	if sawCookie != "" || sawAuth != "Bearer pass" {
		t.Fatalf("the cookie must stay, the passphrase must go on: cookie %q auth %q", sawCookie, sawAuth)
	}
	if c := do("GET", "/talkback/audio/voice-1790-abc123.wav"); c != 200 {
		t.Fatalf("audio: %d", c)
	}
	for _, bad := range []string{"/talkback/api/voice", "/talkback/audio/../etc/passwd", "/talkback/admin", "/talkback/audio/x.wav"} {
		if c := do("GET", bad); c != 404 {
			t.Fatalf("%s: %d, want 404", bad, c)
		}
	}
	if c := do("POST", "/talkback/api/voice"); c != 405 {
		t.Fatalf("POST from outside: %d, want 405", c)
	}
}
