package main

import (
	"log"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

var layoutRe = regexp.MustCompile(`^[a-z]{2,3}(:[a-z0-9_-]{1,32})?$`)

// setKeyboard applies a client's keyboard layout (Linux: nc-keyboard, which
// records it on the desk and applies it to every keyboard now).
func setKeyboard(layout string) {
	if !layoutRe.MatchString(layout) {
		log.Printf("keyboard: ignoring bad layout %q", layout)
		return
	}
	if runtime.GOOS != "linux" {
		return // macOS/Windows hosts use their own configured layout
	}
	out, err := exec.Command("nc-keyboard", layout).CombinedOutput()
	if err != nil {
		log.Printf("keyboard: nc-keyboard %s: %v %s", layout, err, strings.TrimSpace(string(out)))
		return
	}
	log.Printf("keyboard: %s", strings.TrimSpace(string(out)))
}

// runInputHook lets provisioning adjust newly created input devices (the
// desk's keyboard layout). Optional: absent on hosts without it.
func runInputHook() {
	if runtime.GOOS != "linux" {
		return
	}
	p, err := exec.LookPath("nc-input-hook")
	if err != nil {
		return
	}
	if out, err := exec.Command(p).CombinedOutput(); err != nil {
		log.Printf("input hook: %v %s", err, strings.TrimSpace(string(out)))
	}
}
