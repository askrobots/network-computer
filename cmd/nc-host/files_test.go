package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"report.pdf":          "report.pdf",
		"../../etc/passwd":    "passwd",
		`C:\Users\me\a b.txt`: "a b.txt",
		".bashrc":             "bashrc",
		"..":                  "",
		".":                   "",
		"":                    "",
		"/":                   "",
		"a\nb\x00c.txt":       "abc.txt",
		"  photo 1.jpg ":      "photo 1.jpg",
		"日本語.txt":             "日本語.txt",
	} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
	long := safeName(string(make([]rune, 0)) + repeat("é", 150) + ".txt")
	if len(long) > 200 || filepath.Ext(long) != ".txt" {
		t.Errorf("long name not trimmed well: %d bytes, %q", len(long), filepath.Ext(long))
	}
}

func repeat(s string, n int) (r string) {
	for i := 0; i < n; i++ {
		r += s
	}
	return
}

func TestUniquePath(t *testing.T) {
	dir := t.TempDir()
	if got := uniquePath(dir, "a.txt"); got != filepath.Join(dir, "a.txt") {
		t.Fatal(got)
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "a (2).txt"), nil, 0o644)
	if got := uniquePath(dir, "a.txt"); got != filepath.Join(dir, "a (3).txt") {
		t.Fatal(got)
	}
}
