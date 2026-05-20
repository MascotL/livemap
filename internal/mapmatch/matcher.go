package mapmatch

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mascotl/livemap/internal/appconfig"
	"github.com/mascotl/livemap/internal/mapbundle"
)

type Result struct {
	Found    bool
	X        int
	Y        int
	Score    float64
	Global   bool
	TimedOut bool
	Canceled bool
}

type Matcher struct {
	cfg     appconfig.MapMatching
	native  *nativeMatcher
	last    image.Point
	hasLast bool
	waiting bool
}

func New(cfg appconfig.MapMatching) (*Matcher, error) {
	worldPath := cfg.WorldMapPath
	if !filepath.IsAbs(worldPath) {
		if abs, err := filepath.Abs(worldPath); err == nil {
			worldPath = abs
		}
	}

	worldRGBA, err := LoadRGBA(worldPath)
	if err != nil {
		return nil, err
	}

	native, err := newNativeMatcher(worldRGBA, cfg)
	if err != nil {
		return nil, fmt.Errorf("初始化 OpenCV 地图匹配失败: %w", err)
	}

	matcher := &Matcher{cfg: cfg, native: native}
	runtime.SetFinalizer(matcher, (*Matcher).Close)
	return matcher, nil
}

func (m *Matcher) Close() {
	if m == nil || m.native == nil {
		return
	}
	m.native.close()
	m.native = nil
}

func (m *Matcher) Match(minimap *image.RGBA, forceGlobal bool) Result {
	return m.match(context.Background(), minimap, forceGlobal, m.cfg.GlobalTimeoutMS)
}

func (m *Matcher) MatchGlobal(ctx context.Context, minimap *image.RGBA, timeoutMS int) Result {
	return m.match(ctx, minimap, true, timeoutMS)
}

func (m *Matcher) match(ctx context.Context, minimap *image.RGBA, forceGlobal bool, globalTimeoutMS int) Result {
	if isCanceled(ctx) {
		return Result{Canceled: true, Global: forceGlobal}
	}
	if m == nil || m.native == nil {
		return Result{}
	}
	if m.waiting && !forceGlobal {
		return Result{}
	}

	if forceGlobal || !m.hasLast {
		result := m.native.matchGlobal(minimap, globalTimeoutMS)
		m.acceptOrWait(result)
		if isCanceled(ctx) && !result.Found {
			result.Canceled = true
		}
		return result
	}

	localTimeoutMS := m.localTimeoutMS()
	result := m.native.matchLocal(minimap, m.last.X, m.last.Y, max(1, m.cfg.LocalROI), localTimeoutMS)
	if !result.Found && !result.Canceled && m.cfg.LocalExpandedROI > m.cfg.LocalROI {
		result = m.native.matchLocal(minimap, m.last.X, m.last.Y, max(1, m.cfg.LocalExpandedROI), localTimeoutMS)
	}
	if result.Found {
		m.accept(result)
		return result
	}
	m.waiting = true
	return result
}

func (m *Matcher) WaitingForGlobalSearch() bool {
	return m.waiting
}

func (m *Matcher) NeedsGlobalSearch() bool {
	return !m.hasLast || m.waiting
}

func (m *Matcher) acceptOrWait(result Result) {
	if result.Found {
		m.accept(result)
		return
	}
	if !result.Canceled {
		m.waiting = true
	}
}

func (m *Matcher) accept(result Result) {
	if !result.Found {
		return
	}
	m.hasLast = true
	m.waiting = false
	m.last = image.Pt(result.X, result.Y)
}

func (m *Matcher) localTimeoutMS() int {
	timeout := m.cfg.GlobalTimeoutMS
	if timeout <= 0 {
		timeout = 5000
	}
	if timeout > 180 {
		return 180
	}
	return timeout
}

func LoadRGBA(path string) (*image.RGBA, error) {
	reader, err := openImageReader(path)
	if err != nil {
		return nil, fmt.Errorf("打开测试图片失败: %w", err)
	}
	defer reader.Close()

	img, _, err := image.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("解析测试图片失败: %w", err)
	}
	bounds := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, bounds.Min, draw.Src)
	return rgba, nil
}

func openImageReader(path string) (io.ReadCloser, error) {
	if strings.EqualFold(filepath.Ext(path), ".map") {
		bundle, err := mapbundle.Open(path)
		if err != nil {
			return nil, err
		}
		return bundle.Image()
	}
	return os.Open(path)
}

func isCanceled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
