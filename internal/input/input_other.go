//go:build !darwin

package input

import "fmt"

func newPlatform(display int) (Injector, error) {
	return nil, fmt.Errorf("input injection not implemented on this OS yet (linux: uinput, windows: SendInput)")
}
