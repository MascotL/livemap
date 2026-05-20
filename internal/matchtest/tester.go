package matchtest

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"time"

	"github.com/mascotl/livemap/internal/appconfig"
	"github.com/mascotl/livemap/internal/mapmatch"
)

type Options struct {
	Config    appconfig.Config
	ImagePath string
	TimeoutMS int
}

type Result struct {
	Found          bool    `json:"found"`
	X              int     `json:"x"`
	Y              int     `json:"y"`
	Score          float64 `json:"score"`
	ElapsedMS      int64   `json:"elapsedMs"`
	TimedOut       bool    `json:"timedOut"`
	Canceled       bool    `json:"canceled"`
	Message        string  `json:"message"`
	ImagePath      string  `json:"imagePath"`
	TimeoutMS      int     `json:"timeoutMs"`
	WorldMap       string  `json:"worldMap"`
	PreviewDataURL string  `json:"previewDataUrl"`
}

func Run(ctx context.Context, opts Options) Result {
	if opts.TimeoutMS <= 0 {
		opts.TimeoutMS = 30000
	}
	start := time.Now()
	result := Result{
		ImagePath: opts.ImagePath,
		TimeoutMS: opts.TimeoutMS,
		WorldMap:  opts.Config.MapMatching.WorldMapPath,
	}

	cfg := opts.Config
	cfg.MapMatching.GlobalTimeoutMS = opts.TimeoutMS

	matcher, err := mapmatch.New(cfg.MapMatching)
	if err != nil {
		result.Message = "加载大地图失败"
		return result
	}
	minimap, err := mapmatch.LoadRGBA(opts.ImagePath)
	if err != nil {
		result.Message = "加载测试图片失败"
		return result
	}

	match := matcher.MatchGlobal(ctx, minimap, opts.TimeoutMS)
	result.ElapsedMS = time.Since(start).Milliseconds()
	result.Found = match.Found
	result.X = match.X
	result.Y = match.Y
	result.Score = match.Score
	result.TimedOut = match.TimedOut
	result.Canceled = match.Canceled

	switch {
	case match.Canceled:
		result.Message = "测试已停止"
	case match.Found:
		result.Message = "匹配成功"
		if preview, err := BuildPreview(cfg.MapMatching.WorldMapPath, minimap, match.X, match.Y); err == nil {
			result.PreviewDataURL = preview
		}
	case match.TimedOut:
		result.Message = "测试超时"
	default:
		result.Message = "未找到匹配"
	}
	return result
}

func BuildPreview(worldPath string, minimap *image.RGBA, x, y int) (string, error) {
	world, err := loadWorld(worldPath)
	if err != nil {
		return "", err
	}
	miniBounds := minimap.Bounds()
	paddingX := max(24, miniBounds.Dx()/4)
	paddingY := max(24, miniBounds.Dy()/4)
	miniX := x - miniBounds.Dx()/2
	miniY := y - miniBounds.Dy()/2
	crop := image.Rect(
		miniX-paddingX,
		miniY-paddingY,
		miniX+miniBounds.Dx()+paddingX,
		miniY+miniBounds.Dy()+paddingY,
	).Intersect(world.Bounds())
	if crop.Empty() {
		return "", fmt.Errorf("预览区域为空")
	}

	out := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	for py := 0; py < crop.Dy(); py++ {
		for px := 0; px < crop.Dx(); px++ {
			r, g, b, _ := world.At(crop.Min.X+px, crop.Min.Y+py).RGBA()
			out.SetRGBA(px, py, color.RGBA{
				R: uint8(r >> 8),
				G: uint8(g >> 8),
				B: uint8(b >> 8),
				A: 255,
			})
		}
	}

	overlayX := miniX - crop.Min.X
	overlayY := miniY - crop.Min.Y
	blendOverlay(out, minimap, overlayX, overlayY, 0.48)
	drawCenterMarker(out, x-crop.Min.X, y-crop.Min.Y)

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func drawCenterMarker(dst *image.RGBA, x, y int) {
	for dy := -8; dy <= 8; dy++ {
		py := y + dy
		if py < 0 || py >= dst.Bounds().Dy() {
			continue
		}
		for dx := -8; dx <= 8; dx++ {
			px := x + dx
			if px < 0 || px >= dst.Bounds().Dx() {
				continue
			}
			if dx*dx+dy*dy <= 64 {
				dst.SetRGBA(px, py, color.RGBA{R: 255, G: 48, B: 48, A: 255})
			}
		}
	}
}

func loadWorld(path string) (*image.RGBA, error) {
	if !filepath.IsAbs(path) {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}
	return mapmatch.LoadRGBA(path)
}

func blendOverlay(dst *image.RGBA, src *image.RGBA, x, y int, opacity float64) {
	for sy := 0; sy < src.Bounds().Dy(); sy++ {
		dy := y + sy
		if dy < 0 || dy >= dst.Bounds().Dy() {
			continue
		}
		for sx := 0; sx < src.Bounds().Dx(); sx++ {
			dx := x + sx
			if dx < 0 || dx >= dst.Bounds().Dx() {
				continue
			}
			s := src.RGBAAt(sx, sy)
			alpha := opacity * float64(s.A) / 255.0
			if alpha <= 0 {
				continue
			}
			d := dst.RGBAAt(dx, dy)
			dst.SetRGBA(dx, dy, color.RGBA{
				R: uint8(float64(s.R)*alpha + float64(d.R)*(1-alpha)),
				G: uint8(float64(s.G)*alpha + float64(d.G)*(1-alpha)),
				B: uint8(float64(s.B)*alpha + float64(d.B)*(1-alpha)),
				A: 255,
			})
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
