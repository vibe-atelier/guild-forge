//go:build !windows

package camera

// Other systems: cameras come with the Linux tests (Video4Linux).

func devices() ([]Device, error) { return nil, nil }

func open(string) (*Camera, error) { return nil, errNoCamera }
