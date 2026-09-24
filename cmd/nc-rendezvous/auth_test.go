package main

import (
	"net/http/httptest"
	"testing"
)

func TestWrongPasswordsAreLimitedPerAddress(t *testing.T) {
	a := newAuthority("nc", "right")
	r := httptest.NewRequest("POST", "/auth", nil)
	r.RemoteAddr = "203.0.113.9:4000"
	for i := 0; i < maxFails; i++ {
		if ok, limited := a.checkCredsFrom(r, "nc", "wrong"); ok || limited {
			t.Fatalf("attempt %d: ok=%v limited=%v", i, ok, limited)
		}
	}
	if ok, limited := a.checkCredsFrom(r, "nc", "right"); ok || !limited {
		t.Fatalf("after %d failures the right password should be refused too: ok=%v limited=%v", maxFails, ok, limited)
	}
	other := httptest.NewRequest("POST", "/auth", nil)
	other.RemoteAddr = "198.51.100.7:5000"
	if ok, _ := a.checkCredsFrom(other, "nc", "right"); !ok {
		t.Fatal("another address must not be limited")
	}
	local := httptest.NewRequest("POST", "/auth", nil)
	local.RemoteAddr = "127.0.0.1:6000"
	for i := 0; i < maxFails+5; i++ {
		a.checkCredsFrom(local, "nc", "wrong")
	}
	if ok, limited := a.checkCredsFrom(local, "nc", "right"); !ok || limited {
		t.Fatal("the desk itself (loopback) is never limited")
	}
}
