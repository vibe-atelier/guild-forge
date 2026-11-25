//go:build !windows

package capture

// Elsewhere capture isn't written yet (Linux comes with the Debian tests).

// Targets lists what can be shared (only the made-up demo screens here).
func Targets() ([]Target, error) {
	if Demo {
		return demoTargets(), nil
	}
	return nil, ErrUnsupported
}

// Grab takes one picture of a target (only demo screens here).
func Grab(t Target) (*Picture, error) {
	if Demo {
		return demoGrab(t)
	}
	return nil, ErrUnsupported
}
