package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// sitesProxy serves the object server's published sites (/s/<name>/...) on
// the desk's public address, so a site made in Webmaster is at
// https://<desk>/s/<name>. Only /s/, only reading (GET, HEAD); the desk's
// login cookie and any credentials are removed first, so a site published as
// not public stays private, and nothing else of the object server is reached.
// The object server sends the pages sandboxed (CSP), which passes through.
func sitesProxy(target string) (http.Handler, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	p := httputil.NewSingleHostReverseProxy(u)
	base := p.Director
	p.Director = func(r *http.Request) {
		base(r)
		r.Header.Del("Cookie")
		r.Header.Del("Authorization")
		r.Header.Del("X-Forwarded-For") // the object server is not told to trust anyone
		r.Host = u.Host
	}
	p.ModifyResponse = func(res *http.Response) error {
		// a redirect to the object server's own address becomes a path here
		if loc := res.Header.Get("Location"); strings.HasPrefix(loc, target) {
			res.Header.Set("Location", strings.TrimPrefix(loc, strings.TrimRight(target, "/")))
		}
		res.Header.Del("Set-Cookie")
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "read only", http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/s/") || strings.Contains(r.URL.Path, "..") {
			http.NotFound(w, r)
			return
		}
		p.ServeHTTP(w, r)
	}), nil
}
