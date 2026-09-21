//go:build linux

package input

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/askrobots/network-computer/internal/proto"
)

// Linux injector on /dev/uinput: one virtual device that is a keyboard, a
// relative mouse and an absolute tablet at once. Works under X11 and Wayland
// with no display-server-specific code. Needs write access to /dev/uinput
// (root, or a udev rule giving the "input" group access).

const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03

	relX      = 0x00
	relY      = 0x01
	relHWheel = 0x06
	relWheel  = 0x08
	absX      = 0x00
	absY      = 0x01

	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112

	uiSetEvBit   = 0x40045564
	uiSetKeyBit  = 0x40045565
	uiSetRelBit  = 0x40045566
	uiSetAbsBit  = 0x40045567
	uiDevCreate  = 0x5501
	uiDevDestroy = 0x5502
	uiDevSetup   = 0x405c5503 // _IOW(UINPUT_IOCTL_BASE, 3, struct uinput_setup)
	uiAbsSetup   = 0x401c5504 // _IOW(UINPUT_IOCTL_BASE, 4, struct uinput_abs_setup)

	absRange = 32767
)

type inputEvent struct {
	Sec, Usec int64
	Type      uint16
	Code      uint16
	Value     int32
}

type uinputSetup struct {
	Bustype uint16
	Vendor  uint16
	Product uint16
	Version uint16
	Name    [80]byte
	FFMax   uint32
}

type uinputAbsSetup struct {
	Code                                            uint16
	_                                               uint16
	Value, Minimum, Maximum, Fuzz, Flat, Resolution int32
}

// Two devices, because libinput treats anything with relative axes as a mouse
// and ignores its absolute events. "kbd" is a keyboard plus relative mouse and
// wheel; "tab" is an absolute pointer shaped like the QEMU USB tablet.
// Three devices, because libinput classifies a device by its axes: a device
// with relative axes is a mouse and its key events are ignored, and a device
// with both relative and absolute axes ignores the absolute ones. So the
// keyboard, the relative mouse and the absolute tablet must be separate.
type linux struct {
	mu              sync.Mutex
	kbd, mouse, tab *os.File
	heldKeys        map[uint16]bool
	buttons         [3]bool
}

const (
	kindKeyboard = iota
	kindMouse
	kindTablet
)

func newPlatform(display int) (Injector, error) {
	kbd, err := createDevice("network-computer keyboard", kindKeyboard)
	if err != nil {
		return nil, err
	}
	mouse, err := createDevice("network-computer mouse", kindMouse)
	if err != nil {
		kbd.Close()
		return nil, err
	}
	tab, err := createDevice("network-computer tablet", kindTablet)
	if err != nil {
		kbd.Close()
		mouse.Close()
		return nil, err
	}
	time.Sleep(300 * time.Millisecond) // let the display server pick the devices up
	log.Printf("input: uinput devices created (keyboard, mouse, tablet)")
	return &linux{kbd: kbd, mouse: mouse, tab: tab, heldKeys: map[uint16]bool{}}, nil
}

func createDevice(name string, kind int) (*os.File, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/uinput: %w (run as root or add a udev rule)", err)
	}
	ioctl := func(req, val uintptr) error {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, val); e != 0 {
			return e
		}
		return nil
	}
	must := func(err error) {
		if err != nil {
			log.Printf("uinput ioctl: %v", err)
		}
	}
	must(ioctl(uiSetEvBit, evKey))
	must(ioctl(uiSetEvBit, evSyn))
	switch kind {
	case kindKeyboard:
		// keyboard keys only, no buttons and no axes, so libinput makes a keyboard
		for k := uintptr(1); k < 256; k++ {
			must(ioctl(uiSetKeyBit, k))
		}
	case kindMouse:
		must(ioctl(uiSetEvBit, evRel))
		for _, b := range []uintptr{btnLeft, btnRight, btnMiddle} {
			must(ioctl(uiSetKeyBit, b))
		}
		for _, r := range []uintptr{relX, relY, relWheel, relHWheel} {
			must(ioctl(uiSetRelBit, r))
		}
	case kindTablet:
		must(ioctl(uiSetEvBit, evAbs))
		for _, b := range []uintptr{btnLeft, btnRight, btnMiddle} {
			must(ioctl(uiSetKeyBit, b))
		}
		for _, a := range []uint16{absX, absY} {
			must(ioctl(uiSetAbsBit, uintptr(a)))
			as := uinputAbsSetup{Code: a, Minimum: 0, Maximum: absRange}
			must(ioctl(uiAbsSetup, uintptr(unsafe.Pointer(&as))))
		}
	}
	us := uinputSetup{Bustype: 0x03, Vendor: 0x1d6b, Product: 0x0104, Version: 1}
	copy(us.Name[:], name)
	if err := ioctl(uiDevSetup, uintptr(unsafe.Pointer(&us))); err != nil {
		f.Close()
		return nil, fmt.Errorf("UI_DEV_SETUP: %w", err)
	}
	if err := ioctl(uiDevCreate, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("UI_DEV_CREATE: %w", err)
	}
	return f, nil
}

func (l *linux) emit(f *os.File, typ, code uint16, val int32) {
	ev := inputEvent{Type: typ, Code: code, Value: val}
	buf := make([]byte, unsafe.Sizeof(ev))
	binary.LittleEndian.PutUint64(buf[0:], uint64(ev.Sec))
	binary.LittleEndian.PutUint64(buf[8:], uint64(ev.Usec))
	binary.LittleEndian.PutUint16(buf[16:], ev.Type)
	binary.LittleEndian.PutUint16(buf[18:], ev.Code)
	binary.LittleEndian.PutUint32(buf[20:], uint32(ev.Value))
	f.Write(buf)
}

func (l *linux) syn(f *os.File) { l.emit(f, evSyn, 0, 0) }

func (l *linux) Handle(e proto.InputEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch e.T {
	case "mm":
		l.emit(l.tab, evAbs, absX, int32(e.X*absRange))
		l.emit(l.tab, evAbs, absY, int32(e.Y*absRange))
		l.syn(l.tab)
	case "mr":
		l.emit(l.mouse, evRel, relX, int32(e.DX))
		l.emit(l.mouse, evRel, relY, int32(e.DY))
		l.syn(l.mouse)
	case "md", "mu":
		var code uint16
		switch e.B {
		case 0:
			code = btnLeft
		case 1:
			code = btnMiddle
		case 2:
			code = btnRight
		default:
			return
		}
		down := e.T == "md"
		l.buttons[e.B] = down
		l.emit(l.tab, evKey, code, boolInt32(down))
		l.syn(l.tab)
	case "wh":
		// browsers send pixels; one wheel notch is about 100px
		if n := int32(-e.DY / 50); n != 0 {
			l.emit(l.mouse, evRel, relWheel, n)
		}
		if n := int32(e.DX / 50); n != 0 {
			l.emit(l.mouse, evRel, relHWheel, n)
		}
		l.syn(l.mouse)
	case "kd", "ku":
		kc, ok := linuxKeyCodes[e.Code]
		if !ok {
			log.Printf("input: unmapped key %q", e.Code)
			return
		}
		down := e.T == "kd"
		if down {
			l.heldKeys[kc] = true
		} else {
			delete(l.heldKeys, kc)
		}
		l.emit(l.kbd, evKey, kc, boolInt32(down))
		l.syn(l.kbd)
	}
}

func (l *linux) ReleaseAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for kc := range l.heldKeys {
		l.emit(l.kbd, evKey, kc, 0)
	}
	l.heldKeys = map[uint16]bool{}
	l.syn(l.kbd)
	for i, code := range []uint16{btnLeft, btnMiddle, btnRight} {
		if l.buttons[i] {
			l.emit(l.tab, evKey, code, 0)
			l.buttons[i] = false
		}
	}
	l.syn(l.tab)
}

func boolInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// linuxKeyCodes maps KeyboardEvent.code to Linux KEY_* codes (input-event-codes.h).
var linuxKeyCodes = map[string]uint16{
	"Escape": 1, "Digit1": 2, "Digit2": 3, "Digit3": 4, "Digit4": 5, "Digit5": 6, "Digit6": 7, "Digit7": 8, "Digit8": 9,
	"Digit9": 10, "Digit0": 11, "Minus": 12, "Equal": 13, "Backspace": 14, "Tab": 15, "KeyQ": 16, "KeyW": 17, "KeyE": 18,
	"KeyR": 19, "KeyT": 20, "KeyY": 21, "KeyU": 22, "KeyI": 23, "KeyO": 24, "KeyP": 25, "BracketLeft": 26,
	"BracketRight": 27, "Enter": 28, "ControlLeft": 29, "KeyA": 30, "KeyS": 31, "KeyD": 32, "KeyF": 33, "KeyG": 34,
	"KeyH": 35, "KeyJ": 36, "KeyK": 37, "KeyL": 38, "Semicolon": 39, "Quote": 40, "Backquote": 41, "ShiftLeft": 42,
	"Backslash": 43, "KeyZ": 44, "KeyX": 45, "KeyC": 46, "KeyV": 47, "KeyB": 48, "KeyN": 49, "KeyM": 50, "Comma": 51,
	"Period": 52, "Slash": 53, "ShiftRight": 54, "NumpadMultiply": 55, "AltLeft": 56, "Space": 57, "CapsLock": 58,
	"F1": 59, "F2": 60, "F3": 61, "F4": 62, "F5": 63, "F6": 64, "F7": 65, "F8": 66, "F9": 67, "F10": 68, "NumLock": 69,
	"ScrollLock": 70, "Numpad7": 71, "Numpad8": 72, "Numpad9": 73, "NumpadSubtract": 74, "Numpad4": 75, "Numpad5": 76,
	"Numpad6": 77, "NumpadAdd": 78, "Numpad1": 79, "Numpad2": 80, "Numpad3": 81, "Numpad0": 82, "NumpadDecimal": 83,
	"F11": 87, "F12": 88, "NumpadEnter": 96, "ControlRight": 97, "NumpadDivide": 98, "PrintScreen": 99, "AltRight": 100,
	"Home": 102, "ArrowUp": 103, "PageUp": 104, "ArrowLeft": 105, "ArrowRight": 106, "End": 107, "ArrowDown": 108,
	"PageDown": 109, "Insert": 110, "Delete": 111, "Pause": 119, "MetaLeft": 125, "MetaRight": 126, "ContextMenu": 127,
	"F13": 183, "F14": 184, "F15": 185, "F16": 186, "F17": 187, "F18": 188, "F19": 189,
}
