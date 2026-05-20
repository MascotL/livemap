package capture

import (
	"fmt"
	"image"
	"image/color"
	"unsafe"

	"github.com/lxn/win"
)

type desktopCapturer struct{}

func (c *desktopCapturer) Name() string {
	return "DesktopCopy"
}

func (c *desktopCapturer) Close() {}

func (c *desktopCapturer) Capture(hwnd win.HWND) (*Frame, error) {
	if win.IsIconic(hwnd) {
		return nil, fmt.Errorf("窗口已最小化")
	}

	rect, err := clientRectOnScreen(hwnd)
	if err != nil {
		return nil, err
	}

	srcDC := win.GetDC(0)
	if srcDC == 0 {
		return nil, fmt.Errorf("GetDC 失败")
	}
	defer win.ReleaseDC(0, srcDC)

	memDC := win.CreateCompatibleDC(srcDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC 失败")
	}
	defer win.DeleteDC(memDC)

	bitmap, pixels, err := createDIBSection(srcDC, rect.Dx(), rect.Dy())
	if err != nil {
		return nil, err
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))

	oldObj := win.SelectObject(memDC, win.HGDIOBJ(bitmap))
	defer win.SelectObject(memDC, oldObj)

	if !win.BitBlt(memDC, 0, 0, int32(rect.Dx()), int32(rect.Dy()), srcDC, int32(rect.Min.X), int32(rect.Min.Y), win.SRCCOPY) {
		return nil, fmt.Errorf("BitBlt 失败")
	}

	return NewFrame(dibToImage(pixels, rect.Dx(), rect.Dy())), nil
}

func createDIBSection(dc win.HDC, width, height int) (win.HBITMAP, []byte, error) {
	var header win.BITMAPINFOHEADER
	header.BiSize = uint32(unsafe.Sizeof(header))
	header.BiWidth = int32(width)
	header.BiHeight = -int32(height)
	header.BiPlanes = 1
	header.BiBitCount = 32
	header.BiCompression = win.BI_RGB

	var bits unsafe.Pointer
	bitmap := win.CreateDIBSection(dc, &header, win.DIB_RGB_COLORS, &bits, 0, 0)
	if bitmap == 0 || bits == nil {
		return 0, nil, fmt.Errorf("CreateDIBSection 失败")
	}

	return bitmap, unsafe.Slice((*byte)(bits), width*height*4), nil
}

func dibToImage(buf []byte, width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := (y*width + x) * 4
			img.SetRGBA(x, y, color.RGBA{
				R: buf[i+2],
				G: buf[i+1],
				B: buf[i],
				A: 255,
			})
		}
	}
	return img
}

func windowRect(hwnd win.HWND) (image.Rectangle, error) {
	var rect win.RECT
	if !win.GetWindowRect(hwnd, &rect) {
		return image.Rectangle{}, fmt.Errorf("GetWindowRect 失败")
	}

	width := int(rect.Right - rect.Left)
	height := int(rect.Bottom - rect.Top)
	if width <= 0 || height <= 0 {
		return image.Rectangle{}, fmt.Errorf("窗口尺寸无效: %dx%d", width, height)
	}

	return image.Rect(int(rect.Left), int(rect.Top), int(rect.Right), int(rect.Bottom)), nil
}

func clientRectOnScreen(hwnd win.HWND) (image.Rectangle, error) {
	var rect win.RECT
	if !win.GetClientRect(hwnd, &rect) {
		return image.Rectangle{}, fmt.Errorf("GetClientRect 失败")
	}

	topLeft := win.POINT{X: rect.Left, Y: rect.Top}
	bottomRight := win.POINT{X: rect.Right, Y: rect.Bottom}

	if !win.ClientToScreen(hwnd, &topLeft) {
		return image.Rectangle{}, fmt.Errorf("ClientToScreen(topLeft) 失败")
	}
	if !win.ClientToScreen(hwnd, &bottomRight) {
		return image.Rectangle{}, fmt.Errorf("ClientToScreen(bottomRight) 失败")
	}

	width := int(bottomRight.X - topLeft.X)
	height := int(bottomRight.Y - topLeft.Y)
	if width <= 0 || height <= 0 {
		return image.Rectangle{}, fmt.Errorf("客户区尺寸无效: %dx%d", width, height)
	}

	return image.Rect(int(topLeft.X), int(topLeft.Y), int(bottomRight.X), int(bottomRight.Y)), nil
}

func cropToRect(img *image.RGBA, rect image.Rectangle) (*image.RGBA, error) {
	target := rect.Intersect(img.Bounds())
	if target.Empty() {
		return nil, fmt.Errorf("裁切区域无效: %v", rect)
	}

	cropped := image.NewRGBA(image.Rect(0, 0, target.Dx(), target.Dy()))
	for y := 0; y < target.Dy(); y++ {
		srcStart := img.PixOffset(target.Min.X, target.Min.Y+y)
		srcEnd := srcStart + target.Dx()*4
		dstStart := cropped.PixOffset(0, y)
		copy(cropped.Pix[dstStart:dstStart+target.Dx()*4], img.Pix[srcStart:srcEnd])
	}
	return cropped, nil
}
