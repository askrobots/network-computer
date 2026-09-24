package main

import (
	"log"
	"os"
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

var tzRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$`)

// setTimezone makes the desk's clock the client's: the taskbar clock, the
// dashboard, and what voice takes "at 3" to mean. Linux desks only.
func setTimezone(tz string) {
	if runtime.GOOS != "linux" || !tzRe.MatchString(tz) || strings.Contains(tz, "..") {
		return
	}
	if _, err := os.Stat("/usr/share/zoneinfo/" + tz); err != nil {
		log.Printf("timezone: unknown %q", tz)
		return
	}
	if cur, _ := os.Readlink("/etc/localtime"); strings.HasSuffix(cur, "/"+tz) {
		return
	}
	if out, err := exec.Command("timedatectl", "set-timezone", tz).CombinedOutput(); err != nil {
		log.Printf("timezone: %v %s", err, strings.TrimSpace(string(out)))
		return
	}
	log.Printf("timezone: %s (from the client)", tz)
	// the taskbar clock reads the zone when it starts
	exec.Command("nc-as-user", "xfce4-panel", "-r").Start()
}

// launch opens (or closes) the desk's search and launch bar, for the client's
// 🔍 button. Linux desks only: nc-launch comes with provisioning.
func launch() {
	if runtime.GOOS != "linux" {
		return
	}
	if out, err := exec.Command("nc-launch").CombinedOutput(); err != nil {
		log.Printf("launch: %v %s", err, strings.TrimSpace(string(out)))
	}
}
