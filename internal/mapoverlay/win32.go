package mapoverlay

import (
	"fmt"
	"github.com/lxn/win"
	"github.com/mascotl/livemap/internal/logx"
	"github.com/mascotl/livemap/internal/taskbarid"
	"github.com/mascotl/livemap/internal/windowicon"
	"image"
	"syscall"
	"time"
	"unsafe"
)

func (w *Window) create() error {
	exStyle := uint32(win.WS_EX_APPWINDOW | win.WS_EX_LAYERED)
	if w.topmost {
		exStyle |= uint32(win.WS_EX_TOPMOST)
	}
	logx.Infof("实时地图窗口开始创建: width=%d height=%d exStyle=0x%x topmost=%v", w.opts.Width, w.opts.Height, exStyle, w.topmost)
	hwnd := win.CreateWindowEx(
		exStyle,
		syscall.StringToUTF16Ptr(className),
		syscall.StringToUTF16Ptr("LiveMap Overlay"),
		win.WS_POPUP|win.WS_THICKFRAME|win.WS_VISIBLE,
		win.CW_USEDEFAULT,
		win.CW_USEDEFAULT,
		int32(w.opts.Width),
		int32(w.opts.Height),
		0,
		0,
		0,
		nil,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx 失败")
	}
	w.hwnd = hwnd
	logx.Infof("实时地图窗口已创建: hwnd=0x%x", uintptr(hwnd))
	windowicon.SetOverlay(hwnd)
	w.applyOpacity()
	w.applySystemShadow()
	if err := taskbarid.Set(hwnd, taskbarid.OverlayGroupID); err != nil {
		logx.Warnf("设置实时地图任务栏分组失败: %v", err)
	} else {
		logx.Infof("实时地图任务栏分组已设置: %s", taskbarid.OverlayGroupID)
	}

	r, err := newRenderer(hwnd, w.opts)
	if err != nil {
		logx.Warnf("实时地图 renderer 创建失败: %v", err)
		win.DestroyWindow(hwnd)
		return err
	}
	w.renderer = r
	win.SetTimer(hwnd, hoverTimerID, hoverTimerMS, 0)
	win.ShowWindow(hwnd, win.SW_SHOW)
	win.UpdateWindow(hwnd)
	w.ensureFrameTimer()
	logx.Infof("实时地图窗口已显示并启动绘制计时器: hwnd=0x%x", uintptr(hwnd))
	return nil
}

func registerClass() error {
	var err error
	once.Do(func() {
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
	case win.WM_NCCALCSIZE:
		if wParam != 0 {
			return 0
		}
	case win.WM_PAINT:
		current.paint()
		return 0
	case wmUpdate:
		current.applyPublishedUpdate()
		current.ensureFrameTimer()
		current.requestPaint()
		return 0
	case wmSave:
		current.flushSettings()
		return 0
	case wmResource:
		current.applyResourceUpdate()
		current.ensureFrameTimer()
		current.requestPaint()
		return 0
	case win.WM_MOUSEACTIVATE:
		return maNoActivate
	case win.WM_MOUSEMOVE:
		current.onMouseMove(wParam, lParam)
		return 0
	case win.WM_MOUSELEAVE:
		current.hover = false
		current.hoverButton = ""
		current.tracking = false
		current.ensureFrameTimer()
		return 0
	case win.WM_LBUTTONDOWN:
		current.onLeftDown(lParam)
		return 0
	case win.WM_LBUTTONUP:
		current.onLeftUp()
		return 0
	case wmMouseWheel:
		current.onMouseWheel(wParam, lParam)
		return 0
	case win.WM_NCHITTEST:
		return current.hitTest(lParam)
	case wmSetCursor:
		if current.updateCursor() {
			return 1
		}
	case win.WM_SIZE:
		current.onSize()
		return 0
	case win.WM_TIMER:
		switch wParam {
		case hoverTimerID:
			current.pollHover()
		case frameTimerID:
			current.onFrameTimer()
		}
		return 0
	case win.WM_HOTKEY:
		if wParam == hotkeyUnlockID {
			current.unlock()
			return 0
		}
	case win.WM_DESTROY:
		current.flushSettings()
		current.unregisterUnlockHotkey()
		win.KillTimer(hwnd, hoverTimerID)
		win.KillTimer(hwnd, frameTimerID)
		if current.renderer != nil {
			current.renderer.release()
			current.renderer = nil
		}
		win.PostQuitMessage(0)
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

func (w *Window) paint() {
	var ps win.PAINTSTRUCT
	hdc := win.BeginPaint(w.hwnd, &ps)
	defer win.EndPaint(w.hwnd, &ps)
	if w.renderer == nil {
		return
	}
	w.mu.RLock()
	opts := w.opts
	update := w.update
	w.mu.RUnlock()
	if err := w.renderer.draw(w, opts, update); err != nil {
		logx.Warnf("实时地图 Direct2D 绘制失败: %v", err)
	}
	if w.showLog {
		w.renderer.drawGdiLogs(hdc, w, update, clientSize(w.hwnd))
	}
}

func (w *Window) requestPaint() {
	if w.hwnd != 0 {
		win.InvalidateRect(w.hwnd, nil, false)
	}
}

func (w *Window) ensureFrameTimer() {
	if w.frameActive || w.hwnd == 0 {
		return
	}
	w.frameActive = true
	w.lastFrame = time.Now()
	win.SetTimer(w.hwnd, frameTimerID, frameTimerMS, 0)
}

func (w *Window) onFrameTimer() {
	now := time.Now()
	dt := now.Sub(w.lastFrame).Seconds()
	if dt <= 0 || dt > 0.25 {
		dt = float64(frameTimerMS) / 1000
	}
	w.lastFrame = now
	needs := w.stepAnimation(dt, now)
	w.requestPaint()
	if !needs {
		win.KillTimer(w.hwnd, frameTimerID)
		w.frameActive = false
	}
}

func (w *Window) onMouseMove(wParam, lParam uintptr) {
	if w.locked {
		return
	}
	p := image.Pt(int(int16(uint16(lParam))), int(int16(uint16(lParam>>16))))
	if w.draggingAlpha {
		w.setOpacityFromPoint(p)
		return
	}
	if w.draggingMap {
		dx := p.X - w.lastMouse.X
		dy := p.Y - w.lastMouse.Y
		w.panX += float64(dx)
		w.panY += float64(dy)
		w.lastMouse = p
		w.ensureFrameTimer()
		return
	}
	wasHover := w.hover
	oldButton := w.hoverButton
	w.hover = true
	w.lastMouse = p
	w.hoverButton = ""
	if selectedMapPath(w.resources) == "" && p.In(noMapImportRect(clientSize(w.hwnd))) {
		w.hoverButton = "import-map"
	}
	if w.toolbarAlpha > 0.01 || wasHover {
		for _, b := range w.toolbarButtons(clientSize(w.hwnd)) {
			if p.In(b.rect) {
				w.hoverButton = b.id
				break
			}
		}
		if w.menuMode != "" {
			if w.menuMode == "alpha" {
				if p.In(alphaSliderRect(clientSize(w.hwnd))) {
					w.hoverButton = "menu:alpha"
				}
			} else {
				box := resourceMenuRect(w, clientSize(w.hwnd))
				for i, item := range resourceMenuItems(w) {
					if p.In(resourceMenuItemRect(box, i)) {
						w.hoverButton = "menu:" + item.key
						break
					}
				}
			}
		}
	}
	if !w.tracking {
		var tme win.TRACKMOUSEEVENT
		tme.CbSize = uint32(unsafe.Sizeof(tme))
		tme.DwFlags = win.TME_LEAVE
		tme.HwndTrack = w.hwnd
		win.TrackMouseEvent(&tme)
		w.tracking = true
	}
	if !wasHover || oldButton != w.hoverButton {
		w.ensureFrameTimer()
	}
}

func (w *Window) onLeftDown(lParam uintptr) {
	if w.locked {
		return
	}
	w.bringToFront()
	size := clientSize(w.hwnd)
	p := image.Pt(int(int16(uint16(lParam))), int(int16(uint16(lParam>>16))))
	w.lastMouse = p
	if w.menuMode == "alpha" && p.In(alphaSliderRect(size)) {
		w.pressed = "menu:alpha"
		w.draggingAlpha = true
		win.SetCapture(w.hwnd)
		w.setOpacityFromPoint(p)
		w.ensureFrameTimer()
		return
	}
	if w.menuMode != "" {
		if w.handleMenuClick(p, size) {
			w.ensureFrameTimer()
			return
		}
		w.menuMode = ""
	}
	if selectedMapPath(w.resources) == "" && p.In(noMapImportRect(size)) {
		w.pressed = "import-map"
		w.beginMapLoading()
		go func() {
			if w.opts.OnImportMap == nil {
				return
			}
			state, err := w.opts.OnImportMap()
			if err != nil {
				logx.Warnf("导入地图失败: %v", err)
				return
			}
			w.UpdateResources(state)
		}()
		w.ensureFrameTimer()
		return
	}
	for _, b := range w.toolbarButtons(size) {
		if !p.In(b.rect) {
			continue
		}
		w.pressed = b.id
		switch b.id {
		case "move":
			win.ReleaseCapture()
			win.SendMessage(w.hwnd, win.WM_NCLBUTTONDOWN, win.HTCAPTION, 0)
		case "pin":
			w.topmost = !w.topmost
			after := win.HWND_NOTOPMOST
			if w.topmost {
				after = win.HWND_TOPMOST
			}
			win.SetWindowPos(w.hwnd, after, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
			w.scheduleSettingsSave()
		case "alpha":
			w.menuMode = "alpha"
		case "map":
			w.menuMode = "map"
		case "pins":
			w.menuMode = "pins"
		case "log":
			w.showLog = !w.showLog
			w.scheduleSettingsSave()
		case "lock":
			w.lock()
		case "close":
			if w.opts.OnCloseRequested != nil {
				go w.opts.OnCloseRequested()
			}
		}
		w.ensureFrameTimer()
		return
	}
	if p.In(mapRect(size)) {
		w.draggingMap = true
		win.SetCapture(w.hwnd)
	}
}

func (w *Window) onLeftUp() {
	if w.draggingMap {
		w.lastDragEnd = time.Now()
	}
	w.draggingMap = false
	w.draggingAlpha = false
	w.pressed = ""
	win.ReleaseCapture()
	w.ensureFrameTimer()
}

func (w *Window) handleMenuClick(p image.Point, size image.Point) bool {
	if w.menuMode == "alpha" {
		return p.In(alphaMenuRect(size))
	}
	box := resourceMenuRect(w, size)
	for i, item := range resourceMenuItems(w) {
		if !p.In(resourceMenuItemRect(box, i)) {
			continue
		}
		if item.disabled {
			return true
		}
		mode := w.menuMode
		w.pressed = "menu:" + item.key
		if item.importAction {
			w.menuMode = ""
			if mode == "map" {
				w.beginMapLoading()
			}
			go func() {
				var state ResourceState
				var err error
				if mode == "map" && w.opts.OnImportMap != nil {
					state, err = w.opts.OnImportMap()
				} else if mode == "pins" && w.opts.OnImportPins != nil {
					state, err = w.opts.OnImportPins()
				}
				if err != nil {
					logx.Warnf("导入资源失败: %v", err)
					return
				}
				w.UpdateResources(state)
			}()
			return true
		}
		w.menuMode = ""
		go func(path string) {
			var state ResourceState
			var err error
			if mode == "map" && w.opts.OnSelectMap != nil {
				state, err = w.opts.OnSelectMap(path)
			} else if mode == "pins" && w.opts.OnTogglePins != nil {
				state, err = w.opts.OnTogglePins(path)
			}
			if err != nil {
				logx.Warnf("更新资源选择失败: %v", err)
				return
			}
			w.UpdateResources(state)
		}(item.key)
		return true
	}
	return false
}

func (w *Window) onMouseWheel(wParam, lParam uintptr) {
	if w.locked {
		return
	}
	var wr win.RECT
	if !win.GetWindowRect(w.hwnd, &wr) {
		return
	}
	x := int(int16(uint16(lParam))) - int(wr.Left)
	y := int(int16(uint16(lParam>>16))) - int(wr.Top)
	if !image.Pt(x, y).In(mapRect(clientSize(w.hwnd))) {
		return
	}
	delta := int16(uint16(wParam >> 16))
	factor := 1.1
	if delta < 0 {
		factor = 1 / factor
	}
	old := w.targetZoom
	w.targetZoom = clampFloat(w.targetZoom*factor, 0.5, 4.0)
	if old != w.targetZoom {
		w.scheduleSettingsSave()
		w.ensureFrameTimer()
	}
}

func (w *Window) applyResourceUpdate() {
	w.mu.RLock()
	path := w.opts.WorldMapPath
	resources := w.resources
	w.mu.RUnlock()
	if w.renderer == nil {
		return
	}
	if path != "" && path != w.renderer.mapPath {
		if err := w.renderer.loadMap(path); err != nil {
			logx.Warnf("加载实时地图资源失败: %v", err)
		}
	}
	if path == "" && w.renderer.mapBitmap != nil {
		w.renderer.mapBitmap.release()
		w.renderer.mapBitmap = nil
		w.renderer.mapPath = ""
		w.renderer.mapWidth = 0
		w.renderer.mapHeight = 0
	}
	w.renderer.loadPinResources(resources.Categories)
}

func (w *Window) setOpacityFromPoint(p image.Point) {
	r := alphaSliderRect(clientSize(w.hwnd))
	t := clamp01(float64(p.X-r.Min.X) / float64(max(1, r.Dx())))
	w.opacity = byte(minOpacity + int(t*float64(maxOpacity-minOpacity)))
	w.applyOpacity()
	w.scheduleSettingsSave()
	w.ensureFrameTimer()
}

func (w *Window) hitTest(lParam uintptr) uintptr {
	if w.locked {
		return uintptr(^uintptr(0))
	}
	var wr win.RECT
	if !win.GetWindowRect(w.hwnd, &wr) {
		return win.HTCLIENT
	}
	x := int(int16(uint16(lParam))) - int(wr.Left)
	y := int(int16(uint16(lParam>>16))) - int(wr.Top)
	width := int(wr.Right - wr.Left)
	height := int(wr.Bottom - wr.Top)
	left := x < edgeGrip
	right := x >= width-edgeGrip
	top := y < edgeGrip
	bottom := y >= height-edgeGrip
	switch {
	case top && left:
		return win.HTTOPLEFT
	case top && right:
		return win.HTTOPRIGHT
	case bottom && left:
		return win.HTBOTTOMLEFT
	case bottom && right:
		return win.HTBOTTOMRIGHT
	case left:
		return win.HTLEFT
	case right:
		return win.HTRIGHT
	case top:
		return win.HTTOP
	case bottom:
		return win.HTBOTTOM
	default:
		return win.HTCLIENT
	}
}

func (w *Window) updateCursor() bool {
	if w.locked {
		win.SetCursor(0)
		return true
	}
	if w.draggingMap {
		win.SetCursor(win.LoadCursor(0, (*uint16)(unsafe.Pointer(uintptr(win.IDC_SIZEALL)))))
		return true
	}
	return false
}

func (w *Window) pollHover() {
	if w.locked || !w.hover {
		return
	}
	var p win.POINT
	if !win.GetCursorPos(&p) {
		return
	}
	var wr win.RECT
	if !win.GetWindowRect(w.hwnd, &wr) {
		return
	}
	if p.X < wr.Left || p.X >= wr.Right || p.Y < wr.Top || p.Y >= wr.Bottom {
		w.hover = false
		w.hoverButton = ""
		w.tracking = false
		w.ensureFrameTimer()
	}
}

func (w *Window) onSize() {
	if w.renderer != nil {
		_ = w.renderer.resize(clientSize(w.hwnd))
	}
	w.scheduleSettingsSave()
	w.requestPaint()
}

func (w *Window) scheduleSettingsSave() {
	if w.opts.OnSettingsChanged == nil || w.hwnd == 0 {
		return
	}
	if w.saveTimer != nil {
		w.saveTimer.Stop()
	}
	w.saveTimer = time.AfterFunc(800*time.Millisecond, func() {
		if w.hwnd != 0 {
			win.PostMessage(w.hwnd, wmSave, 0, 0)
		}
	})
}

func (w *Window) flushSettings() {
	if w.saveTimer != nil {
		w.saveTimer.Stop()
		w.saveTimer = nil
	}
	if w.opts.OnSettingsChanged == nil {
		return
	}
	size := clientSize(w.hwnd)
	if size.X <= 0 || size.Y <= 0 {
		size = image.Pt(w.opts.Width, w.opts.Height)
	}
	settings := Settings{
		Width:   size.X,
		Height:  size.Y,
		Zoom:    w.targetZoom,
		Opacity: int(w.opacity),
		Topmost: w.topmost,
		ShowLog: w.showLog,
	}
	go w.opts.OnSettingsChanged(settings)
}

func (w *Window) lock() {
	if w.locked {
		return
	}
	w.locked = true
	w.hover = false
	w.hoverButton = ""
	w.alphaMode = false
	w.menuMode = ""
	w.draggingMap = false
	w.draggingAlpha = false
	w.registerUnlockHotkey()
	w.applyClickThrough(true)
	w.ensureFrameTimer()
}

func (w *Window) unlock() {
	if !w.locked {
		return
	}
	w.locked = false
	w.unregisterUnlockHotkey()
	w.applyClickThrough(false)
	w.ensureFrameTimer()
}

func (w *Window) applyClickThrough(enabled bool) {
	style := win.GetWindowLong(w.hwnd, win.GWL_EXSTYLE)
	if enabled {
		style |= win.WS_EX_TRANSPARENT
	} else {
		style &^= win.WS_EX_TRANSPARENT
	}
	win.SetWindowLong(w.hwnd, win.GWL_EXSTYLE, style)
	win.SetWindowPos(w.hwnd, 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED)
}

func (w *Window) applyOpacity() {
	procSetLayeredWindowAttribute.Call(uintptr(w.hwnd), 0, uintptr(w.opacity), lwaAlpha)
}

func (w *Window) applySystemShadow() {
	policy := int32(dwmNCRenderingEnabled)
	procDwmSetWindowAttribute.Call(uintptr(w.hwnd), dwmNCRenderingPolicy, uintptr(unsafe.Pointer(&policy)), unsafe.Sizeof(policy))
	margins := dwmMargins{Left: 1, Right: 1, Top: 1, Bottom: 1}
	procDwmExtendFrame.Call(uintptr(w.hwnd), uintptr(unsafe.Pointer(&margins)))
	win.SetWindowPos(w.hwnd, 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED)
}

func (w *Window) bringToFront() {
	after := win.HWND_TOP
	if w.topmost {
		after = win.HWND_TOPMOST
	}
	win.SetWindowPos(w.hwnd, after, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE)
}

func (w *Window) registerUnlockHotkey() {
	procRegisterHotKey.Call(uintptr(w.hwnd), hotkeyUnlockID, 0, vkEnd)
}

func (w *Window) unregisterUnlockHotkey() {
	procUnregisterHotKey.Call(uintptr(w.hwnd), hotkeyUnlockID)
}

func (w *Window) toolbarButtons(size image.Point) []buttonHit {
	if (w.toolbarAlpha <= 0.01 && !w.hover) || w.locked {
		return nil
	}
	pill := toolbarRect(size, w.alphaMode)
	x := pill.Min.X + toolbarPadX()
	y := pill.Min.Y + 8
	ids := []buttonHit{
		{id: "move", title: "拖拽移动"},
		{id: "pin", title: "窗口置顶"},
		{id: "map", title: "选择地图"},
		{id: "pins", title: "选择标点文件"},
		{id: "alpha", title: "透明度"},
		{id: "log", title: "显示日志"},
		{id: "lock", title: "锁定穿透，按 End 解锁"},
		{id: "close", title: "停止运行"},
	}
	for i := range ids {
		ids[i].rect = image.Rect(x, y, x+30, y+30)
		x += toolbarButtonStep()
	}
	return ids
}
