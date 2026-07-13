//go:build !cgo

package midi

import "errors"

// New is unavailable without CGO (rtmidi). Use NewOptional for release builds.
func New() (Midi, error) {
	return nil, errors.New("midi hardware requires cgo (rtmidi); rebuild with CGO_ENABLED=1")
}

// NewOptional returns a silent no-op backend when CGO is disabled
// (cross-compile / release matrix: CGO_ENABLED=0).
func NewOptional() Midi {
	return &noop{}
}
