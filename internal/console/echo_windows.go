//go:build windows

package console

import "syscall"

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

const enableEchoInput = 0x0004

// hideTyping stops the console showing what is typed and returns a function
// that puts it back. It fails if input is not a console.
func hideTyping() (func(), error) {
	h := syscall.Stdin
	var old uint32
	if err := syscall.GetConsoleMode(h, &old); err != nil {
		return nil, err
	}
	mode := old &^ enableEchoInput
	if r, _, err := procSetConsoleMode.Call(uintptr(h), uintptr(mode)); r == 0 {
		return nil, err
	}
	return func() {
		procSetConsoleMode.Call(uintptr(h), uintptr(old))
	}, nil
}
