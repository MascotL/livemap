package mapoverlay

import (
	"fmt"
	"github.com/lxn/win"
	"image"
	"math"
	"strings"
	"syscall"
)

func (r *renderer) drawLogs(w *Window, update Update, size image.Point) {
	leftLines := []string{}
	state := strings.ToLower(update.State)
	if state == "lost" {
		leftLines = append(leftLines, "丢失目标")
	} else {
		elapsed := clampInt(int(update.ElapsedMS), 0, 999)
		leftLines = append(leftLines, fmt.Sprintf("%dms", elapsed))
	}
	// leftLines = append(leftLines, fmt.Sprintf("x=%d  y=%d  score=%.3f", update.X, update.Y, update.Score))
	leftLines = append(leftLines, fmt.Sprintf("x=%d", update.X))
	leftLines = append(leftLines, fmt.Sprintf("y=%d", update.Y))
	leftLines = append(leftLines, fmt.Sprintf("score=%.3f", update.Score))

	leftW := 0.0
	for _, line := range leftLines {
		leftW = math.Max(leftW, estimateTextWidth(line))
	}
	leftRect := rectF(12, float64(size.Y-10-len(leftLines)*22-16), 12+leftW+20, float64(size.Y-10))
	r.fillRounded(leftRect, 8, "logBg")
	y := leftRect.Top + 8
	firstBrush := "green"
	elapsed := clampInt(int(update.ElapsedMS), 0, 999)
	if state == "lost" {
		firstBrush = "red"
	} else if elapsed > 500 {
		firstBrush = "red"
	} else if elapsed > 200 {
		firstBrush = "yellow"
	}
	for i, line := range leftLines {
		brush := "text"
		if i == 0 {
			brush = firstBrush
		}
		_ = brush
		_ = line
		y += 22
	}

	rightLines := make([]string, 0, 3)
	if w.locked {
		rightLines = append(rightLines, "已锁定")
	}
	rightLines = append(rightLines,
		fmt.Sprintf("Zoom %.0f%%", w.targetZoom*100),
		fmt.Sprintf("Opacity %.0f%%", float64(w.opacity)/255*100),
	)
	rightW := 20.0
	for _, line := range rightLines {
		rightW = math.Max(rightW, monoTextWidth(line)+20)
	}
	rightRect := rectF(float64(size.X)-rightW-12, float64(size.Y-10-len(rightLines)*22-16), float64(size.X-12), float64(size.Y-10))
	r.fillRounded(rightRect, 8, "logBg")
	y = rightRect.Top + 8
	for _, line := range rightLines {
		_ = line
		y += 22
	}
}

func monoTextWidth(text string) float64 {
	width := 0.0
	for _, r := range text {
		if r <= 0x7f {
			width += 8.45
		} else {
			width += 14
		}
	}
	return math.Max(18, width)
}

func (r *renderer) drawGdiLogs(hdc win.HDC, w *Window, update Update, size image.Point) {
	if hdc == 0 || size.X <= 0 || size.Y <= 0 {
		return
	}
	if r.logFont != 0 {
		old := win.SelectObject(hdc, win.HGDIOBJ(r.logFont))
		defer win.SelectObject(hdc, old)
	}
	win.SetBkMode(hdc, win.TRANSPARENT)

	leftLines := []string{}
	state := strings.ToLower(update.State)
	if state == "lost" {
		leftLines = append(leftLines, "丢失目标")
	} else {
		leftLines = append(leftLines, fmt.Sprintf("%dms", clampInt(int(update.ElapsedMS), 0, 999)))
	}
	// leftLines = append(leftLines, fmt.Sprintf("x=%d  y=%d  score=%.3f", update.X, update.Y, update.Score))
	leftLines = append(leftLines, fmt.Sprintf("x=%d", update.X))
	leftLines = append(leftLines, fmt.Sprintf("y=%d", update.Y))
	leftLines = append(leftLines, fmt.Sprintf("score=%.3f", update.Score))

	leftW := 0.0
	for _, line := range leftLines {
		leftW = math.Max(leftW, estimateTextWidth(line))
	}
	leftRect := rectF(12, float64(size.Y-10-len(leftLines)*22-16), 12+leftW+20, float64(size.Y-10))
	y := int32(leftRect.Top + 8)
	firstColor := win.RGB(48, 220, 132)
	elapsed := clampInt(int(update.ElapsedMS), 0, 999)
	if state == "lost" {
		firstColor = win.RGB(255, 86, 86)
	} else if elapsed > 500 {
		firstColor = win.RGB(255, 86, 86)
	} else if elapsed > 200 {
		firstColor = win.RGB(245, 190, 60)
	}
	for i, line := range leftLines {
		color := win.RGB(235, 245, 255)
		if i == 0 {
			color = firstColor
		}
		drawGdiText(hdc, int32(leftRect.Left+10), y, line, color)
		y += 22
	}

	rightLines := make([]string, 0, 3)
	if w.locked {
		rightLines = append(rightLines, "已锁定")
	}
	rightLines = append(rightLines,
		fmt.Sprintf("Zoom %.0f%%", w.targetZoom*100),
		fmt.Sprintf("Opacity %.0f%%", float64(w.opacity)/255*100),
	)
	rightW := 20.0
	for _, line := range rightLines {
		rightW = math.Max(rightW, monoTextWidth(line)+20)
	}
	rightRect := rectF(float64(size.X)-rightW-12, float64(size.Y-10-len(rightLines)*22-16), float64(size.X-12), float64(size.Y-10))
	y = int32(rightRect.Top + 8)
	for _, line := range rightLines {
		lineW := monoTextWidth(line)
		drawGdiText(hdc, int32(rightRect.Right-10-float32(lineW)), y, line, win.RGB(235, 245, 255))
		y += 22
	}
}

func drawGdiText(hdc win.HDC, x, y int32, text string, color win.COLORREF) {
	win.SetTextColor(hdc, color)
	ptr := syscall.StringToUTF16Ptr(text)
	win.TextOut(hdc, x, y, ptr, int32(len([]rune(text))))
}

func estimateTextWidth(text string) float64 {
	width := 0.0
	for _, r := range text {
		if r <= 0x7f {
			width += 7.5
		} else {
			width += 14
		}
	}
	return math.Max(18, width)
}
