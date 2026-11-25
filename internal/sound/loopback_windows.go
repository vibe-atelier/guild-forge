//go:build windows

package sound

// Recording the computer's sound without Nexus's own: Windows 10
// (2004) and later can record every program except one, through a special
// "process loopback" device. Otherwise people watching a shared screen
// would hear the call's voices, their own included, coming back.

import (
	"errors"
	"nexus/internal/wincom"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	mmdevapi     = syscall.NewLazyDLL("mmdevapi.dll")
	activateIfce = mmdevapi.NewProc("ActivateAudioInterfaceAsync")

	iidCompletionHandler = guid{0x41D949AB, 0x9862, 0x444A, [8]byte{0x80, 0xF6, 0xC2, 0x61, 0x33, 0x4D, 0xA5, 0xEB}}
	iidAgileObject       = guid{0x94EA2B94, 0xE9CC, 0x49E0, [8]byte{0xC0, 0xFF, 0xEE, 0x64, 0xCA, 0x8F, 0x5B, 0x90}}
	iidUnknown           = guid{0x00000000, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// handler is a small COM object Windows calls back when the device is ready.
type handler struct {
	vtbl *[4]uintptr
	done chan struct{}
}

var (
	handlerVtbl [4]uintptr
	handlers    = map[uintptr]*handler{}
)

func init() {
	handlerVtbl[0] = syscall.NewCallback(func(this uintptr, riid *guid, ppv *uintptr) uintptr {
		if *riid == iidUnknown || *riid == iidAgileObject || *riid == iidCompletionHandler {
			*ppv = this
			return 0
		}
		*ppv = 0
		return 0x80004002 // This is E_NOINTERFACE.
	})
	handlerVtbl[1] = syscall.NewCallback(func(this uintptr) uintptr { return 1 })
	handlerVtbl[2] = syscall.NewCallback(func(this uintptr) uintptr { return 1 })
	handlerVtbl[3] = syscall.NewCallback(func(this, op uintptr) uintptr {
		if h := handlers[this]; h != nil {
			close(h.done)
		}
		return 0
	})
}

// loopbackWithoutMe opens an audio client recording every program but this
// one. The client is already started with our format.
func loopbackWithoutMe() (unsafe.Pointer, error) {
	if activateIfce.Find() != nil {
		return nil, errors.New("process loopback is not available")
	}
	// These are AUDIOCLIENT_ACTIVATION_PARAMS: process loopback, leaving out our process tree.
	params := struct {
		kind, pid, mode uint32
	}{1, uint32(os.Getpid()), 1}
	var pv [24]byte // It is a PROPVARIANT holding a BLOB.
	*(*uint16)(unsafe.Pointer(&pv[0])) = 65
	*(*uint32)(unsafe.Pointer(&pv[8])) = uint32(unsafe.Sizeof(params))
	*(*uintptr)(unsafe.Pointer(&pv[16])) = uintptr(unsafe.Pointer(&params))

	h := &handler{vtbl: &handlerVtbl, done: make(chan struct{})}
	key := uintptr(unsafe.Pointer(h))
	handlers[key] = h
	defer delete(handlers, key)
	device, _ := syscall.UTF16PtrFromString(`VAD\Process_Loopback`)
	var op unsafe.Pointer
	r, _, _ := activateIfce.Call(uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(&iidIAudioClient)),
		uintptr(unsafe.Pointer(&pv[0])), key, uintptr(unsafe.Pointer(&op)))
	if err := wincom.Check(r, "opening process loopback"); err != nil {
		return nil, err
	}
	defer wincom.Release(op)
	select {
	case <-h.done:
	case <-time.After(3 * time.Second):
		return nil, errors.New("process loopback did not answer")
	}
	var result uintptr
	var unk unsafe.Pointer
	if err := wincom.Check(wincom.Call(op, 3, uintptr(unsafe.Pointer(&result)), uintptr(unsafe.Pointer(&unk))), "process loopback"); err != nil {
		return nil, err
	}
	if err := wincom.Check(result, "process loopback"); err != nil || unk == nil {
		return nil, errors.New("process loopback was refused")
	}
	client := unk // It is the IAudioClient asked for.
	wf := waveFormat{tag: 1, channels: 1, samplesPerSec: Rate, avgBytesPerSec: Rate * 2, blockAlign: 2, bitsPerSample: 16}
	const bufferTime = 1_000_000
	if err := wincom.Check(wincom.Call(client, 3, sharedMode, flagLoopback|flagAutoConvert|flagSRCQuality, bufferTime, 0, uintptr(unsafe.Pointer(&wf)), 0), "starting process loopback"); err != nil {
		wincom.Release(client)
		return nil, err
	}
	return client, nil
}
