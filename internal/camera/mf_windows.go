//go:build windows

package camera

// On Windows, cameras use Media Foundation (part of Windows), reached through COM with
// plain system calls, like the sound code. A source reader turns the
// camera's own format into 32-bit RGB (BGRA in memory) for us.

import (
	"fmt"
	"nexus/internal/wincom"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	ole32          = syscall.NewLazyDLL("ole32.dll")
	coInitializeEx = ole32.NewProc("CoInitializeEx")
	coUninitialize = ole32.NewProc("CoUninitialize")
	coTaskMemFree  = ole32.NewProc("CoTaskMemFree")

	mfplat                        = syscall.NewLazyDLL("mfplat.dll")
	mfStartup                     = mfplat.NewProc("MFStartup")
	mfShutdown                    = mfplat.NewProc("MFShutdown")
	mfCreateAttributes            = mfplat.NewProc("MFCreateAttributes")
	mfCreateMediaType             = mfplat.NewProc("MFCreateMediaType")
	mf                            = syscall.NewLazyDLL("mf.dll")
	mfEnumDeviceSources           = mf.NewProc("MFEnumDeviceSources")
	mfreadwrite                   = syscall.NewLazyDLL("mfreadwrite.dll")
	mfCreateSourceReaderFromMedia = mfreadwrite.NewProc("MFCreateSourceReaderFromMediaSource")
)

type guid struct {
	a    uint32
	b, c uint16
	d    [8]byte
}

var (
	attrSourceType    = guid{0xc60ac5fe, 0x252a, 0x478f, [8]byte{0xa0, 0xef, 0xbc, 0x8f, 0xa5, 0xf7, 0xca, 0xd3}}
	sourceTypeVidcap  = guid{0x8ac3587a, 0x4ae7, 0x42d8, [8]byte{0x99, 0xe0, 0x0a, 0x60, 0x13, 0xee, 0xf9, 0x0f}}
	attrFriendlyName  = guid{0x60d0e559, 0x52f8, 0x4fa2, [8]byte{0xbb, 0xce, 0xac, 0xdb, 0x34, 0xa8, 0xec, 0x01}}
	attrSymbolicLink  = guid{0x58f0aad8, 0x22bf, 0x4f8a, [8]byte{0xbb, 0x3d, 0xd2, 0xc4, 0x97, 0x8c, 0x6e, 0x2f}}
	iidIMFMediaSource = guid{0x279a808d, 0xaec7, 0x40c8, [8]byte{0x9c, 0x6b, 0xa6, 0xb4, 0x92, 0xc7, 0x8a, 0x66}}
	attrVideoProcess  = guid{0xfb394f3d, 0xccf1, 0x42ee, [8]byte{0xbb, 0xb3, 0xf9, 0xb8, 0x45, 0xd5, 0x68, 0x1d}}
	mtMajorType       = guid{0x48eba18e, 0xf8c9, 0x4687, [8]byte{0xbf, 0x11, 0x0a, 0x74, 0xc9, 0xf9, 0x6a, 0x8f}}
	mtSubtype         = guid{0xf7e34c9a, 0x42e8, 0x4714, [8]byte{0xb7, 0x4b, 0xcb, 0x29, 0xd7, 0x2c, 0x35, 0xe5}}
	mtFrameSize       = guid{0x1652c33d, 0xd6b2, 0x4012, [8]byte{0xb8, 0x34, 0x72, 0x03, 0x08, 0x49, 0xa3, 0x7d}}
	mtDefaultStride   = guid{0x644b4e48, 0x1e02, 0x4516, [8]byte{0xb0, 0xeb, 0xc0, 0x1c, 0xa9, 0xd4, 0x9a, 0xc6}}
	mediaTypeVideo    = guid{0x73646976, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	videoFormatRGB32  = guid{0x00000016, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
)

const (
	mfVersion           = 0x00020070
	firstVideoStream    = 0xFFFFFFFC
	readerEndOfStream   = 0x2
	coinitMultithreaded = 0

	// Method numbers (each interface's place in its table).
	attrGetUINT32          = 7
	attrGetUINT64          = 8
	attrGetAllocatedString = 13
	attrSetUINT32          = 21
	attrSetGUID            = 24
	activateObject         = 33
	readerGetNativeType    = 5
	readerGetCurrentType   = 6
	readerSetCurrentType   = 7
	readerReadSample       = 9
	sampleToContiguous     = 41
	bufferLock             = 3
	bufferUnlock           = 4
	sourceShutdown         = 12
)

// withMF runs f on a locked thread with COM and Media Foundation ready.
func withMF(f func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	coInitializeEx.Call(0, coinitMultithreaded)
	defer coUninitialize.Call()
	if err := wincom.Check(callProc(mfStartup, mfVersion, 0), "starting Media Foundation"); err != nil {
		return err
	}
	defer mfShutdown.Call()
	return f()
}

func callProc(p *syscall.LazyProc, args ...uintptr) uintptr {
	if p.Find() != nil {
		return 0x80004001 // The interface isn't there (E_NOTIMPL).
	}
	r, _, _ := p.Call(args...)
	return r
}

// activates lists the cameras as Media Foundation "activates"; release
// each, then free the list.
func activates() (list []unsafe.Pointer, free func(), err error) {
	var attr unsafe.Pointer
	if err = wincom.Check(callProc(mfCreateAttributes, uintptr(unsafe.Pointer(&attr)), 1), "listing cameras"); err != nil {
		return nil, func() {}, err
	}
	defer wincom.Release(attr)
	wincom.Call(attr, attrSetGUID, uintptr(unsafe.Pointer(&attrSourceType)), uintptr(unsafe.Pointer(&sourceTypeVidcap)))
	var arr unsafe.Pointer
	var n uint32
	if err = wincom.Check(callProc(mfEnumDeviceSources, uintptr(attr), uintptr(unsafe.Pointer(&arr)), uintptr(unsafe.Pointer(&n))), "listing cameras"); err != nil {
		return nil, func() {}, err
	}
	for i := uint32(0); i < n; i++ {
		list = append(list, *(*unsafe.Pointer)(unsafe.Add(arr, uintptr(i)*unsafe.Sizeof(uintptr(0)))))
	}
	return list, func() {
		for _, a := range list {
			wincom.Release(a)
		}
		coTaskMemFree.Call(uintptr(arr))
	}, nil
}

func attrString(obj unsafe.Pointer, key *guid) string {
	var p unsafe.Pointer
	var n uint32
	if int32(wincom.Call(obj, attrGetAllocatedString, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&n)))) < 0 {
		return ""
	}
	defer coTaskMemFree.Call(uintptr(p))
	return wincom.String(p)
}

func devices() ([]Device, error) {
	var out []Device
	err := withMF(func() error {
		list, free, err := activates()
		if err != nil {
			return err
		}
		defer free()
		for _, a := range list {
			out = append(out, Device{ID: attrString(a, &attrSymbolicLink), Name: attrString(a, &attrFriendlyName)})
		}
		return nil
	})
	return out, err
}

func open(id string) (*Camera, error) {
	c := newCamera()
	ready := make(chan error, 1)
	go func() {
		defer close(c.done)
		defer close(c.frames)
		withMF(func() error {
			reader, source, err := openReader(id)
			if err != nil {
				ready <- err
				return err
			}
			defer func() {
				wincom.Release(reader)
				wincom.Call(source, sourceShutdown)
				wincom.Release(source)
			}()
			w, h, stride, err := chooseFormat(reader)
			ready <- err
			if err != nil {
				return err
			}
			c.readLoop(reader, w, h, stride)
			return nil
		})
	}()
	if err := <-ready; err != nil {
		<-c.done
		return nil, err
	}
	return c, nil
}

// openReader turns the chosen camera on and makes a source reader for it.
func openReader(id string) (reader, source unsafe.Pointer, err error) {
	list, free, err := activates()
	if err != nil {
		return nil, nil, err
	}
	defer free()
	var chosen unsafe.Pointer
	for _, a := range list {
		if id == "" || attrString(a, &attrSymbolicLink) == id {
			chosen = a
			break
		}
	}
	if chosen == nil {
		return nil, nil, errNoCamera
	}
	if err = wincom.Check(wincom.Call(chosen, activateObject, uintptr(unsafe.Pointer(&iidIMFMediaSource)), uintptr(unsafe.Pointer(&source))), "turning the camera on"); err != nil {
		return nil, nil, fmt.Errorf("%w (is another app using it?)", err)
	}
	var attr unsafe.Pointer
	callProc(mfCreateAttributes, uintptr(unsafe.Pointer(&attr)), 1)
	if attr != nil {
		// This lets Media Foundation convert the picture to RGB.
		wincom.Call(attr, attrSetUINT32, uintptr(unsafe.Pointer(&attrVideoProcess)), 1)
		defer wincom.Release(attr)
	}
	if err = wincom.Check(callProc(mfCreateSourceReaderFromMedia, uintptr(source), uintptr(attr), uintptr(unsafe.Pointer(&reader))), "reading the camera"); err != nil {
		wincom.Call(source, sourceShutdown)
		wincom.Release(source)
		return nil, nil, err
	}
	return reader, source, nil
}

// chooseFormat picks the camera's own size closest to 640×480 and asks for
// it as 32-bit RGB. stride is the number of bytes in a row (negative:
// bottom row first).
func chooseFormat(reader unsafe.Pointer) (w, h, stride int, err error) {
	best, bestScore := -1, 1<<30
	for i := 0; i < 200; i++ {
		var mt unsafe.Pointer
		if int32(wincom.Call(reader, readerGetNativeType, firstVideoStream, uintptr(i), uintptr(unsafe.Pointer(&mt)))) < 0 {
			break
		}
		var size uint64
		wincom.Call(mt, attrGetUINT64, uintptr(unsafe.Pointer(&mtFrameSize)), uintptr(unsafe.Pointer(&size)))
		wincom.Release(mt)
		tw, th := int(size>>32), int(uint32(size))
		score := abs(tw-WantW) + abs(th-WantH)
		if tw < WantW {
			score += 2000 // A size smaller than wanted looks worse than a bigger one.
		}
		if tw > 0 && score < bestScore {
			best, bestScore = i, score
		}
	}
	if best >= 0 {
		var mt unsafe.Pointer
		if int32(wincom.Call(reader, readerGetNativeType, firstVideoStream, uintptr(best), uintptr(unsafe.Pointer(&mt)))) >= 0 {
			wincom.Call(reader, readerSetCurrentType, firstVideoStream, 0, uintptr(mt))
			wincom.Release(mt)
		}
	}
	var mt unsafe.Pointer
	if err = wincom.Check(callProc(mfCreateMediaType, uintptr(unsafe.Pointer(&mt))), "setting up the camera"); err != nil {
		return
	}
	defer wincom.Release(mt)
	wincom.Call(mt, attrSetGUID, uintptr(unsafe.Pointer(&mtMajorType)), uintptr(unsafe.Pointer(&mediaTypeVideo)))
	wincom.Call(mt, attrSetGUID, uintptr(unsafe.Pointer(&mtSubtype)), uintptr(unsafe.Pointer(&videoFormatRGB32)))
	if err = wincom.Check(wincom.Call(reader, readerSetCurrentType, firstVideoStream, 0, uintptr(mt)), "setting up the camera"); err != nil {
		return
	}
	var cur unsafe.Pointer
	if err = wincom.Check(wincom.Call(reader, readerGetCurrentType, firstVideoStream, uintptr(unsafe.Pointer(&cur))), "setting up the camera"); err != nil {
		return
	}
	defer wincom.Release(cur)
	var size uint64
	wincom.Call(cur, attrGetUINT64, uintptr(unsafe.Pointer(&mtFrameSize)), uintptr(unsafe.Pointer(&size)))
	w, h = int(size>>32), int(uint32(size))
	var s uint32
	if int32(wincom.Call(cur, attrGetUINT32, uintptr(unsafe.Pointer(&mtDefaultStride)), uintptr(unsafe.Pointer(&s)))) >= 0 {
		stride = int(int32(s))
	} else {
		stride = w * 4
	}
	if w <= 0 || h <= 0 {
		err = fmt.Errorf("the camera gave no picture size")
	}
	return
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// readLoop reads pictures until Close.
func (c *Camera) readLoop(reader unsafe.Pointer, w, h, stride int) {
	row := w * 4
	for {
		select {
		case <-c.stop:
			return
		default:
		}
		var index, flags uint32
		var stamp int64
		var sample unsafe.Pointer
		r := wincom.Call(reader, readerReadSample, firstVideoStream, 0, uintptr(unsafe.Pointer(&index)),
			uintptr(unsafe.Pointer(&flags)), uintptr(unsafe.Pointer(&stamp)), uintptr(unsafe.Pointer(&sample)))
		if int32(r) < 0 || flags&readerEndOfStream != 0 {
			wincom.Release(sample)
			return // The camera was unplugged.
		}
		if sample == nil {
			continue
		}
		var buf unsafe.Pointer
		if int32(wincom.Call(sample, sampleToContiguous, uintptr(unsafe.Pointer(&buf)))) >= 0 {
			var p unsafe.Pointer
			var max, cur uint32
			if int32(wincom.Call(buf, bufferLock, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&max)), uintptr(unsafe.Pointer(&cur)))) >= 0 {
				if int(cur) >= row*h && p != nil {
					data := unsafe.Slice((*byte)(p), int(cur))
					pix := make([]byte, row*h)
					for y := 0; y < h; y++ {
						src := y
						if stride < 0 {
							src = h - 1 - y // The picture's bottom row comes first.
						}
						copy(pix[y*row:(y+1)*row], data[src*row:src*row+row])
					}
					for i := 3; i < len(pix); i += 4 {
						pix[i] = 255
					}
					c.send(Frame{Pix: pix, W: w, H: h})
				}
				wincom.Call(buf, bufferUnlock)
			}
			wincom.Release(buf)
		}
		wincom.Release(sample)
	}
}
