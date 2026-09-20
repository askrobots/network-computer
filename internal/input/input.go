// Package input injects keyboard and mouse events into the host OS.
package input

import (
	"log"

	"github.com/askrobots/network-computer/internal/proto"
)

// Injector turns InputEvents into OS events.
type Injector interface {
	Handle(proto.InputEvent)
	ReleaseAll() // release held buttons and keys, e.g. when a client drops
}

// New returns the platform injector for the given display, or a logger when
// dryRun is set or the platform has no implementation yet.
func New(display int, dryRun bool) (Injector, error) {
	if dryRun {
		return logInjector{}, nil
	}
	return newPlatform(display)
}

type logInjector struct{}

func (logInjector) Handle(e proto.InputEvent) {
	if e.T == "mm" {
		return // too chatty
	}
	log.Printf("input: %+v", e)
}
func (logInjector) ReleaseAll() {}
