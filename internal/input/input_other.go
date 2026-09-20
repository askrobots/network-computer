//go:build !darwin && !linux

package input

import "fmt"

func newPlatform(display int) (Injector, error) {
	return nil, fmt.Errorf("input injection not implemented on this OS yet (windows: SendInput)")
}
