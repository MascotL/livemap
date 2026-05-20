package capture

import (
	"fmt"
	"image"
	"syscall"

	"github.com/lxn/win"
)

var (
	procPrintWindow = user32.NewProc("PrintWindow")
)

const pwRenderFullContent = 0x00000002

type printWindowCapturer struct{}

func (c *printWindowCapturer) Name() string {
	return "PrintWindow"
}

func (c *printWindowCapturer) Close() {}

func (c *printWindowCapturer) Capture(hwnd win.HWND) (*Frame, error) {
	if win.IsIconic(hwnd) {
		return nil, fmt.Errorf("窗口已最小化")
	}

	windowBounds, err := windowRect(hwnd)
	if err != nil {
		return nil, err
	}
	clientBounds, err := clientRectOnScreen(hwnd)
	if err != nil {
		return nil, err
	}

	srcDC := win.GetDC(hwnd)
	if srcDC == 0 {
		return nil, fmt.Errorf("GetDC 失败")
	}
	defer win.ReleaseDC(hwnd, srcDC)

	memDC := win.CreateCompatibleDC(srcDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC 失败")
	}
	defer win.DeleteDC(memDC)

	bitmap, pixels, err := createDIBSection(srcDC, windowBounds.Dx(), windowBounds.Dy())
	if err != nil {
		return nil, err
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))

	oldObj := win.SelectObject(memDC, win.HGDIOBJ(bitmap))
	defer win.SelectObject(memDC, oldObj)

	ret, _, callErr := procPrintWindow.Call(uintptr(hwnd), uintptr(memDC), uintptr(pwRenderFullContent))
	if ret == 0 {
		if callErr != syscall.Errno(0) {
			return nil, callErr
		}
		return nil, fmt.Errorf("PrintWindow 返回失败")
	}

	full := dibToImage(pixels, windowBounds.Dx(), windowBounds.Dy())
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
