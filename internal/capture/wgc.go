package capture

import (
	"fmt"
	"image"
	"image/color"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"github.com/mascotl/livemap/internal/logx"
)

type wgcCapturer struct {
	dll          *syscall.LazyDLL
	createByHWND *syscall.LazyProc
	grabFrame    *syscall.LazyProc
	releaseFrame *syscall.LazyProc
	destroy      *syscall.LazyProc
	lastError    *syscall.LazyProc

	handle uintptr
	hwnd   uintptr
}

func newWGCCapturer() *wgcCapturer {
	return &wgcCapturer{}
}

func (c *wgcCapturer) Name() string {
	return "Windows.Graphics.Capture"
}

func (c *wgcCapturer) Capture(hwnd win.HWND) (*Frame, error) {
	if err := c.ensureDLL(); err != nil {
		return nil, err
	}

	if c.handle == 0 || c.hwnd != uintptr(hwnd) {
		c.Close()
		if err := c.create(uintptr(hwnd)); err != nil {
			return nil, err
		}
	}

	var framePtr uintptr
	var width, height, stride uint32
	r1, _, callErr := c.grabFrame.Call(
		c.handle,
		uintptr(unsafe.Pointer(&framePtr)),
		uintptr(unsafe.Pointer(&width)),
		uintptr(unsafe.Pointer(&height)),
		uintptr(unsafe.Pointer(&stride)),
	)
	if r1 == 0 {
		return nil, c.wrapErr("WgcGrabFrameBGRA", callErr)
	}
	defer c.releaseFrame.Call(c.handle, framePtr)

	windowBounds, err := windowRect(hwnd)
	if err != nil {
		return nil, err
	}
	clientBounds, err := clientRectOnScreen(hwnd)
	if err != nil {
		return nil, err
	}

	full := bgraToRGBA(framePtr, int(width), int(height), int(stride))
	clientRect := image.Rect(
		clientBounds.Min.X-windowBounds.Min.X,
		clientBounds.Min.Y-windowBounds.Min.Y,
		clientBounds.Max.X-windowBounds.Min.X,
		clientBounds.Max.Y-windowBounds.Min.Y,
	)
	cropped, err := cropToRect(full, clientRect)
	if err != nil {
		return nil, err
	}

	return NewFrame(cropped), nil
}

func (c *wgcCapturer) Close() {
	if c.destroy != nil && c.handle != 0 {
		c.destroy.Call(c.handle)
	}
	c.handle = 0
	c.hwnd = 0
}

func (c *wgcCapturer) ensureDLL() error {
	if c.dll != nil {
		return nil
	}

	dllPath := logx.FindLibrary("wgc_capture.dll")
	c.dll = syscall.NewLazyDLL(dllPath)
	c.createByHWND = c.dll.NewProc("WgcCreateSessionByHwnd")
	c.grabFrame = c.dll.NewProc("WgcGrabFrameBGRA")
	c.releaseFrame = c.dll.NewProc("WgcReleaseFrame")
	c.destroy = c.dll.NewProc("WgcDestroySession")
	c.lastError = c.dll.NewProc("WgcLastError")

	if err := c.dll.Load(); err != nil {
		return fmt.Errorf("加载 wgc_capture.dll 失败: path=%s err=%w", dllPath, err)
	}
	return nil
}

func (c *wgcCapturer) create(hwnd uintptr) error {
	var handle uintptr
	r1, _, callErr := c.createByHWND.Call(hwnd, uintptr(unsafe.Pointer(&handle)))
	if r1 == 0 {
		return c.wrapErr("WgcCreateSessionByHwnd", callErr)
	}
	c.handle = handle
	c.hwnd = hwnd
	return nil
}

func (c *wgcCapturer) wrapErr(op string, callErr error) error {
	if c.lastError != nil {
		ptr, _, _ := c.lastError.Call()
		if ptr != 0 {
			msg := syscall.UTF16ToString((*[1 << 15]uint16)(unsafe.Pointer(ptr))[:])
			if msg != "" {
				return fmt.Errorf("%s 失败: %s", op, msg)
			}
		}
	}
	if callErr != syscall.Errno(0) && callErr != nil {
		return fmt.Errorf("%s 失败: %v", op, callErr)
	}
	return fmt.Errorf("%s 失败", op)
}

func bgraToRGBA(framePtr uintptr, width, height, stride int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	buf := unsafe.Slice((*byte)(unsafe.Pointer(framePtr)), stride*height)

	for y := 0; y < height; y++ {
		row := buf[y*stride:]
		for x := 0; x < width; x++ {
			i := x * 4
			img.SetRGBA(x, y, color.RGBA{
				R: row[i+2],
				G: row[i+1],
				B: row[i],
				A: row[i+3],
			})
		}
	}

	return img
}
