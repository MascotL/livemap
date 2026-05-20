package regionselect

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"github.com/mascotl/livemap/internal/appconfig"
	"github.com/mascotl/livemap/internal/logx"
	"github.com/mascotl/livemap/internal/procwin"
	"github.com/mascotl/livemap/internal/taskbarid"
	"github.com/mascotl/livemap/internal/windowicon"
)

const (
	className   = "LiveMapRegionSelector"
	minSize     = 100
	maxSize     = 900
	defaultSize = 260
	wheelStep   = 6

	ulwAlpha        = 0x00000002
	acSrcOver       = 0x00
	htTransparent   = ^uintptr(0) - 1
	smoothingAA     = 4
	unitPixel       = 2
	stringAlignNear = 0
	stringAlignMid  = 1
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")
	gdip   = syscall.NewLazyDLL("gdiplus.dll")

	procUpdateLayeredWindow = user32.NewProc("UpdateLayeredWindow")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetCapture          = user32.NewProc("SetCapture")
	procReleaseCapture      = user32.NewProc("ReleaseCapture")

	procGdiplusStartup               = gdip.NewProc("GdiplusStartup")
	procGdiplusShutdown              = gdip.NewProc("GdiplusShutdown")
	procGdipCreateFromHDC            = gdip.NewProc("GdipCreateFromHDC")
	procGdipDeleteGraphics           = gdip.NewProc("GdipDeleteGraphics")
	procGdipSetSmoothingMode         = gdip.NewProc("GdipSetSmoothingMode")
	procGdipGraphicsClear            = gdip.NewProc("GdipGraphicsClear")
	procGdipCreateSolidFill          = gdip.NewProc("GdipCreateSolidFill")
	procGdipDeleteBrush              = gdip.NewProc("GdipDeleteBrush")
	procGdipCreatePen1               = gdip.NewProc("GdipCreatePen1")
	procGdipDeletePen                = gdip.NewProc("GdipDeletePen")
	procGdipFillEllipseI             = gdip.NewProc("GdipFillEllipseI")
	procGdipDrawEllipseI             = gdip.NewProc("GdipDrawEllipseI")
	procGdipCreateFontFamilyFromName = gdip.NewProc("GdipCreateFontFamilyFromName")
	procGdipDeleteFontFamily         = gdip.NewProc("GdipDeleteFontFamily")
	procGdipCreateFont               = gdip.NewProc("GdipCreateFont")
	procGdipDeleteFont               = gdip.NewProc("GdipDeleteFont")
	procGdipCreateStringFormat       = gdip.NewProc("GdipCreateStringFormat")
	procGdipDeleteStringFormat       = gdip.NewProc("GdipDeleteStringFormat")
	procGdipSetStringFormatAlign     = gdip.NewProc("GdipSetStringFormatAlign")
	procGdipSetStringFormatLineAlign = gdip.NewProc("GdipSetStringFormatLineAlign")
	procGdipDrawString               = gdip.NewProc("GdipDrawString")

	current    *selector
	classOnce  sync.Once
	gdiplusTok uintptr
	gdiplusErr error
)

type gdiplusStartupInput struct {
	GdiplusVersion           uint32
	DebugEventCallback       uintptr
	SuppressBackgroundThread int32
	SuppressExternalCodecs   int32
}

type gdiplusStartupOutput struct {
	NotificationHook   uintptr
	NotificationUnhook uintptr
}

type rectF struct {
	X, Y, Width, Height float32
}

type selector struct {
	hwnd       win.HWND
	size       int
	clientRect procwin.Rect
	result     appconfig.MinimapRegion
	confirmed  bool

	dragging bool
	dragX    int
	dragY    int
	winX     int
	winY     int
}

func Select(ctx context.Context, clientRect procwin.Rect, initial appconfig.MinimapRegion) (appconfig.MinimapRegion, bool, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := ensureGDIPlus(); err != nil {
		return appconfig.MinimapRegion{}, false, err
	}

	size := initial.Size
	if size <= 0 {
		size = defaultSize
	}
	size = clamp(size, minSize, maxSize)

	centerX := clientRect.X + clientRect.W/2
	centerY := clientRect.Y + clientRect.H/2
	if initial.Valid() {
		centerX = clientRect.X + initial.X + initial.Size/2
		centerY = clientRect.Y + initial.Y + initial.Size/2
	}

	s := &selector{size: size, clientRect: clientRect}
	current = s
	defer func() { current = nil }()

	if err := registerClass(); err != nil {
		return appconfig.MinimapRegion{}, false, err
	}
	if err := s.create(centerX-size/2, centerY-size/2); err != nil {
		return appconfig.MinimapRegion{}, false, err
	}
	logx.Infof("请在选区窗口中选择小地图范围，回车确认")

	go func() {
		<-ctx.Done()
		if s.hwnd != 0 {
			win.PostMessage(s.hwnd, win.WM_CLOSE, 0, 0)
		}
	}()

	var msg win.MSG
	for {
		ret := win.GetMessage(&msg, 0, 0, 0)
		switch ret {
		case 0:
			return s.result, s.confirmed, nil
		case -1:
			return appconfig.MinimapRegion{}, false, fmt.Errorf("GetMessage 失败")
		default:
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}
	}
}

func (s *selector) create(x, y int) error {
	logx.Infof("选区窗口开始创建: x=%d y=%d size=%d exStyle=0x%x", x, y, s.size, uintptr(win.WS_EX_TOPMOST|win.WS_EX_APPWINDOW|win.WS_EX_LAYERED))
	title := syscall.StringToUTF16Ptr("LiveMap Region Selector")
	hwnd := win.CreateWindowEx(
		win.WS_EX_TOPMOST|win.WS_EX_APPWINDOW|win.WS_EX_LAYERED,
		syscall.StringToUTF16Ptr(className),
		title,
		win.WS_POPUP|win.WS_VISIBLE,
		int32(x),
		int32(y),
		int32(s.size),
		int32(s.size),
		0,
		0,
		0,
		nil,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx 失败")
	}
	s.hwnd = hwnd
	windowicon.SetOverlay(hwnd)
	logx.Infof("选区窗口已创建: hwnd=0x%x", uintptr(hwnd))
	if err := taskbarid.Set(hwnd, taskbarid.OverlayGroupID); err != nil {
		logx.Warnf("设置选区窗口任务栏分组失败: %v", err)
	} else {
		logx.Infof("选区窗口任务栏分组已设置: %s", taskbarid.OverlayGroupID)
	}
	if err := s.render(); err != nil {
		logx.Warnf("选区窗口首次渲染失败: %v", err)
		win.DestroyWindow(hwnd)
		s.hwnd = 0
		return err
	}
	win.ShowWindow(hwnd, win.SW_SHOW)
	win.UpdateWindow(hwnd)
	logx.Infof("选区窗口已显示: hwnd=0x%x", uintptr(hwnd))
	return nil
}

func (s *selector) resize(delta int) {
	var rect win.RECT
	win.GetWindowRect(s.hwnd, &rect)
	old := s.size
	s.size = clamp(s.size+delta, minSize, maxSize)
	if old == s.size {
		return
	}
	cx := int(rect.Left) + old/2
	cy := int(rect.Top) + old/2
	win.SetWindowPos(s.hwnd, 0, int32(cx-s.size/2), int32(cy-s.size/2), int32(s.size), int32(s.size), win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	if err := s.render(); err != nil {
		logx.Errorf("选区窗口重绘失败: %v", err)
	}
}

func (s *selector) confirm() {
	var rect win.RECT
	win.GetWindowRect(s.hwnd, &rect)
	s.result = appconfig.MinimapRegion{
		X:    int(rect.Left) - s.clientRect.X,
		Y:    int(rect.Top) - s.clientRect.Y,
		Size: s.size,
	}
	s.confirmed = true
	logx.Infof("已选择小地图范围: x=%d y=%d size=%d", s.result.X, s.result.Y, s.result.Size)
	win.DestroyWindow(s.hwnd)
}

func (s *selector) beginDrag() {
	var pt win.POINT
	if !getCursorPos(&pt) {
		return
	}
	var rect win.RECT
	win.GetWindowRect(s.hwnd, &rect)
	s.dragging = true
	s.dragX = int(pt.X)
	s.dragY = int(pt.Y)
	s.winX = int(rect.Left)
	s.winY = int(rect.Top)
	procSetCapture.Call(uintptr(s.hwnd))
}

func (s *selector) moveDrag() {
	if !s.dragging {
		return
	}
	var pt win.POINT
	if !getCursorPos(&pt) {
		return
	}
	x := s.winX + int(pt.X) - s.dragX
	y := s.winY + int(pt.Y) - s.dragY
	win.SetWindowPos(s.hwnd, 0, int32(x), int32(y), 0, 0, win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE)
}

func (s *selector) endDrag() {
	if !s.dragging {
		return
	}
	s.dragging = false
	procReleaseCapture.Call()
}

func (s *selector) render() error {
	screen := win.GetDC(0)
	if screen == 0 {
		return fmt.Errorf("GetDC 失败")
	}
	defer win.ReleaseDC(0, screen)

	memDC := win.CreateCompatibleDC(screen)
	if memDC == 0 {
		return fmt.Errorf("CreateCompatibleDC 失败")
	}
	defer win.DeleteDC(memDC)

	var bits unsafe.Pointer
	bmi := win.BITMAPINFOHEADER{
		BiSize:        uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})),
		BiWidth:       int32(s.size),
		BiHeight:      -int32(s.size),
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: win.BI_RGB,
	}
	bitmap := win.CreateDIBSection(screen, &bmi, win.DIB_RGB_COLORS, &bits, 0, 0)
	if bitmap == 0 || bits == nil {
		return fmt.Errorf("CreateDIBSection 失败")
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))
	oldBitmap := win.SelectObject(memDC, win.HGDIOBJ(bitmap))
	if oldBitmap != 0 {
		defer win.SelectObject(memDC, oldBitmap)
	}

	var graphics uintptr
	if status, _, _ := procGdipCreateFromHDC.Call(uintptr(memDC), uintptr(unsafe.Pointer(&graphics))); status != 0 || graphics == 0 {
		return fmt.Errorf("GdipCreateFromHDC 失败: status=%d", status)
	}
	defer procGdipDeleteGraphics.Call(graphics)
	procGdipSetSmoothingMode.Call(graphics, smoothingAA)
	procGdipGraphicsClear.Call(graphics, 0x00000000)

	drawSelectorGraphics(graphics, s.size)

	var rect win.RECT
	win.GetWindowRect(s.hwnd, &rect)
	dstPt := win.POINT{X: rect.Left, Y: rect.Top}
	srcPt := win.POINT{}
	size := win.SIZE{CX: int32(s.size), CY: int32(s.size)}
	blend := win.BLENDFUNCTION{
		BlendOp:             acSrcOver,
		SourceConstantAlpha: 255,
		AlphaFormat:         win.AC_SRC_ALPHA,
	}
	ok, _, callErr := procUpdateLayeredWindow.Call(
		uintptr(s.hwnd),
		uintptr(screen),
		uintptr(unsafe.Pointer(&dstPt)),
		uintptr(unsafe.Pointer(&size)),
		uintptr(memDC),
		uintptr(unsafe.Pointer(&srcPt)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		ulwAlpha,
	)
	if ok == 0 {
		return fmt.Errorf("UpdateLayeredWindow 失败: %v", callErr)
	}
	logx.Infof("选区窗口 layered 已提交: hwnd=0x%x pos=%d,%d size=%dx%d", uintptr(s.hwnd), rect.Left, rect.Top, s.size, s.size)
	return nil
}

func drawSelectorGraphics(graphics uintptr, size int) {
	margin := 5
	diameter := max(1, size-margin*2)
	bg := createSolidBrush(0x9A09121F)
	if bg != 0 {
		procGdipFillEllipseI.Call(graphics, bg, uintptr(margin), uintptr(margin), uintptr(diameter), uintptr(diameter))
		procGdipDeleteBrush.Call(bg)
	}

	glow := createPen(0x66FFFFFF, 5)
	if glow != 0 {
		procGdipDrawEllipseI.Call(graphics, glow, uintptr(margin+1), uintptr(margin+1), uintptr(diameter-2), uintptr(diameter-2))
		procGdipDeletePen.Call(glow)
	}
	border := createPen(0xF5FFFFFF, 2)
	if border != 0 {
		procGdipDrawEllipseI.Call(graphics, border, uintptr(margin), uintptr(margin), uintptr(diameter), uintptr(diameter))
		procGdipDeletePen.Call(border)
	}

	titleLines := splitEvenly("选取小地图范围", titleLineCount(size))
	title := strings.Join(titleLines, "\n")
	titleSize := float32(clamp(size/11, 13, 28))
	drawCenteredText(graphics, title, titleSize, 1, 0xFFFFFFFF, rectF{
		X:      float32(size) * 0.18,
		Y:      float32(size) * 0.28,
		Width:  float32(size) * 0.64,
		Height: float32(size) * 0.30,
	})

	if size > 150 {
		bodySize := float32(clamp(size/20, 9, 16))
		drawCenteredText(graphics, "拖动移动   滚轮缩放\n回车确认   Esc取消", bodySize, 0, 0xD9FFFFFF, rectF{
			X:      float32(size) * 0.12,
			Y:      float32(size) * 0.53,
			Width:  float32(size) * 0.76,
			Height: float32(size) * 0.24,
		})
	}
}

func drawCenteredText(graphics uintptr, text string, emSize float32, style int, argb uint32, layout rectF) {
	family := createFontFamily("Microsoft YaHei UI")
	if family == 0 {
		return
	}
	defer procGdipDeleteFontFamily.Call(family)
	var font uintptr
	if status, _, _ := procGdipCreateFont.Call(family, uintptr(math.Float32bits(emSize)), uintptr(style), unitPixel, uintptr(unsafe.Pointer(&font))); status != 0 || font == 0 {
		return
	}
	defer procGdipDeleteFont.Call(font)

	format := createCenteredStringFormat()
	if format != 0 {
		defer procGdipDeleteStringFormat.Call(format)
	}
	brush := createSolidBrush(argb)
	if brush == 0 {
		return
	}
	defer procGdipDeleteBrush.Call(brush)

	ptr := syscall.StringToUTF16Ptr(text)
	procGdipDrawString.Call(
		graphics,
		uintptr(unsafe.Pointer(ptr)),
		uintptr(^uint32(0)),
		font,
		uintptr(unsafe.Pointer(&layout)),
		format,
		brush,
	)
}

func registerClass() error {
	var err error
	classOnce.Do(func() {
		wndClass := win.WNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
			LpfnWndProc:   syscall.NewCallback(wndProc),
			HInstance:     0,
			HCursor:       win.LoadCursor(0, (*uint16)(unsafe.Pointer(uintptr(win.IDC_ARROW)))),
			HbrBackground: 0,
			LpszClassName: syscall.StringToUTF16Ptr(className),
		}
		if win.RegisterClassEx(&wndClass) == 0 {
			err = fmt.Errorf("RegisterClassEx 失败")
		}
	})
	return err
}

func wndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	if current == nil || hwnd != current.hwnd {
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	switch msg {
	case win.WM_NCHITTEST:
		x := int(int16(lParam & 0xffff))
		y := int(int16((lParam >> 16) & 0xffff))
		if !current.hitTestScreen(x, y) {
			return htTransparent
		}
		return win.HTCLIENT
	case win.WM_MOUSEWHEEL:
		delta := int(int16((wParam >> 16) & 0xffff))
		if delta > 0 {
			current.resize(wheelStep)
		} else {
			current.resize(-wheelStep)
		}
		return 0
	case win.WM_LBUTTONDOWN:
		current.beginDrag()
		return 0
	case win.WM_MOUSEMOVE:
		current.moveDrag()
		return 0
	case win.WM_LBUTTONUP:
		current.endDrag()
		return 0
	case win.WM_KEYDOWN:
		if wParam == win.VK_RETURN {
			current.confirm()
			return 0
		}
		if wParam == win.VK_ESCAPE {
			win.DestroyWindow(hwnd)
			return 0
		}
	case win.WM_DESTROY:
		current.endDrag()
		win.PostQuitMessage(0)
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

func (s *selector) hitTestScreen(screenX, screenY int) bool {
	var rect win.RECT
	if !win.GetWindowRect(s.hwnd, &rect) {
		return false
	}
	x := screenX - int(rect.Left)
	y := screenY - int(rect.Top)
	r := float64(s.size) / 2
	dx := float64(x) - r
	dy := float64(y) - r
	return dx*dx+dy*dy <= r*r
}

func ensureGDIPlus() error {
	if gdiplusTok != 0 || gdiplusErr != nil {
		return gdiplusErr
	}
	input := gdiplusStartupInput{GdiplusVersion: 1}
	var output gdiplusStartupOutput
	status, _, _ := procGdiplusStartup.Call(uintptr(unsafe.Pointer(&gdiplusTok)), uintptr(unsafe.Pointer(&input)), uintptr(unsafe.Pointer(&output)))
	if status != 0 {
		gdiplusErr = fmt.Errorf("GdiplusStartup 失败: status=%d", status)
	}
	return gdiplusErr
}

func createSolidBrush(argb uint32) uintptr {
	var brush uintptr
	status, _, _ := procGdipCreateSolidFill.Call(uintptr(argb), uintptr(unsafe.Pointer(&brush)))
	if status != 0 {
		return 0
	}
	return brush
}

func createPen(argb uint32, width float32) uintptr {
	var pen uintptr
	status, _, _ := procGdipCreatePen1.Call(uintptr(argb), uintptr(math.Float32bits(width)), unitPixel, uintptr(unsafe.Pointer(&pen)))
	if status != 0 {
		return 0
	}
	return pen
}

func createFontFamily(name string) uintptr {
	var family uintptr
	ptr := syscall.StringToUTF16Ptr(name)
	status, _, _ := procGdipCreateFontFamilyFromName.Call(uintptr(unsafe.Pointer(ptr)), 0, uintptr(unsafe.Pointer(&family)))
	if status != 0 {
		return 0
	}
	return family
}

func createCenteredStringFormat() uintptr {
	var format uintptr
	status, _, _ := procGdipCreateStringFormat.Call(0, 0, uintptr(unsafe.Pointer(&format)))
	if status != 0 || format == 0 {
		return 0
	}
	procGdipSetStringFormatAlign.Call(format, stringAlignMid)
	procGdipSetStringFormatLineAlign.Call(format, stringAlignMid)
	return format
}

func getCursorPos(pt *win.POINT) bool {
	ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(pt)))
	return ok != 0
}

func titleLineCount(size int) int {
	switch {
	case size <= 120:
		return 3
	case size <= 170:
		return 2
	default:
		return 1
	}
}

func splitEvenly(text string, lines int) []string {
	runes := []rune(text)
	out := make([]string, 0, lines)
	start := 0
	for i := 0; i < lines; i++ {
		remainingChars := len(runes) - start
		remainingLines := lines - i
		count := (remainingChars + remainingLines - 1) / remainingLines
		out = append(out, string(runes[start:start+count]))
		start += count
	}
	return out
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
