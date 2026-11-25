//go:build windows

package sound

// On Windows, sound uses the Windows Audio Session API (WASAPI), reached through COM with
// plain system calls. Each stream runs on its own thread, which COM needs.

import (
	"nexus/internal/wincom"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	ole32           = syscall.NewLazyDLL("ole32.dll")
	coInitializeEx  = ole32.NewProc("CoInitializeEx")
	coUninitialize  = ole32.NewProc("CoUninitialize")
	coCreateInstace = ole32.NewProc("CoCreateInstance")
	coTaskMemFree   = ole32.NewProc("CoTaskMemFree")
	propVariantClr  = ole32.NewProc("PropVariantClear")
)

type guid struct {
	a    uint32
	b, c uint16
	d    [8]byte
}

var (
	clsidMMDeviceEnumerator = guid{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = guid{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = guid{0x1CB9AD4C, 0xDBFA, 0x4C32, [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioRenderClient   = guid{0xF294ACFC, 0x3146, 0x4483, [8]byte{0xA7, 0xBF, 0xAD, 0xDC, 0xA7, 0xC2, 0x60, 0xE2}}
	iidIAudioCaptureClient  = guid{0xC8ADBD64, 0xE71E, 0x48A0, [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
	pkeyFriendlyName        = propKey{guid{0xA45C254E, 0xDF1C, 0x4EFD, [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0}}, 14}
)

type propKey struct {
	fmtid guid
	pid   uint32
}

const (
	eRender, eCapture   = 0, 1
	eConsole            = 0
	deviceStateActive   = 1
	clsctxAll           = 23
	coinitMultithreaded = 0
	sharedMode          = 0
	flagAutoConvert     = 0x80000000
	flagSRCQuality      = 0x08000000
	bufferSilent        = 2
	flagLoopback        = 0x00020000
)

// onCOMThread runs f on a locked thread with COM ready.
func onCOMThread(f func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	coInitializeEx.Call(0, coinitMultithreaded)
	defer coUninitialize.Call()
	f()
}

func enumerator() (unsafe.Pointer, error) {
	var e unsafe.Pointer
	r, _, _ := coCreateInstace.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&e)))
	return e, wincom.Check(r, "opening the sound devices")
}

func deviceID(dev unsafe.Pointer) string {
	var p unsafe.Pointer
	if int32(wincom.Call(dev, 5, uintptr(unsafe.Pointer(&p)))) < 0 {
		return ""
	}
	defer coTaskMemFree.Call(uintptr(p))
	return wincom.String(p)
}

func deviceName(dev unsafe.Pointer) string {
	var store unsafe.Pointer
	// Method 4 is OpenPropertyStore(STGM_READ).
	if int32(wincom.Call(dev, 4, 0, uintptr(unsafe.Pointer(&store)))) < 0 {
		return ""
	}
	defer wincom.Release(store)
	var pv [24]byte // It is a PROPVARIANT.
	if int32(wincom.Call(store, 5, uintptr(unsafe.Pointer(&pkeyFriendlyName)), uintptr(unsafe.Pointer(&pv[0])))) < 0 {
		return ""
	}
	defer propVariantClr.Call(uintptr(unsafe.Pointer(&pv[0])))
	return wincom.String(*(*unsafe.Pointer)(unsafe.Pointer(&pv[8])))
}

func list(flow int) ([]Device, error) {
	var out []Device
	var err error
	onCOMThread(func() {
		var e unsafe.Pointer
		if e, err = enumerator(); err != nil {
			return
		}
		defer wincom.Release(e)
		def := ""
		var d unsafe.Pointer
		if int32(wincom.Call(e, 4, uintptr(flow), eConsole, uintptr(unsafe.Pointer(&d)))) >= 0 {
			def = deviceID(d)
			wincom.Release(d)
		}
		var col unsafe.Pointer
		if err = wincom.Check(wincom.Call(e, 3, uintptr(flow), deviceStateActive, uintptr(unsafe.Pointer(&col))), "listing the sound devices"); err != nil {
			return
		}
		defer wincom.Release(col)
		var n uint32
		wincom.Call(col, 3, uintptr(unsafe.Pointer(&n)))
		for i := uint32(0); i < n; i++ {
			var dev unsafe.Pointer
			if int32(wincom.Call(col, 4, uintptr(i), uintptr(unsafe.Pointer(&dev)))) < 0 {
				continue
			}
			id := deviceID(dev)
			out = append(out, Device{ID: id, Name: deviceName(dev), Default: id == def})
			wincom.Release(dev)
		}
	})
	return out, err
}

// Inputs lists the microphones; Outputs lists the speakers.
func Inputs() ([]Device, error)  { return list(eCapture) }
func Outputs() ([]Device, error) { return list(eRender) }

type waveFormat struct {
	tag            uint16
	channels       uint16
	samplesPerSec  uint32
	avgBytesPerSec uint32
	blockAlign     uint16
	bitsPerSample  uint16
	size           uint16
}

// openClient opens a device's audio client for 48 kHz mono 16-bit sound.
func openClient(flow int, id string, extra uintptr) (client unsafe.Pointer, frames uint32, err error) {
	e, err := enumerator()
	if err != nil {
		return nil, 0, err
	}
	defer wincom.Release(e)
	var dev unsafe.Pointer
	if id == "" {
		err = wincom.Check(wincom.Call(e, 4, uintptr(flow), eConsole, uintptr(unsafe.Pointer(&dev))), "finding the default device")
	} else {
		p, _ := syscall.UTF16PtrFromString(id)
		err = wincom.Check(wincom.Call(e, 5, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&dev))), "finding the device")
	}
	if err != nil || dev == nil {
		return nil, 0, ErrNoDevice
	}
	defer wincom.Release(dev)
	if err = wincom.Check(wincom.Call(dev, 3, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client))), "opening the device"); err != nil {
		return nil, 0, err
	}
	wf := waveFormat{tag: 1, channels: 1, samplesPerSec: Rate, avgBytesPerSec: Rate * 2, blockAlign: 2, bitsPerSample: 16}
	const bufferTime = 1_000_000 // This is 100 ms, in 100-nanosecond units.
	if err = wincom.Check(wincom.Call(client, 3, sharedMode, flagAutoConvert|flagSRCQuality|extra, bufferTime, 0, uintptr(unsafe.Pointer(&wf)), 0), "starting the device"); err != nil {
		wincom.Release(client)
		return nil, 0, err
	}
	wincom.Call(client, 4, uintptr(unsafe.Pointer(&frames)))
	return client, frames, nil
}

// Capture records from a microphone.
type Capture struct {
	frames chan []int16
	stop   chan struct{}
	once   sync.Once
	Err    error
}

// OpenCapture starts recording from microphone id ("" = the default).
// Frames of 20 ms arrive on Frames().
func OpenCapture(id string) (*Capture, error) { return openCapture(eCapture, id, 0) }

// OpenLoopback records what speaker id ("" = the default) is playing: the
// computer's own sound, for screen sharing.
func OpenLoopback(id string) (*Capture, error) { return openCapture(eRender, id, flagLoopback) }

func openCapture(flow int, id string, extra uintptr) (*Capture, error) {
	c := &Capture{frames: make(chan []int16, 25), stop: make(chan struct{})}
	ready := make(chan error, 1)
	go onCOMThread(func() {
		var client unsafe.Pointer
		var err error
		if extra == flagLoopback && id == "" {
			client, err = loopbackWithoutMe() // This records everything but Nexus itself.
		}
		if client == nil {
			client, _, err = openClient(flow, id, extra)
		}
		if err != nil {
			ready <- err
			return
		}
		defer wincom.Release(client)
		var cap unsafe.Pointer
		if err := wincom.Check(wincom.Call(client, 14, uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&cap))), "opening the microphone"); err != nil {
			ready <- err
			return
		}
		defer wincom.Release(cap)
		if err := wincom.Check(wincom.Call(client, 10), "starting the microphone"); err != nil {
			ready <- err
			return
		}
		defer wincom.Call(client, 11)
		ready <- nil
		var pending []int16
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-c.stop:
				close(c.frames)
				return
			case <-tick.C:
			}
			for {
				var n uint32
				if int32(wincom.Call(cap, 5, uintptr(unsafe.Pointer(&n)))) < 0 || n == 0 {
					break
				}
				var data unsafe.Pointer
				var got, flags uint32
				if int32(wincom.Call(cap, 3, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&got)), uintptr(unsafe.Pointer(&flags)), 0, 0)) < 0 {
					break
				}
				if flags&bufferSilent != 0 || data == nil {
					pending = append(pending, make([]int16, got)...)
				} else {
					pending = append(pending, unsafe.Slice((*int16)(data), got)...)
				}
				wincom.Call(cap, 4, uintptr(got))
			}
			for len(pending) >= Frame {
				f := make([]int16, Frame)
				copy(f, pending)
				pending = pending[Frame:]
				select {
				case c.frames <- f:
				default: // Nobody is reading, so the oldest sound is dropped.
				}
			}
		}
	})
	if err := <-ready; err != nil {
		return nil, err
	}
	return c, nil
}

// Frames is where the recorded sound arrives, 20 ms at a time.
func (c *Capture) Frames() <-chan []int16 { return c.frames }

// Close stops recording.
func (c *Capture) Close() { c.once.Do(func() { close(c.stop) }) }

// Playback plays to a speaker.
type Playback struct {
	stop chan struct{}
	once sync.Once
}

// OpenPlayback starts playing to speaker id ("" = the default). src is
// asked for 20 ms of sound whenever the speaker needs more.
func OpenPlayback(id string, src Source) (*Playback, error) {
	p := &Playback{stop: make(chan struct{})}
	ready := make(chan error, 1)
	go onCOMThread(func() {
		client, size, err := openClient(eRender, id, 0)
		if err != nil {
			ready <- err
			return
		}
		defer wincom.Release(client)
		var ren unsafe.Pointer
		if err := wincom.Check(wincom.Call(client, 14, uintptr(unsafe.Pointer(&iidIAudioRenderClient)), uintptr(unsafe.Pointer(&ren))), "opening the speaker"); err != nil {
			ready <- err
			return
		}
		defer wincom.Release(ren)
		if err := wincom.Check(wincom.Call(client, 10), "starting the speaker"); err != nil {
			ready <- err
			return
		}
		defer wincom.Call(client, 11)
		ready <- nil
		var left []int16 // It holds sound asked for but not yet written.
		frame := make([]int16, Frame)
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-tick.C:
			}
			var padding uint32
			if int32(wincom.Call(client, 6, uintptr(unsafe.Pointer(&padding)))) < 0 {
				continue
			}
			// Keep about 60 ms queued: enough to never run dry, short
			// enough to stay quick.
			want := int(size) - int(padding)
			if lim := 3*Frame - int(padding); want > lim {
				want = lim
			}
			if want <= 0 {
				continue
			}
			for len(left) < want {
				src(frame)
				left = append(left, frame...)
			}
			var data unsafe.Pointer
			if int32(wincom.Call(ren, 3, uintptr(want), uintptr(unsafe.Pointer(&data)))) < 0 {
				continue
			}
			copy(unsafe.Slice((*int16)(data), want), left[:want])
			left = left[want:]
			wincom.Call(ren, 4, uintptr(want), 0)
		}
	})
	if err := <-ready; err != nil {
		return nil, err
	}
	return p, nil
}

// Close stops playing.
func (p *Playback) Close() { p.once.Do(func() { close(p.stop) }) }
