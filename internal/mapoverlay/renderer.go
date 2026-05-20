package mapoverlay

import (
	"fmt"

	"github.com/lxn/win"
	"github.com/mascotl/livemap/internal/logx"
	"image"
	"math"
	"syscall"
	"unsafe"
)

func (r *renderer) draw(w *Window, opts Options, update Update) error {
	if r.target == nil {
		return nil
	}
	size := clientSize(w.hwnd)
	if size.X <= 0 || size.Y <= 0 {
		return nil
	}
	r.target.call(48)
	r.target.call(47, uintptr(unsafe.Pointer(&d2dColorF{R: 0.02, G: 0.03, B: 0.05, A: 0.0})))
	r.drawBackground(w, update, size)
	r.drawFrame(size)
	if w.showLog {
		r.drawLogs(w, update, size)
	}
	if w.toolbarAlpha > 0.001 && !w.locked {
		r.drawToolbar(w, size)
	}
	hr := r.target.call(49, 0, 0)
	if failed(hr) {
		r.recreateTarget(size)
		return fmt.Errorf("EndDraw 失败: 0x%x", hr)
	}
	return nil
}

func (r *renderer) recreateTarget(size image.Point) {
	r.releaseDeviceResources()
	if r.target != nil {
		r.target.release()
		r.target = nil
	}
	if err := r.createTarget(size); err != nil {
		logx.Warnf("重建 Direct2D render target 失败: %v", err)
		return
	}
	if err := r.createDeviceResources(); err != nil {
		logx.Warnf("重建 Direct2D 资源失败: %v", err)
	}
	r.loadIcons()
	if r.mapPath != "" {
		if err := r.loadMap(r.mapPath); err != nil {
			logx.Warnf("重建 Direct2D 地图 bitmap 失败: %v", err)
		}
	}
}

func (r *renderer) drawBackground(w *Window, update Update, size image.Point) {
	r.drawWindowShell(size)
	if w.loadingMap {
		r.drawLoading(w, size)
		return
	}
	if r.mapBitmap == nil && selectedMapPath(w.resources) == "" {
		r.drawNoMap(w, size)
		return
	}
	if !w.hasFix || r.mapBitmap == nil {
		r.drawLoading(w, size)
		return
	}
	cx := w.displayX
	cy := w.displayY
	if update.State == "lost" {
		cx = w.lastFoundX
		cy = w.lastFoundY
	}
	dstW := float64(r.mapWidth) * w.zoom
	dstH := float64(r.mapHeight) * w.zoom
	dstX := float64(size.X)/2 + w.panX - cx*w.zoom
	dstY := float64(size.Y)/2 + w.panY - cy*w.zoom
	dst := rectF(dstX, dstY, dstX+dstW, dstY+dstH)
	inset := borderWidth + 6
	clip := rectF(inset, inset, float64(size.X)-inset, float64(size.Y)-inset)
	dst.Left += float32(inset)
	dst.Top += float32(inset)
	dst.Right -= float32(inset)
	dst.Bottom -= float32(inset)
	r.target.call(55, uintptr(unsafe.Pointer(&clip)), d2dAntialiasPerPrimitive)
	r.drawBitmap(r.mapBitmap, dst, 1)
	r.drawPinMarkers(w, dst, inset)
	r.target.call(56)
	px := float64(size.X)/2 + w.panX + (w.displayX-cx)*w.zoom
	py := float64(size.Y)/2 + w.panY + (w.displayY-cy)*w.zoom
	if update.State == "lost" {
		px = float64(size.X)/2 + w.panX
		py = float64(size.Y)/2 + w.panY
	}
	r.drawMarker(px, py, update.State == "lost")
}

func (r *renderer) drawNoMap(w *Window, size image.Point) {
	cx := float64(size.X) / 2
	cy := float64(size.Y)/2 - 2
	text := "请导入地图文件"
	textW := estimateTextWidth(text)
	r.drawText(text, float32(cx-textW/2), float32(cy-25), float32(cx+textW/2+8), float32(cy-1), "text")
	btnRect := noMapImportRect(size)
	brush := "import"
	edge := "importEdge"
	if w.hoverButton == "import-map" {
		brush = "importHover"
		edge = "importEdgeHover"
	}
	if w.pressed == "import-map" {
		brush = "importPress"
	}
	btn := rectFromImage(btnRect)
	r.fillRounded(btn, float64(btnRect.Dy())/2, brush)
	r.drawRounded(btn, float64(btnRect.Dy())/2, edge, 1)
	if icon := r.icons["map-plus"]; icon != nil {
		r.drawBitmap(icon, rectF(float64(btnRect.Min.X+14), float64(btnRect.Min.Y+7), float64(btnRect.Min.X+28), float64(btnRect.Min.Y+21)), 1)
	}
	r.drawSmallText("导入地图", float32(btnRect.Min.X+34), float32(btnRect.Min.Y+6), float32(btnRect.Max.X-10), float32(btnRect.Max.Y), "text")
}

func (r *renderer) drawLoading(w *Window, size image.Point) {
	cx := float64(size.X) / 2
	cy := float64(size.Y) / 2
	r.fillEllipse(cx+1, cy+2, 17, 17, "shadow")
	r.fillEllipse(cx, cy, 15, 15, "button")
	r.drawLoadingIcon(cx, cy, w.loadingRot)
}

func (r *renderer) drawFrame(size image.Point) {
	r.drawInnerShadow(size)
	r.drawWindowBorder(size)
}

func (r *renderer) drawWindowShell(size image.Point) {
	r.fillRounded(rectF(0, 0, float64(size.X), float64(size.Y)), 0, "bg")
}

func (r *renderer) drawWindowBorder(size image.Point) {
	w := float64(size.X)
	h := float64(size.Y)
	bw := borderWidth
	r.fillRounded(rectF(0, 0, w, bw), 0, "border")
	r.fillRounded(rectF(0, h-bw, w, h), 0, "border")
	r.fillRounded(rectF(0, bw, bw, h-bw), 0, "border")
	r.fillRounded(rectF(w-bw, bw, w, h-bw), 0, "border")
}

func (r *renderer) drawInnerShadow(size image.Point) {
	w := float64(size.X)
	h := float64(size.Y)
	edge := borderWidth
	strong := 4.0
	soft := 8.0
	r.fillRounded(rectF(edge, edge, w-edge, edge+soft), 0, "innerShadowSoft")
	r.fillRounded(rectF(edge, edge, edge+soft, h-edge), 0, "innerShadowSoft")
	r.fillRounded(rectF(edge, h-edge-soft, w-edge, h-edge), 0, "innerShadowSoft")
	r.fillRounded(rectF(w-edge-soft, edge, w-edge, h-edge), 0, "innerShadowSoft")
	r.fillRounded(rectF(edge, edge, w-edge, edge+strong), 0, "innerShadow")
	r.fillRounded(rectF(edge, edge, edge+strong, h-edge), 0, "innerShadow")
}

func (r *renderer) drawMarker(x, y float64, lost bool) {
	r.fillEllipse(x+1.5, y+2.5, 13, 13, "shadow")
	r.fillEllipse(x, y, 12.5, 12.5, "markerOuter")
	if lost {
		r.fillEllipse(x, y, 8.5, 8.5, "markerLost")
		return
	}
	r.fillEllipse(x, y, 8.5, 8.5, "marker")
}

func (r *renderer) drawToolbar(w *Window, size image.Point) {
	pill := toolbarRect(size, w.alphaMode)
	alpha := float32(w.toolbarAlpha)
	r.setBrushAlpha("shadow", 0.20*alpha)
	r.fillRounded(offsetRect(rectFromImage(pill), 0, 3), float64(pill.Dy())/2, "shadow")
	r.setBrushAlpha("pill", 0.84*alpha)
	r.fillRounded(rectFromImage(pill), float64(pill.Dy())/2, "pill")
	r.setBrushAlpha("pillEdge", 0.28*alpha)
	r.drawRounded(rectFromImage(pill), float64(pill.Dy())/2, "pillEdge", 1)
	r.setBrushAlpha("shadow", 0.22)
	r.setBrushAlpha("pill", 0.84)
	r.setBrushAlpha("pillEdge", 0.28)

	for _, b := range w.toolbarButtons(size) {
		active := (b.id == "pin" && w.topmost) || (b.id == "log" && w.showLog)
		brush := "button"
		if b.id == w.hoverButton {
			brush = "hover"
		}
		if b.id == w.pressed {
			brush = "button"
		}
		if active {
			brush = "active"
		}
		r.setBrushAlpha(brush, brushBaseAlpha(brush)*alpha)
		r.setBrushAlpha("text", 0.98*alpha)
		r.fillEllipseRect(b.rect, brush)
		r.drawIcon(b.id, b.rect, active, alpha)
	}
	if w.menuMode != "" {
		if w.menuMode == "alpha" {
			r.drawAlphaMenu(w, size)
		} else {
			r.drawResourceMenu(w, size)
		}
	}
	if w.hoverButton != "" {
		for _, b := range w.toolbarButtons(size) {
			if b.id == w.hoverButton {
				r.drawTooltip(b.title, b.rect)
				break
			}
		}
	}
	r.restoreToolbarBrushAlpha()
}

func (r *renderer) restoreToolbarBrushAlpha() {
	for _, name := range []string{"button", "hover", "active", "text", "markerOuter"} {
		r.setBrushAlpha(name, brushBaseAlpha(name))
	}
}

func brushBaseAlpha(name string) float32 {
	switch name {
	case "button":
		return 0.78
	case "hover":
		return 0.86
	case "active":
		return 0.96
	case "markerOuter", "text":
		return 0.98
	default:
		return 1
	}
}

func (r *renderer) drawAlphaMenu(w *Window, size image.Point) {
	alpha := float32(w.toolbarAlpha)
	r.setBrushAlpha("button", brushBaseAlpha("button")*alpha)
	r.setBrushAlpha("active", brushBaseAlpha("active")*alpha)
	r.setBrushAlpha("markerOuter", brushBaseAlpha("markerOuter")*alpha)
	r.setBrushAlpha("text", brushBaseAlpha("text")*alpha)
	box := alphaMenuRect(size)
	r.fillRounded(offsetRect(rectFromImage(box), 0, 3), 10, "shadow")
	r.fillRounded(rectFromImage(box), 10, "pill")
	r.drawRounded(rectFromImage(box), 10, "pillEdge", 1)
	sr := alphaSliderRect(size)
	r.fillRounded(rectFromImage(sr), float64(sr.Dy())/2, "button")
	t := float64(w.opacity-minOpacity) / float64(maxOpacity-minOpacity)
	filled := image.Rect(sr.Min.X, sr.Min.Y, sr.Min.X+int(float64(sr.Dx())*t), sr.Max.Y)
	r.fillRounded(rectFromImage(filled), float64(sr.Dy())/2, "active")
	kx := float64(sr.Min.X) + float64(sr.Dx())*t
	r.fillEllipse(kx, float64(sr.Min.Y+sr.Dy()/2), 6, 6, "markerOuter")
}

func (r *renderer) drawIcon(id string, rect image.Rectangle, active bool, opacity float32) {
	iconID := id
	switch id {
	case "pin":
		if !active {
			iconID = "pin-off"
		}
	case "log":
		if !active {
			iconID = "log-off"
		}
	case "lock":
		iconID = "lock-off"
	}
	if icon := r.icons[iconID]; icon != nil {
		inset := 6
		dst := rectF(float64(rect.Min.X+inset), float64(rect.Min.Y+inset), float64(rect.Max.X-inset), float64(rect.Max.Y-inset))
		r.drawBitmap(icon, dst, opacity)
		return
	}
	cx := float64(rect.Min.X + rect.Dx()/2)
	cy := float64(rect.Min.Y + rect.Dy()/2)
	brush := "text"
	if active {
		brush = "text"
	}
	switch id {
	case "move":
		for x := -5.0; x <= 5; x += 5 {
			for y := -5.0; y <= 5; y += 5 {
				r.fillRounded(rectF(cx+x-1.5, cy+y-1.5, cx+x+1.5, cy+y+1.5), 1, brush)
			}
		}
	case "pin":
		r.drawLine(cx-3, cy-8, cx+7, cy+2, 2, brush)
		r.fillEllipse(cx-4, cy-8, 4, 4, brush)
		r.fillEllipse(cx+7, cy+2, 3, 3, brush)
		r.drawLine(cx+1, cy+5, cx-6, cy+12, 2, brush)
	case "alpha":
		r.drawEllipse(cx, cy, 8.5, 8.5, 2, brush)
		r.fillEllipse(cx-3, cy-3, 4, 4, brush)
	case "log":
		r.drawRounded(rectF(cx-8, cy-9, cx+8, cy+9), 3, brush, 2)
		r.drawLine(cx-4, cy-3, cx+5, cy-3, 2, brush)
		r.drawLine(cx-4, cy+3, cx+3, cy+3, 2, brush)
	case "lock":
		r.drawRounded(rectF(cx-8, cy-1, cx+8, cy+10), 3, brush, 2)
		r.drawEllipse(cx, cy-1, 7, 9, 2, brush)
		r.fillRounded(rectF(cx-9, cy-1, cx+9, cy+8), 2, "button")
		r.drawRounded(rectF(cx-8, cy-1, cx+8, cy+10), 3, brush, 2)
	case "back":
		r.drawLine(cx+5, cy-8, cx-4, cy, 2.3, brush)
		r.drawLine(cx-4, cy, cx+5, cy+8, 2.3, brush)
	case "close":
		r.drawEllipse(cx, cy, 9, 9, 2, brush)
		r.drawLine(cx-4.5, cy-4.5, cx+4.5, cy+4.5, 2.2, brush)
		r.drawLine(cx+4.5, cy-4.5, cx-4.5, cy+4.5, 2.2, brush)
	}
}

func (r *renderer) drawPinMarkers(w *Window, mapDst d2dRectF, inset float64) {
	if len(w.resources.Markers) == 0 {
		return
	}
	valid := make(map[string]bool, len(w.resources.Categories))
	for _, c := range w.resources.Categories {
		valid[c.Name] = true
	}
	for _, marker := range w.resources.Markers {
		x := float64(mapDst.Left) + float64(marker.X)*w.zoom - inset
		y := float64(mapDst.Top) + float64(marker.Y)*w.zoom - inset
		dst := rectF(x-11, y-11, x+11, y+11)
		icon := r.pinIcons[marker.Category]
		if marker.Category == "" || !valid[marker.Category] || icon == nil {
			icon = r.icons["question"]
		}
		if icon != nil {
			r.setBrushAlpha("shadow", 0.34)
			r.fillEllipse(x+2.2, y+3.0, 13.5, 13.5, "shadow")
			r.setBrushAlpha("shadow", 0.22)
			r.fillEllipse(x+1.0, y+1.5, 12.5, 12.5, "shadow")
			r.setBrushAlpha("shadow", 0.22)
			r.fillEllipse(x, y, 12, 12, "pinBg")
			r.drawEllipse(x, y, 12, 12, 2, "pinBorder")
			r.drawBitmap(icon, dst, 1)
		}
	}
}

func (r *renderer) drawResourceMenu(w *Window, size image.Point) {
	box := resourceMenuRect(w, size)
	r.fillRounded(offsetRect(rectFromImage(box), 0, 3), 10, "shadow")
	r.fillRounded(rectFromImage(box), 10, "pill")
	r.drawRounded(rectFromImage(box), 10, "pillEdge", 1)
	items := resourceMenuItems(w)
	for i, item := range items {
		row := resourceMenuItemRect(box, i)
		if !item.disabled && w.hoverButton == "menu:"+item.key {
			brush := "hover"
			if w.pressed == "menu:"+item.key {
				brush = "button"
			}
			r.fillRounded(rectFromImage(row), 6, brush)
		}
		brush := "text"
		if item.disabled {
			brush = "muted"
		}
		if item.importAction {
			textW := estimateTextWidth(item.label)
			start := float64(row.Min.X+row.Dx()/2) - (textW+28)/2
			if icon := r.icons[item.icon]; icon != nil {
				r.drawBitmap(icon, rectF(start, float64(row.Min.Y+5), start+14, float64(row.Min.Y+19)), 1)
			}
			r.drawSmallText(item.label, float32(start+20), float32(row.Min.Y+4), float32(row.Max.X-10), float32(row.Max.Y), brush)
			continue
		}
		r.drawSmallText(item.label, float32(row.Min.X+10), float32(row.Min.Y+4), float32(row.Max.X-30), float32(row.Max.Y), brush)
		if item.checked {
			if icon := r.icons["check"]; icon != nil {
				r.drawBitmap(icon, rectF(float64(row.Max.X-24), float64(row.Min.Y+5), float64(row.Max.X-10), float64(row.Min.Y+19)), 1)
			}
		}
	}
}

func selectedMapPath(state ResourceState) string {
	for _, item := range state.Maps {
		if item.Selected {
			return item.Path
		}
	}
	return ""
}

func (r *renderer) drawLoadingIcon(cx, cy, rot float64) {
	if icon := r.icons["loading"]; icon != nil {
		r.target.call(30, uintptr(unsafe.Pointer(&d2dMatrix3x2F{
			M11: float32(math.Cos(rot)),
			M12: float32(math.Sin(rot)),
			M21: float32(-math.Sin(rot)),
			M22: float32(math.Cos(rot)),
			DX:  float32(cx - cx*math.Cos(rot) + cy*math.Sin(rot)),
			DY:  float32(cy - cx*math.Sin(rot) - cy*math.Cos(rot)),
		})))
		r.drawBitmap(icon, rectF(cx-10, cy-10, cx+10, cy+10), 1)
		r.target.call(30, uintptr(unsafe.Pointer(&d2dMatrix3x2F{M11: 1, M22: 1})))
		return
	}
	for i := 0; i < 9; i++ {
		t := float64(i) / 9
		a := rot + t*math.Pi*1.55
		x := cx + math.Cos(a)*7
		y := cy + math.Sin(a)*7
		r.setBrushAlpha("text", float32(0.25+0.7*t))
		r.fillEllipse(x, y, 1.8, 1.8, "text")
	}
	r.setBrushAlpha("text", 0.98)
}

func (r *renderer) drawTooltip(text string, rect image.Rectangle) {
	w := estimateTextWidth(text) + 16
	x := float64(rect.Min.X+rect.Dx()/2) - w/2
	y := float64(rect.Max.Y + 8)
	bg := rectF(x, y, x+w, y+28)
	r.fillRounded(bg, 7, "logBg")
	r.drawText(text, bg.Left+8, bg.Top+6, bg.Right-8, bg.Bottom, "text")
}

func (r *renderer) drawBitmap(bitmap *comObject, dst d2dRectF, opacity float32) {
	r.target.call(26, bitmap.ptr, uintptr(unsafe.Pointer(&dst)), uintptr(math.Float32bits(opacity)), d2dBitmapInterpLinear, 0)
}

func (r *renderer) drawText(text string, left, top, right, bottom float32, brush string) {
	r.drawTextWithFormat(text, left, top, right, bottom, brush, r.textUI)
}

func (r *renderer) drawSmallText(text string, left, top, right, bottom float32, brush string) {
	format := r.textSmall
	if format == nil {
		format = r.textUI
	}
	r.drawTextWithFormat(text, left, top, right, bottom, brush, format)
}

func (r *renderer) drawLogText(text string, left, top, right, bottom float32, brush string) {
	r.drawTextWithFormat(text, left, top, right, bottom, brush, r.textLog)
}

func (r *renderer) drawTextWithFormat(text string, left, top, right, bottom float32, brush string, format *comObject) {
	if format == nil || text == "" {
		return
	}
	utf := syscall.StringToUTF16(text)
	layout := d2dRectF{Left: left, Top: top, Right: right, Bottom: bottom}
	r.target.call(27, uintptr(unsafe.Pointer(&utf[0])), uintptr(uint32(len(utf)-1)), format.ptr, uintptr(unsafe.Pointer(&layout)), r.brush(brush).ptr, 0, dwriteMeasuringNatural)
}

func (r *renderer) fillRounded(rect d2dRectF, radius float64, brush string) {
	rr := d2dRoundedRect{Rect: rect, RadiusX: float32(radius), RadiusY: float32(radius)}
	r.target.call(19, uintptr(unsafe.Pointer(&rr)), r.brush(brush).ptr)
}

func (r *renderer) drawRounded(rect d2dRectF, radius float64, brush string, width float64) {
	rr := d2dRoundedRect{Rect: rect, RadiusX: float32(radius), RadiusY: float32(radius)}
	r.target.call(18, uintptr(unsafe.Pointer(&rr)), r.brush(brush).ptr, uintptr(math.Float32bits(float32(width))), 0)
}

func (r *renderer) fillEllipse(x, y, rx, ry float64, brush string) {
	e := d2dEllipse{Point: d2dPoint2F{X: float32(x), Y: float32(y)}, RadiusX: float32(rx), RadiusY: float32(ry)}
	r.target.call(21, uintptr(unsafe.Pointer(&e)), r.brush(brush).ptr)
}

func (r *renderer) fillEllipseRect(rect image.Rectangle, brush string) {
	r.fillEllipse(float64(rect.Min.X+rect.Dx()/2), float64(rect.Min.Y+rect.Dy()/2), float64(rect.Dx())/2, float64(rect.Dy())/2, brush)
}

func (r *renderer) drawEllipse(x, y, rx, ry, width float64, brush string) {
	e := d2dEllipse{Point: d2dPoint2F{X: float32(x), Y: float32(y)}, RadiusX: float32(rx), RadiusY: float32(ry)}
	r.target.call(20, uintptr(unsafe.Pointer(&e)), r.brush(brush).ptr, uintptr(math.Float32bits(float32(width))), 0)
}

func (r *renderer) drawLine(x1, y1, x2, y2, width float64, brush string) {
	p1 := d2dPoint2F{X: float32(x1), Y: float32(y1)}
	p2 := d2dPoint2F{X: float32(x2), Y: float32(y2)}
	r.target.call(15, packPoint2F(p1), packPoint2F(p2), r.brush(brush).ptr, uintptr(math.Float32bits(float32(width))), 0)
}

func (r *renderer) setBrushAlpha(name string, alpha float32) {
	if b := r.brushes[name]; b != nil {
		b.call(4, uintptr(math.Float32bits(alpha)))
	}
}

func (r *renderer) brush(name string) *comObject {
	if b := r.brushes[name]; b != nil {
		return b
	}
	return r.brushes["text"]
}

func (r *renderer) releaseDeviceResources() {
	for name, b := range r.brushes {
		if b != nil {
			b.release()
		}
		delete(r.brushes, name)
	}
	for name, icon := range r.icons {
		if icon != nil {
			icon.release()
		}
		delete(r.icons, name)
	}
	for name, icon := range r.pinIcons {
		if icon != nil {
			icon.release()
		}
		delete(r.pinIcons, name)
	}
	if r.textUI != nil {
		r.textUI.release()
		r.textUI = nil
	}
	if r.textSmall != nil {
		r.textSmall.release()
		r.textSmall = nil
	}
	if r.textLog != nil {
		r.textLog.release()
		r.textLog = nil
	}
	if r.logFont != 0 {
		win.DeleteObject(win.HGDIOBJ(r.logFont))
		r.logFont = 0
	}
	if r.mapBitmap != nil {
		r.mapBitmap.release()
		r.mapBitmap = nil
	}
}

func (r *renderer) release() {
	r.releaseDeviceResources()
	for _, obj := range []*comObject{r.target, r.dwrite, r.wic, r.factory} {
		if obj != nil {
			obj.release()
		}
	}
	r.unloadPrivateFonts()
	r.target = nil
	r.dwrite = nil
	r.wic = nil
	r.factory = nil
}
