//go:build darwin

package input

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreGraphics
#include <ApplicationServices/ApplicationServices.h>

static int displayBounds(int idx, double *x, double *y, double *w, double *h) {
	CGDirectDisplayID ids[16];
	uint32_t n = 0;
	if (CGGetActiveDisplayList(16, ids, &n) != kCGErrorSuccess || idx < 0 || (uint32_t)idx >= n) return -1;
	CGRect r = CGDisplayBounds(ids[idx]);
	*x = r.origin.x; *y = r.origin.y; *w = r.size.width; *h = r.size.height;
	return 0;
}

static void postMouse(int type, double x, double y, int button, uint64_t flags) {
	CGEventRef e = CGEventCreateMouseEvent(NULL, (CGEventType)type, CGPointMake(x, y), (CGMouseButton)button);
	CGEventSetFlags(e, (CGEventFlags)flags);
	CGEventPost(kCGHIDEventTap, e);
	CFRelease(e);
}

static void postScroll(double dx, double dy, uint64_t flags) {
	CGEventRef e = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitPixel, 2, (int32_t)dy, (int32_t)dx);
	CGEventSetFlags(e, (CGEventFlags)flags);
	CGEventPost(kCGHIDEventTap, e);
	CFRelease(e);
}

static void postKey(int keycode, int down, uint64_t flags) {
	CGEventRef e = CGEventCreateKeyboardEvent(NULL, (CGKeyCode)keycode, down ? true : false);
	CGEventSetFlags(e, (CGEventFlags)flags);
	CGEventPost(kCGHIDEventTap, e);
	CFRelease(e);
}
*/
import "C"

import (
	"fmt"
	"log"
	"sync"

	"github.com/dbbasic/network-computer/internal/proto"
)

const (
	evMouseMove           = 5
	evLeftDown            = 1
	evLeftUp              = 2
	evRightDown           = 3
	evRightUp             = 4
	evLeftDragged         = 6
	evRightDragged        = 7
	evOtherDown           = 25
	evOtherUp             = 26
	evOtherDragged        = 27
	flagShift      uint64 = 1 << 17
	flagCtrl       uint64 = 1 << 18
	flagAlt        uint64 = 1 << 19
	flagCmd        uint64 = 1 << 20
)

type mac struct {
	mu       sync.Mutex
	ox, oy   float64 // display origin in global points
	w, h     float64
	x, y     float64
	buttons  [3]bool
	mods     uint64
	heldKeys map[int]bool
	display  int
}

func newPlatform(display int) (Injector, error) {
	// avfoundation screen index == CG active display index in practice
	// (both enumerate in the same order); revisit if it is not.
	var x, y, w, h C.double
	if C.displayBounds(C.int(display), &x, &y, &w, &h) != 0 {
		return nil, fmt.Errorf("display %d not found", display)
	}
	m := &mac{ox: float64(x), oy: float64(y), w: float64(w), h: float64(h), heldKeys: map[int]bool{}, display: display}
	log.Printf("input: display %d bounds %vx%v at (%v,%v). Needs Accessibility permission for this process.", display, w, h, x, y)
	return m, nil
}

func (m *mac) Handle(e proto.InputEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch e.T {
	case "mm":
		m.x = m.ox + e.X*m.w
		m.y = m.oy + e.Y*m.h
		typ := evMouseMove
		switch {
		case m.buttons[0]:
			typ = evLeftDragged
		case m.buttons[2]:
			typ = evRightDragged
		case m.buttons[1]:
			typ = evOtherDragged
		}
		C.postMouse(C.int(typ), C.double(m.x), C.double(m.y), C.int(m.cgButton()), C.uint64_t(m.mods))
	case "md", "mu":
		down := e.T == "md"
		b := e.B
		if b < 0 || b > 2 {
			return
		}
		m.buttons[b] = down
		var typ, btn int
		switch b {
		case 0:
			typ, btn = evLeftDown, 0
		case 2:
			typ, btn = evRightDown, 1
		case 1:
			typ, btn = evOtherDown, 2
		}
		if !down {
			typ++ // Up is Down+1 for all three
		}
		C.postMouse(C.int(typ), C.double(m.x), C.double(m.y), C.int(btn), C.uint64_t(m.mods))
	case "wh":
		C.postScroll(C.double(-e.DX), C.double(-e.DY), C.uint64_t(m.mods))
	case "kd", "ku":
		kc, ok := keyCodes[e.Code]
		if !ok {
			log.Printf("input: unmapped key %q", e.Code)
			return
		}
		down := e.T == "kd"
		if f, isMod := modFlags[e.Code]; isMod {
			if down {
				m.mods |= f
			} else {
				m.mods &^= f
			}
		}
		if down {
			m.heldKeys[kc] = true
		} else {
			delete(m.heldKeys, kc)
		}
		C.postKey(C.int(kc), boolInt(down), C.uint64_t(m.mods))
	}
}

func (m *mac) cgButton() int {
	switch {
	case m.buttons[2]:
		return 1
	case m.buttons[1]:
		return 2
	}
	return 0
}

func (m *mac) ReleaseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for kc := range m.heldKeys {
		C.postKey(C.int(kc), 0, 0)
	}
	m.heldKeys = map[int]bool{}
	m.mods = 0
	for b, held := range m.buttons {
		if held {
			m.Handle(proto.InputEvent{T: "mu", B: b})
		}
	}
}

func boolInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

var modFlags = map[string]uint64{
	"ShiftLeft": flagShift, "ShiftRight": flagShift,
	"ControlLeft": flagCtrl, "ControlRight": flagCtrl,
	"AltLeft": flagAlt, "AltRight": flagAlt,
	"MetaLeft": flagCmd, "MetaRight": flagCmd,
}

// keyCodes maps KeyboardEvent.code to macOS virtual key codes (Carbon kVK_*).
var keyCodes = map[string]int{
	"KeyA": 0x00, "KeyS": 0x01, "KeyD": 0x02, "KeyF": 0x03, "KeyH": 0x04, "KeyG": 0x05, "KeyZ": 0x06, "KeyX": 0x07,
	"KeyC": 0x08, "KeyV": 0x09, "KeyB": 0x0B, "KeyQ": 0x0C, "KeyW": 0x0D, "KeyE": 0x0E, "KeyR": 0x0F, "KeyY": 0x10,
	"KeyT": 0x11, "Digit1": 0x12, "Digit2": 0x13, "Digit3": 0x14, "Digit4": 0x15, "Digit6": 0x16, "Digit5": 0x17,
	"Equal": 0x18, "Digit9": 0x19, "Digit7": 0x1A, "Minus": 0x1B, "Digit8": 0x1C, "Digit0": 0x1D, "BracketRight": 0x1E,
	"KeyO": 0x1F, "KeyU": 0x20, "BracketLeft": 0x21, "KeyI": 0x22, "KeyP": 0x23, "Enter": 0x24, "KeyL": 0x25, "KeyJ": 0x26,
	"Quote": 0x27, "KeyK": 0x28, "Semicolon": 0x29, "Backslash": 0x2A, "Comma": 0x2B, "Slash": 0x2C, "KeyN": 0x2D,
	"KeyM": 0x2E, "Period": 0x2F, "Tab": 0x30, "Space": 0x31, "Backquote": 0x32, "Backspace": 0x33, "Escape": 0x35,
	"MetaRight": 0x36, "MetaLeft": 0x37, "ShiftLeft": 0x38, "CapsLock": 0x39, "AltLeft": 0x3A, "ControlLeft": 0x3B,
	"ShiftRight": 0x3C, "AltRight": 0x3D, "ControlRight": 0x3E, "Fn": 0x3F,
	"NumpadDecimal": 0x41, "NumpadMultiply": 0x43, "NumpadAdd": 0x45, "NumLock": 0x47, "NumpadDivide": 0x4B,
	"NumpadEnter": 0x4C, "NumpadSubtract": 0x4E, "NumpadEqual": 0x51, "Numpad0": 0x52, "Numpad1": 0x53, "Numpad2": 0x54,
	"Numpad3": 0x55, "Numpad4": 0x56, "Numpad5": 0x57, "Numpad6": 0x58, "Numpad7": 0x59, "Numpad8": 0x5B, "Numpad9": 0x5C,
	"F5": 0x60, "F6": 0x61, "F7": 0x62, "F3": 0x63, "F8": 0x64, "F9": 0x65, "F11": 0x67, "F13": 0x69, "F16": 0x6A,
	"F14": 0x6B, "F10": 0x6D, "F12": 0x6F, "F15": 0x71, "Help": 0x72, "Insert": 0x72, "Home": 0x73, "PageUp": 0x74,
	"Delete": 0x75, "F4": 0x76, "End": 0x77, "F2": 0x78, "PageDown": 0x79, "F1": 0x7A, "ArrowLeft": 0x7B,
	"ArrowRight": 0x7C, "ArrowDown": 0x7D, "ArrowUp": 0x7E,
}
