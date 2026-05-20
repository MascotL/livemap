package view

import (
	"context"
	"fmt"
	"image"
	"io"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"github.com/mascotl/livemap/internal/logx"
)

const viewerClassName = "LiveMapFrameViewer"
const wmFrameReady = win.WM_APP + 1

var (
	currentViewer *Viewer
	classOnce     sync.Once
	user32        = syscall.NewLazyDLL("user32.dll")
	gdi32         = syscall.NewLazyDLL("gdi32.dll")
	procDrawTextW = user32.NewProc("DrawTextW")
	procStretch   = gdi32.NewProc("SetStretchBltMode")
)

type Viewer struct {
	stream io.Reader
	frames <-chan DebugFrame
	title  string

	hwnd            win.HWND
	frameMu         sync.RWMutex
	frame           *Frame
	frameBGRA       []byte
	streamErr       error
	closedOnce      sync.Once
	firstFrameRead  bool
	firstFramePaint bool
	fixedClientSize bool
	memDC           win.HDC
	bitmap          win.HBITMAP
	bitmapBits      []byte
	bitmapWidth     int
	bitmapHeight    int
}

type DebugFrame struct {
	Image     *image.RGBA
	FrameID   uint64
	Timestamp int64
}

func Run(stream io.Reader, title string) error {
	return RunContext(context.Background(), stream, title)
}

func RunContext(ctx context.Context, stream io.Reader, title string) error {
	return run(ctx, &Viewer{stream: stream, title: title})
}

func RunDebugContext(ctx context.Context, frames <-chan DebugFrame, title string) error {
	return run(ctx, &Viewer{frames: frames, title: title})
}

func run(ctx context.Context, viewer *Viewer) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	logx.Infof("debug 窗口准备启动: %s", viewer.title)
	currentViewer = viewer
	defer func() { currentViewer = nil }()

	if err := viewer.initWindow(); err != nil {
		return err
	}

	if viewer.frames != nil {
		go viewer.frameLoop()
	} else {
		go viewer.readLoop()
	}
	go func() {
		<-ctx.Done()
		if viewer.hwnd != 0 {
			win.PostMessage(viewer.hwnd, win.WM_CLOSE, 0, 0)
		}
	}()

	var msg win.MSG
	for {
		ret := win.GetMessage(&msg, 0, 0, 0)
		switch ret {
		case 0:
			if err := ctx.Err(); err != nil {
				return err
			}
			return viewer.streamErr
		case -1:
			return fmt.Errorf("GetMessage 失败")
		default:
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}
	}
}

func (v *Viewer) initWindow() error {
	if err := registerWindowClass(); err != nil {
		return err
	}

	titlePtr, _ := syscall.UTF16PtrFromString(v.title)
	hwnd := win.CreateWindowEx(
		0,
		syscall.StringToUTF16Ptr(viewerClassName),
		titlePtr,
		win.WS_OVERLAPPED|win.WS_CAPTION|win.WS_SYSMENU|win.WS_VISIBLE,
		win.CW_USEDEFAULT,
		win.CW_USEDEFAULT,
		960,
		640,
		0,
		0,
		0,
		nil,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx 失败")
	}

	v.hwnd = hwnd
	logx.Infof("debug 窗口已创建: HWND=%d", hwnd)
	win.ShowWindow(hwnd, win.SW_SHOW)
	win.UpdateWindow(hwnd)
	return nil
}

func registerWindowClass() error {
	var regErr error
	classOnce.Do(func() {
		wndClass := win.WNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
			LpfnWndProc:   syscall.NewCallback(wndProc),
			HInstance:     0,
			HCursor:       win.LoadCursor(0, (*uint16)(unsafe.Pointer(uintptr(win.IDC_ARROW)))),
			HbrBackground: win.HBRUSH(win.COLOR_WINDOW + 1),
			LpszClassName: syscall.StringToUTF16Ptr(viewerClassName),
		}
		if win.RegisterClassEx(&wndClass) == 0 {
			regErr = fmt.Errorf("RegisterClassEx 失败")
		}
	})
	return regErr
}

func (v *Viewer) readLoop() {
	logx.Infof("预览窗口开始等待视频流")
	for {
		frame, err := ReadFrame(v.stream)
		if err != nil {
			if err != io.EOF {
				v.streamErr = err
				logx.Errorf("读取视频流失败: %v", err)
			} else {
				logx.Warnf("视频流已结束")
			}
			if v.hwnd != 0 {
				win.PostMessage(v.hwnd, win.WM_CLOSE, 0, 0)
			}
			return
		}

		if !v.firstFrameRead {
			logx.Infof("预览窗口收到首帧: Frame=%d, Size=%dx%d, Stride=%d", frame.FrameID, frame.Width, frame.Height, frame.Stride)
			v.firstFrameRead = true
		}

		bgra := frameToBGRA(frame)

		v.frameMu.Lock()
		v.frame = frame
		v.frameBGRA = bgra
		v.frameMu.Unlock()

		if v.hwnd != 0 {
			win.PostMessage(v.hwnd, wmFrameReady, 0, 0)
		}
	}
}

func (v *Viewer) frameLoop() {
	logx.Infof("debug 窗口开始等待工作流帧")
	for item := range v.frames {
		if item.Image == nil {
			continue
		}
		if !v.firstFrameRead {
			logx.Infof("debug 窗口收到首帧: Frame=%d, Size=%dx%d", item.FrameID, item.Image.Rect.Dx(), item.Image.Rect.Dy())
			v.firstFrameRead = true
		}

		pix := make([]byte, len(item.Image.Pix))
		copy(pix, item.Image.Pix)
		frame := &Frame{
			Width:     item.Image.Rect.Dx(),
			Height:    item.Image.Rect.Dy(),
			Stride:    item.Image.Stride,
			FrameID:   item.FrameID,
			Timestamp: item.Timestamp,
			Pix:       pix,
		}
		bgra := frameToBGRA(frame)

		v.frameMu.Lock()
		v.frame = frame
		v.frameBGRA = bgra
		v.frameMu.Unlock()

		if v.hwnd != 0 {
			win.PostMessage(v.hwnd, wmFrameReady, 0, 0)
		}
	}
	if v.hwnd != 0 {
		win.PostMessage(v.hwnd, win.WM_CLOSE, 0, 0)
	}
}

func (v *Viewer) paint() {
	var ps win.PAINTSTRUCT
	hdc := win.BeginPaint(v.hwnd, &ps)
	defer win.EndPaint(v.hwnd, &ps)

	var client win.RECT
	win.GetClientRect(v.hwnd, &client)

	v.frameMu.RLock()
	frame := v.frame
	bgra := v.frameBGRA
	v.frameMu.RUnlock()

	if frame == nil || len(bgra) == 0 {
		text := syscall.StringToUTF16Ptr("等待视频流...")
		drawText(hdc, text, &client)
		return
	}

	if !v.firstFramePaint {
		logx.Infof("预览窗口开始绘制首帧: Frame=%d", frame.FrameID)
		v.firstFramePaint = true
	}
	if !v.fixedClientSize {
		v.setClientSize(frame.Width, frame.Height)
		v.fixedClientSize = true
	}

	if err := v.ensureBitmap(hdc, frame.Width, frame.Height); err != nil {
		logx.Errorf("预览窗口创建位图失败: %v", err)
		return
	}
	copy(v.bitmapBits, bgra)

	oldMode, _, _ := procStretch.Call(uintptr(hdc), 4)
	if oldMode != 0 {
		defer procStretch.Call(uintptr(hdc), oldMode)
	}
	ok := win.StretchBlt(
		hdc,
		0,
		0,
		client.Right-client.Left,
		client.Bottom-client.Top,
		v.memDC,
		0,
		0,
		int32(frame.Width),
		int32(frame.Height),
		win.SRCCOPY,
	)
	if !ok {
		logx.Errorf("预览窗口绘制失败: Frame=%d, Width=%d, Height=%d", frame.FrameID, frame.Width, frame.Height)
		return
	}
}

func (v *Viewer) setClientSize(width, height int) {
	var windowRect win.RECT
	var clientRect win.RECT
	if !win.GetWindowRect(v.hwnd, &windowRect) || !win.GetClientRect(v.hwnd, &clientRect) {
		return
	}
	nonClientW := int((windowRect.Right - windowRect.Left) - (clientRect.Right - clientRect.Left))
	nonClientH := int((windowRect.Bottom - windowRect.Top) - (clientRect.Bottom - clientRect.Top))
	win.SetWindowPos(
		v.hwnd,
		0,
		0,
		0,
		int32(width+nonClientW),
		int32(height+nonClientH),
		win.SWP_NOMOVE|win.SWP_NOZORDER|win.SWP_NOACTIVATE,
	)
	logx.Infof("debug 窗口已调整为 ROI 1:1 客户区: %dx%d", width, height)
}

func (v *Viewer) close() {
	v.closedOnce.Do(func() {
		win.DestroyWindow(v.hwnd)
	})
}

func wndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	if currentViewer == nil || hwnd != currentViewer.hwnd {
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	}

	switch msg {
	case win.WM_PAINT:
		currentViewer.paint()
		return 0
	case wmFrameReady:
		win.InvalidateRect(hwnd, nil, false)
		return 0
	case win.WM_DESTROY:
		currentViewer.releaseBitmap()
		win.PostQuitMessage(0)
		return 0
	default:
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	}
}

func frameToBGRA(frame *Frame) []byte {
	dst := make([]byte, frame.Width*frame.Height*4)
	for y := 0; y < frame.Height; y++ {
		srcRow := y * frame.Stride
		dstRow := y * frame.Width * 4
		for x := 0; x < frame.Width; x++ {
			src := srcRow + x*4
			dstIndex := dstRow + x*4
			dst[dstIndex] = frame.Pix[src+2]
			dst[dstIndex+1] = frame.Pix[src+1]
			dst[dstIndex+2] = frame.Pix[src]
			dst[dstIndex+3] = frame.Pix[src+3]
		}
	}
	return dst
}

func drawText(hdc win.HDC, text *uint16, rect *win.RECT) {
	const dtFlags = 0x00000001 | 0x00000004 | 0x00000020
	procDrawTextW.Call(
		uintptr(hdc),
		uintptr(unsafe.Pointer(text)),
		uintptr(^uint32(0)),
		uintptr(unsafe.Pointer(rect)),
		uintptr(dtFlags),
	)
}

func (v *Viewer) ensureBitmap(hdc win.HDC, width, height int) error {
	if v.bitmap != 0 && v.bitmapWidth == width && v.bitmapHeight == height {
		return nil
	}

	v.releaseBitmap()

	memDC := win.CreateCompatibleDC(hdc)
	if memDC == 0 {
		return fmt.Errorf("CreateCompatibleDC 失败")
	}

	var bmi win.BITMAPINFO
	bmi.BmiHeader.BiSize = uint32(unsafe.Sizeof(bmi.BmiHeader))
	bmi.BmiHeader.BiWidth = int32(width)
	bmi.BmiHeader.BiHeight = -int32(height)
	bmi.BmiHeader.BiPlanes = 1
	bmi.BmiHeader.BiBitCount = 32
	bmi.BmiHeader.BiCompression = win.BI_RGB

	var bits unsafe.Pointer
	bitmap := win.CreateDIBSection(hdc, &bmi.BmiHeader, win.DIB_RGB_COLORS, &bits, 0, 0)
	if bitmap == 0 || bits == nil {
		win.DeleteDC(memDC)
		return fmt.Errorf("CreateDIBSection 失败")
	}

	win.SelectObject(memDC, win.HGDIOBJ(bitmap))

	v.memDC = memDC
	v.bitmap = bitmap
	v.bitmapBits = unsafe.Slice((*byte)(bits), width*height*4)
	v.bitmapWidth = width
	v.bitmapHeight = height
	logx.Infof("预览窗口已重建位图缓冲: %dx%d", width, height)
	return nil
}

func (v *Viewer) releaseBitmap() {
	if v.bitmap != 0 {
		win.DeleteObject(win.HGDIOBJ(v.bitmap))
		v.bitmap = 0
	}
	if v.memDC != 0 {
		win.DeleteDC(v.memDC)
		v.memDC = 0
	}
	v.bitmapBits = nil
	v.bitmapWidth = 0
	v.bitmapHeight = 0
}
