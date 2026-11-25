// Package wincom holds the few helpers Nexus needs to talk to Windows' COM
// objects (Media Foundation for cameras, WASAPI for sound) with only the
// standard library: calling a method by its number, releasing an object,
// turning a COM result into an error, and reading a COM text.
package wincom

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Call calls method i of a COM object (0 to 2 are QueryInterface, AddRef
// and Release; the object's own methods follow in its documented order).
func Call(obj unsafe.Pointer, i int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(i)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

// Release lets go of a COM object (nil is ignored).
func Release(obj unsafe.Pointer) {
	if obj != nil {
		Call(obj, 2)
	}
}

// Check turns a COM result (an HRESULT) into an error naming what failed;
// nil when it succeeded.
func Check(r uintptr, what string) error {
	if int32(r) < 0 {
		return fmt.Errorf("%s failed (0x%08X)", what, uint32(r))
	}
	return nil
}

// String reads a text Windows wrote as UTF-16 ending in a zero ("" for nil).
func String(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	var s []uint16
	for i := uintptr(0); ; i += 2 {
		c := *(*uint16)(unsafe.Add(p, i))
		if c == 0 {
			break
		}
		s = append(s, c)
	}
	return syscall.UTF16ToString(s)
}
