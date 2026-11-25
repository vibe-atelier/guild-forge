//go:build windows

package capture

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32  = syscall.NewLazyDLL("user32.dll")
	gdi32   = syscall.NewLazyDLL("gdi32.dll")
	dwmapi  = syscall.NewLazyDLL("dwmapi.dll")
	shcore  = syscall.NewLazyDLL("shcore.dll")
	getDC   = user32.NewProc("GetDC")
	relDC   = user32.NewProc("ReleaseDC")
	enumMon = user32.NewProc("EnumDisplayMonitors")
	monInfo = user32.NewProc("GetMonitorInfoW")
	enumWin = user32.NewProc("EnumWindows")
	isVis   = user32.NewProc("IsWindowVisible")
	isIcon  = user32.NewProc("IsIconic")
	winText = user32.NewProc("GetWindowTextW")
	winLen  = user32.NewProc("GetWindowTextLengthW")
	winRect = user32.NewProc("GetWindowRect")
	isWin   = user32.NewProc("IsWindow")
	winLong = user32.NewProc("GetWindowLongW")
	owner   = user32.NewProc("GetWindow")
	printW  = user32.NewProc("PrintWindow")
	dpiCtx  = user32.NewProc("SetThreadDpiAwarenessContext")
	dwmAttr = dwmapi.NewProc("DwmGetWindowAttribute")
	mkDC    = gdi32.NewProc("CreateCompatibleDC")
	delDC   = gdi32.NewProc("DeleteDC")
	mkDIB   = gdi32.NewProc("CreateDIBSection")
	selObj  = gdi32.NewProc("SelectObject")
	delObj  = gdi32.NewProc("DeleteObject")
	bitBlt  = gdi32.NewProc("BitBlt")
	_       = shcore
)

type rect struct{ L, T, R, B int32 }

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

// realPixels makes this thread see screens in real pixels, not scaled ones.
func realPixels() {
	const perMonitorAwareV2 = ^uintptr(3) // This is DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4).
	if dpiCtx.Find() == nil {
		dpiCtx.Call(perMonitorAwareV2)
	}
}

func screens() []rect {
	var out []rect
	cb := syscall.NewCallback(func(h, hdc, r, l uintptr) uintptr {
		mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
		monInfo.Call(h, uintptr(unsafe.Pointer(&mi)))
		out = append(out, mi.Monitor)
		return 1
	})
	enumMon.Call(0, 0, cb, 0)
	return out
}

// Targets lists the screens and the windows that can be shared.
func Targets() ([]Target, error) {
	if Demo {
		return demoTargets(), nil
	}
	realPixels()
	var out []Target
	all := screens()
	for i, r := range all {
		w, h := int(r.R-r.L), int(r.B-r.T)
		out = append(out, Target{ID: fmt.Sprintf("screen:%d", i), Name: screenName(i, len(all), w, h), Screen: true, W: w, H: h})
	}
	cb := syscall.NewCallback(func(hwnd, l uintptr) uintptr {
		if v, _, _ := isVis.Call(hwnd); v == 0 {
			return 1
		}
		if ic, _, _ := isIcon.Call(hwnd); ic != 0 {
			return 1 // Minimised windows can't be drawn.
		}
		if o, _, _ := owner.Call(hwnd, 4); o != 0 { // Windows with an owner (GW_OWNER) are dialogs and pop-ups.
			return 1
		}
		const gwlExStyle, wsExToolWindow = ^uintptr(19), 0x80 // GWL_EXSTYLE is -20.
		if ex, _, _ := winLong.Call(hwnd, gwlExStyle); ex&wsExToolWindow != 0 {
			return 1
		}
		var cloaked uint32
		if dwmAttr.Find() == nil {
			dwmAttr.Call(hwnd, 14, uintptr(unsafe.Pointer(&cloaked)), 4) // Attribute 14 is DWMWA_CLOAKED.
		}
		if cloaked != 0 {
			return 1
		}
		n, _, _ := winLen.Call(hwnd)
		if n == 0 {
			return 1
		}
		buf := make([]uint16, n+1)
		winText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
		title := strings.TrimSpace(syscall.UTF16ToString(buf))
		var r rect
		winRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		w, h := int(r.R-r.L), int(r.B-r.T)
		if title == "" || w < 80 || h < 60 || title == "Program Manager" {
			return 1
		}
		out = append(out, Target{ID: "window:" + strconv.FormatUint(uint64(hwnd), 10), Name: title, W: w, H: h})
		return 1
	})
	enumWin.Call(cb, 0)
	return out, nil
}

// Grab takes one picture of a target.
func Grab(t Target) (*Picture, error) {
	if Demo {
		return demoGrab(t)
	}
	realPixels()
	if strings.HasPrefix(t.ID, "screen:") {
		i, _ := strconv.Atoi(strings.TrimPrefix(t.ID, "screen:"))
		list := screens()
		if i < 0 || i >= len(list) {
			return nil, ErrGone
		}
		r := list[i]
		return grabDC(0, r.L, r.T, int(r.R-r.L), int(r.B-r.T))
	}
	h, err := strconv.ParseUint(strings.TrimPrefix(t.ID, "window:"), 10, 64)
	if err != nil {
		return nil, ErrGone
	}
	hwnd := uintptr(h)
	if ok, _, _ := isWin.Call(hwnd); ok == 0 {
		return nil, ErrGone
	}
	var r rect
	winRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return grabWindow(hwnd, int(r.R-r.L), int(r.B-r.T))
}

type bitmapInfo struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPels, YPels  int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// dib makes a 32-bit picture in memory to draw into.
func dib(w, h int) (dc, bmp, old uintptr, bits unsafe.Pointer, err error) {
	if w <= 0 || h <= 0 {
		return 0, 0, 0, nil, ErrGone
	}
	dc, _, _ = mkDC.Call(0)
	bi := bitmapInfo{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	var p unsafe.Pointer
	bmp, _, _ = mkDIB.Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&p)), 0, 0)
	if bmp == 0 || p == nil {
		delDC.Call(dc)
		return 0, 0, 0, nil, fmt.Errorf("capture: no memory for a %d×%d picture", w, h)
	}
	old, _, _ = selObj.Call(dc, bmp)
	return dc, bmp, old, p, nil
}

func finish(dc, bmp, old uintptr, bits unsafe.Pointer, w, h int) *Picture {
	pix := make([]byte, w*h*4)
	copy(pix, unsafe.Slice((*byte)(bits), w*h*4))
	selObj.Call(dc, old)
	delObj.Call(bmp)
	delDC.Call(dc)
	return &Picture{Pix: pix, W: w, H: h}
}

// grabDC copies part of the whole desktop (what is on screen).
func grabDC(_ uintptr, x, y int32, w, h int) (*Picture, error) {
	dc, bmp, old, bits, err := dib(w, h)
	if err != nil {
		return nil, err
	}
	screen, _, _ := getDC.Call(0)
	const srcCopy, captureBlt = 0x00CC0020, 0x40000000
	bitBlt.Call(dc, 0, 0, uintptr(w), uintptr(h), screen, uintptr(x), uintptr(y), srcCopy|captureBlt)
	relDC.Call(0, screen)
	return finish(dc, bmp, old, bits, w, h), nil
}

// grabWindow asks a window to draw itself, so it shows even when covered.
func grabWindow(hwnd uintptr, w, h int) (*Picture, error) {
	dc, bmp, old, bits, err := dib(w, h)
	if err != nil {
		return nil, err
	}
	const pwRenderFullContent = 2
	printW.Call(hwnd, dc, pwRenderFullContent)
	return finish(dc, bmp, old, bits, w, h), nil
}
