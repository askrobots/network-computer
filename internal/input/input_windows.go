//go:build windows

package input

import (
	"encoding/binary"
	"log"
	"sync"
	"syscall"
	"unsafe"

	"github.com/askrobots/network-computer/internal/proto"
)

// Windows injector via SendInput (user32.dll). Pure Go, no cgo. One INPUT is 40
// bytes on amd64: DWORD type + 4 pad + a 32-byte union. We build the bytes by
// hand to avoid union-layout guesswork.

var (
	user32      = syscall.NewLazyDLL("user32.dll")
	procSend    = user32.NewProc("SendInput")
	procMetrics = user32.NewProc("GetSystemMetrics")
)

const (
	inputMouse    = 0
	inputKeyboard = 1
	inputSize     = 40

	mouseMove       = 0x0001
	mouseLeftDown   = 0x0002
	mouseLeftUp     = 0x0004
	mouseRightDown  = 0x0008
	mouseRightUp    = 0x0010
	mouseMiddleDown = 0x0020
	mouseMiddleUp   = 0x0040
	mouseWheel      = 0x0800
	mouseHWheel     = 0x1000
	mouseAbsolute   = 0x8000
	mouseVirtualDsk = 0x4000

	keyKeyUp    = 0x0002
	keyExtended = 0x0001
	wheelDelta  = 120
	smXVirtual  = 76
	smYVirtual  = 77
	smCXVirtual = 78
	smCYVirtual = 79
)

type win struct {
	mu       sync.Mutex
	heldKeys map[uint16]bool
	buttons  [3]bool
}

func newPlatform(display int) (Injector, error) {
	log.Printf("input: using SendInput (windows)")
	return &win{heldKeys: map[uint16]bool{}}, nil
}

func metric(i uintptr) int32 {
	r, _, _ := procMetrics.Call(i)
	return int32(r)
}

func send(buf []byte) {
	procSend.Call(1, uintptr(unsafe.Pointer(&buf[0])), uintptr(inputSize))
}

func mouseEvent(flags uint32, dx, dy int32, data uint32) {
	b := make([]byte, inputSize)
	binary.LittleEndian.PutUint32(b[0:], inputMouse)
	// union at offset 8: dx, dy, mouseData, dwFlags, time, dwExtraInfo
	binary.LittleEndian.PutUint32(b[8:], uint32(dx))
	binary.LittleEndian.PutUint32(b[12:], uint32(dy))
	binary.LittleEndian.PutUint32(b[16:], data)
	binary.LittleEndian.PutUint32(b[20:], flags)
	send(b)
}

func keyEvent(vk uint16, flags uint32) {
	b := make([]byte, inputSize)
	binary.LittleEndian.PutUint32(b[0:], inputKeyboard)
	// union at offset 8: wVk, wScan, dwFlags, time, dwExtraInfo
	binary.LittleEndian.PutUint16(b[8:], vk)
	binary.LittleEndian.PutUint32(b[12:], flags)
	send(b)
}

func (w *win) Handle(e proto.InputEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch e.T {
	case "mm":
		// absolute over the virtual desktop, normalised 0..65535
		vx, vy := metric(smXVirtual), metric(smYVirtual)
		vw, vh := metric(smCXVirtual), metric(smCYVirtual)
		if vw <= 0 || vh <= 0 {
			return
		}
		nx := int32((e.X*float64(vw) + float64(vx)) / float64(vw) * 65535)
		ny := int32((e.Y*float64(vh) + float64(vy)) / float64(vh) * 65535)
		mouseEvent(mouseMove|mouseAbsolute|mouseVirtualDsk, nx, ny, 0)
	case "mr":
		mouseEvent(mouseMove, int32(e.DX), int32(e.DY), 0)
	case "md", "mu":
		down := e.T == "md"
		var flag uint32
		switch e.B {
		case 0:
			flag = pick(down, mouseLeftDown, mouseLeftUp)
		case 1:
			flag = pick(down, mouseMiddleDown, mouseMiddleUp)
		case 2:
			flag = pick(down, mouseRightDown, mouseRightUp)
		default:
			return
		}
		if e.B >= 0 && e.B < 3 {
			w.buttons[e.B] = down
		}
		mouseEvent(flag, 0, 0, 0)
	case "wh":
		if e.DY != 0 {
			mouseEvent(mouseWheel, 0, 0, uint32(int32(-e.DY/100*wheelDelta)))
		}
		if e.DX != 0 {
			mouseEvent(mouseHWheel, 0, 0, uint32(int32(e.DX/100*wheelDelta)))
		}
	case "kd", "ku":
		k, ok := winVK[e.Code]
		if !ok {
			log.Printf("input: unmapped key %q", e.Code)
			return
		}
		vk := k.vk
		flags := uint32(0)
		if k.ext {
			flags |= keyExtended
		}
		down := e.T == "kd"
		if !down {
			flags |= keyKeyUp
			delete(w.heldKeys, vk)
		} else {
			w.heldKeys[vk] = true
		}
		keyEvent(vk, flags)
	}
}

func (w *win) ReleaseAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for vk := range w.heldKeys {
		keyEvent(vk, keyKeyUp)
	}
	w.heldKeys = map[uint16]bool{}
	for i, held := range w.buttons {
		if held {
			w.buttons[i] = false
			switch i {
			case 0:
				mouseEvent(mouseLeftUp, 0, 0, 0)
			case 1:
				mouseEvent(mouseMiddleUp, 0, 0, 0)
			case 2:
				mouseEvent(mouseRightUp, 0, 0, 0)
			}
		}
	}
}

func pick(cond bool, a, b uint32) uint32 {
	if cond {
		return a
	}
	return b
}

// winVK maps KeyboardEvent.code -> (virtual-key, isExtended).
var winVK = func() map[string]struct {
	vk  uint16
	ext bool
} {
	m := map[string]struct {
		vk  uint16
		ext bool
	}{}
	set := func(code string, vk uint16, ext bool) {
		m[code] = struct {
			vk  uint16
			ext bool
		}{vk, ext}
	}
	for c := byte('A'); c <= 'Z'; c++ {
		set("Key"+string(c), uint16(c), false)
	}
	for d := 0; d <= 9; d++ {
		set("Digit"+string(rune('0'+d)), uint16('0'+d), false)
	}
	for f := 1; f <= 12; f++ {
		set("F"+itoa(f), uint16(0x70+f-1), false)
	}
	set("Enter", 0x0D, false)
	set("Escape", 0x1B, false)
	set("Backspace", 0x08, false)
	set("Tab", 0x09, false)
	set("Space", 0x20, false)
	set("CapsLock", 0x14, false)
	set("ShiftLeft", 0xA0, false)
	set("ShiftRight", 0xA1, true)
	set("ControlLeft", 0xA2, false)
	set("ControlRight", 0xA3, true)
	set("AltLeft", 0xA4, false)
	set("AltRight", 0xA5, true)
	set("MetaLeft", 0x5B, true)
	set("MetaRight", 0x5C, true)
	set("ArrowLeft", 0x25, true)
	set("ArrowUp", 0x26, true)
	set("ArrowRight", 0x27, true)
	set("ArrowDown", 0x28, true)
	set("Home", 0x24, true)
	set("End", 0x23, true)
	set("PageUp", 0x21, true)
	set("PageDown", 0x22, true)
	set("Insert", 0x2D, true)
	set("Delete", 0x2E, true)
	set("Semicolon", 0xBA, false)
	set("Equal", 0xBB, false)
	set("Comma", 0xBC, false)
	set("Minus", 0xBD, false)
	set("Period", 0xBE, false)
	set("Slash", 0xBF, false)
	set("Backquote", 0xC0, false)
	set("BracketLeft", 0xDB, false)
	set("Backslash", 0xDC, false)
	set("BracketRight", 0xDD, false)
	set("Quote", 0xDE, false)
	return m
}()

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
