package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
)

// talkbackPaths are all a phone may reach: what is new, the next event, and
// its audio. Adding speech or notices (POST) is for the desk itself only.
var talkbackPaths = regexp.MustCompile(`^/(health|api/voice/status|api/voice/next|audio/voice-[0-9]+-[0-9a-f]+\.wav)$`)

// talkbackProxy lets the SRT Stream app on a phone hear and see what the desk
// says back to a live stream (nc-voice's talkback, on loopback) at
// https://<desk>/talkback/..., over the desk's own certificate. Reading only;
// the phone's passphrase (Authorization: Bearer) goes through and is checked
// by the talkback service; the desk's login cookie never does.
func talkbackProxy(target string) (http.Handler, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	p := httputil.NewSingleHostReverseProxy(u)
	base := p.Director
	p.Director = func(r *http.Request) {
		base(r)
		r.Header.Del("Cookie")
		r.Host = u.Host
	}
	p.ModifyResponse = func(res *http.Response) error {
		res.Header.Del("Set-Cookie")
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "read only", http.StatusMethodNotAllowed)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/talkback")
		if !talkbackPaths.MatchString(rest) {
			http.NotFound(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path, r2.URL.RawPath = rest, ""
		p.ServeHTTP(w, r2)
	}), nil
}
