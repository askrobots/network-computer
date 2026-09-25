package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSitesProxy(t *testing.T) {
	var sawCookie, sawAuth string
	obj := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawCookie, sawAuth = r.Header.Get("Cookie"), r.Header.Get("Authorization")
		if r.URL.Path == "/s/site" {
			http.Redirect(w, r, "http://"+r.Host+"/s/site/index", http.StatusFound)
			return
		}
		w.Header().Set("Set-Cookie", "os=1")
		io.WriteString(w, "page "+r.URL.Path)
	}))
	defer obj.Close()
	h, err := sitesProxy(obj.URL)
	if err != nil {
		t.Fatal(err)
	}
	get := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Cookie", "nc_session=secret")
		r.Header.Set("Authorization", "Bearer x")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := get("GET", "/s/site/index")
	if w.Code != 200 || w.Body.String() != "page /s/site/index" {
		t.Fatalf("page: %d %q", w.Code, w.Body.String())
	}
	if sawCookie != "" || sawAuth != "" {
		t.Fatalf("credentials passed on: cookie %q auth %q", sawCookie, sawAuth)
	}
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatal("the object server's cookie reached the visitor")
	}
	if w := get("GET", "/s/site"); w.Header().Get("Location") != "/s/site/index" {
		t.Fatalf("redirect: %q", w.Header().Get("Location"))
	}
	if w := get("POST", "/s/site/index"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", w.Code)
	}
	if w := get("GET", "/api/records"); w.Code != http.StatusNotFound {
		t.Fatalf("outside /s/: %d", w.Code)
	}
}
