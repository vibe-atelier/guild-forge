//go:build !linux && !windows

package console

import "errors"

// hideTyping is not supported on this system; input is read normally.
func hideTyping() (func(), error) {
	return nil, errors.New("changing the terminal mode is not supported on this system")
}
