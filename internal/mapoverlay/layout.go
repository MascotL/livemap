package mapoverlay

import (
	"github.com/lxn/win"
	"image"
	"os"
	"path/filepath"
	"syscall"
)

func toolbarRect(size image.Point, alphaMode bool) image.Rectangle {
	w := toolbarPadX()*2 + 8*30 + 7*(toolbarButtonStep()-30)
	h := 46
	x := (size.X - w) / 2
	return image.Rect(x, 12, x+w, 12+h)
}

func toolbarPadX() int {
	return 12
}

func toolbarButtonStep() int {
	return 38
}

func backRect(size image.Point) image.Rectangle {
	p := toolbarRect(size, true)
	return image.Rect(p.Min.X+12, p.Min.Y+8, p.Min.X+42, p.Min.Y+38)
}

func sliderRect(size image.Point) image.Rectangle {
	return alphaSliderRect(size)
}

func alphaMenuRect(size image.Point) image.Rectangle {
	pill := toolbarRect(size, false)
	x := pill.Min.X + toolbarPadX() + 4*toolbarButtonStep()
	return image.Rect(x-66, pill.Max.Y+8, x+152, pill.Max.Y+44)
}

func alphaSliderRect(size image.Point) image.Rectangle {
	box := alphaMenuRect(size)
	return image.Rect(box.Min.X+24, box.Min.Y+15, box.Max.X-24, box.Min.Y+21)
}

type resourceMenuItem struct {
	key          string
	label        string
	icon         string
	checked      bool
	disabled     bool
	importAction bool
}

func resourceMenuRect(w *Window, size image.Point) image.Rectangle {
	pill := toolbarRect(size, false)
	x := pill.Min.X + toolbarPadX() + 2*toolbarButtonStep()
	if w.menuMode == "pins" {
		x += toolbarButtonStep()
	}
	items := resourceMenuItems(w)
	h := 10 + max(1, len(items))*26
	return image.Rect(x-20, pill.Max.Y+8, x+116, pill.Max.Y+8+h)
}

func resourceMenuItemRect(menu image.Rectangle, idx int) image.Rectangle {
	y := menu.Min.Y + 6 + idx*26
	return image.Rect(menu.Min.X+7, y, menu.Max.X-7, y+22)
}

func resourceMenuItems(w *Window) []resourceMenuItem {
	if w.menuMode == "map" {
		items := make([]resourceMenuItem, 0, len(w.resources.Maps)+1)
		for _, item := range w.resources.Maps {
			label := item.Game
			if label == "" {
				label = filepath.Base(item.Path)
			}
			items = append(items, resourceMenuItem{key: item.Path, label: label, checked: item.Selected})
		}
		if len(items) == 0 {
			items = append(items, resourceMenuItem{key: "none", label: "无", disabled: true})
		}
		items = append(items, resourceMenuItem{key: "import-map", label: "导入地图", icon: "map-plus", importAction: true})
		return items
	}
	items := make([]resourceMenuItem, 0, len(w.resources.Pins)+1)
	for _, item := range w.resources.Pins {
		items = append(items, resourceMenuItem{key: item.Path, label: item.Name, checked: item.Enabled})
	}
	if len(items) == 0 {
		items = append(items, resourceMenuItem{key: "none", label: "无", disabled: true})
	}
	items = append(items, resourceMenuItem{key: "import-pins", label: "导入标点文件", icon: "plus", importAction: true})
	return items
}

func noMapImportRect(size image.Point) image.Rectangle {
	cx := size.X / 2
	cy := size.Y/2 - 4
	return image.Rect(cx-48, cy+2, cx+48, cy+30)
}

func mapRect(size image.Point) image.Rectangle {
	return image.Rect(edgeGrip, edgeGrip, size.X-edgeGrip, size.Y-edgeGrip)
}

func clientSize(hwnd win.HWND) image.Point {
	var rect win.RECT
	if !win.GetClientRect(hwnd, &rect) {
		return image.Point{}
	}
	return image.Pt(int(rect.Right-rect.Left), int(rect.Bottom-rect.Top))
}

func resolvePath(path string) string {
	if path == "" {
		path = "resource/world_map.png"
	}
	if filepath.IsAbs(path) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func resolveAssetPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if abs, err := filepath.Abs(path); err == nil {
		if _, statErr := os.Stat(abs); statErr == nil {
			return abs
		}
	}
	dir, err := os.Getwd()
	if err == nil {
		for {
			candidate := filepath.Join(dir, path)
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), path)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate
		}
	}
	return path
}

func createGdiFont(family string, size, weight int32) win.HFONT {
	var lf win.LOGFONT
	lf.LfHeight = -size
	lf.LfWeight = weight
	lf.LfQuality = win.CLEARTYPE_QUALITY
	copy(lf.LfFaceName[:], syscall.StringToUTF16(family))
	return win.CreateFontIndirect(&lf)
}

func colorF(r, g, b int, a float32) d2dColorF {
	return d2dColorF{R: float32(r) / 255, G: float32(g) / 255, B: float32(b) / 255, A: a}
}

func rectF(l, t, r, b float64) d2dRectF {
	return d2dRectF{Left: float32(l), Top: float32(t), Right: float32(r), Bottom: float32(b)}
}

func rectFromImage(r image.Rectangle) d2dRectF {
	return rectF(float64(r.Min.X), float64(r.Min.Y), float64(r.Max.X), float64(r.Max.Y))
}

func offsetRect(r d2dRectF, dx, dy float64) d2dRectF {
	return d2dRectF{Left: r.Left + float32(dx), Top: r.Top + float32(dy), Right: r.Right + float32(dx), Bottom: r.Bottom + float32(dy)}
}

func approach(v, target, step float64) float64 {
	step = clamp01(step)
	return v + (target-v)*step
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
