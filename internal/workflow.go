package internal

import (
	"context"
	"image"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mascotl/livemap/internal/appconfig"
	"github.com/mascotl/livemap/internal/capture"
	"github.com/mascotl/livemap/internal/logx"
	"github.com/mascotl/livemap/internal/maplibrary"
	"github.com/mascotl/livemap/internal/mapmatch"
	"github.com/mascotl/livemap/internal/mapoverlay"
	"github.com/mascotl/livemap/internal/procwin"
	"github.com/mascotl/livemap/internal/regionselect"
	"github.com/mascotl/livemap/internal/view"
)

var procGetAsyncKeyState = syscall.NewLazyDLL("user32.dll").NewProc("GetAsyncKeyState")

type DebugSink interface {
	Publish(view.DebugFrame)
}

type OverlaySink interface {
	Publish(mapoverlay.Update)
	Close()
}

type liveMatcher struct {
	mu      sync.RWMutex
	matcher *mapmatch.Matcher
	cfg     appconfig.MapMatching
}

func (m *liveMatcher) set(cfg appconfig.MapMatching) error {
	var matcher *mapmatch.Matcher
	var err error
	if cfg.WorldMapPath != "" {
		matcher, err = mapmatch.New(cfg)
		if err != nil {
			return err
		}
	}
	m.mu.Lock()
	old := m.matcher
	m.matcher = matcher
	m.cfg = cfg
	m.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}

func (m *liveMatcher) clear(cfg appconfig.MapMatching) {
	m.mu.Lock()
	old := m.matcher
	m.matcher = nil
	m.cfg = cfg
	m.mu.Unlock()
	if old != nil {
		old.Close()
	}
}

func (m *liveMatcher) snapshot() (*mapmatch.Matcher, appconfig.MapMatching) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.matcher, m.cfg
}

func startMatcherAsync(live *liveMatcher, cfg appconfig.MapMatching, successMessage string) {
	if cfg.WorldMapPath == "" {
		live.clear(cfg)
		return
	}
	go func() {
		if err := live.set(cfg); err != nil {
			live.clear(cfg)
			logx.Warnf("%s失败: path=%s err=%v", successMessage, cfg.WorldMapPath, err)
			return
		}
		logx.Infof("%s: %s", successMessage, cfg.WorldMapPath)
	}()
}

type RunOption func(*runOptions)

type runOptions struct {
	StopWorkflow   context.CancelFunc
	SelectMapFile  func() (string, error)
	SelectPinFiles func() ([]string, error)
}

func Run() error {
	return RunContext(context.Background())
}

func RunContext(ctx context.Context) error {
	// 第一步：处理内部工作进程入口。
	// 父进程会通过这个隐藏入口拉起专门的截图工作进程。
	// 正常启动时不需要手动传任何命令行参数。
	if len(os.Args) > 1 && os.Args[1] == "--worker" {
		return runWorker()
	}

	// 第二步：加载配置，并把目标进程解析成窗口句柄。
	// 现在所有运行参数都从配置文件读取。
	// Workflow 这里只负责把进程定位到窗口句柄，截图细节仍然留在 capture 模块内部处理。
	cfgPath := appconfig.DefaultConfigPath
	cfg, err := appconfig.Load(cfgPath)
	if err != nil {
		logx.Warnf("加载配置文件失败，回退默认值: path=%s err=%v", appconfig.ResolvePath(cfgPath), err)
		cfg = appconfig.Default()
	} else {
		logx.Infof("已加载配置文件: %s", appconfig.ResolvePath(cfgPath))
	}

	return RunWithConfig(ctx, cfg)
}

func RunWithConfig(ctx context.Context, cfg appconfig.Config) error {
	return runWithConfig(ctx, cfg, os.Stdout, nil)
}

func RunWithPreview(ctx context.Context, cfg appconfig.Config) error {
	return runWithConfig(ctx, cfg, nil, nil)
}

func RunWithDebugSink(ctx context.Context, cfg appconfig.Config, sink DebugSink) error {
	return runWithConfig(ctx, cfg, nil, sink)
}

func RunWithDebugSinkOptions(ctx context.Context, cfg appconfig.Config, sink DebugSink, opts ...RunOption) error {
	runOpts := runOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&runOpts)
		}
	}
	return runWithConfigOptions(ctx, cfg, nil, sink, runOpts)
}

func WithStopWorkflow(cancel context.CancelFunc) RunOption {
	return func(opts *runOptions) {
		opts.StopWorkflow = cancel
	}
}

func WithResourceDialogs(selectMap func() (string, error), selectPins func() ([]string, error)) RunOption {
	return func(opts *runOptions) {
		opts.SelectMapFile = selectMap
		opts.SelectPinFiles = selectPins
	}
}

func runWithConfig(ctx context.Context, cfg appconfig.Config, output io.Writer, sink DebugSink) error {
	return runWithConfigOptions(ctx, cfg, output, sink, runOptions{})
}

func runWithConfigOptions(ctx context.Context, cfg appconfig.Config, output io.Writer, sink DebugSink, runOpts runOptions) error {
	maplibrary.Clean(&cfg)
	if err := appconfig.Save(appconfig.DefaultConfigPath, cfg); err != nil {
		logx.Warnf("清理地图资源配置失败: %v", err)
	}
	window, err := procwin.FindMainWindowByProcessName(cfg.ProcessName)
	if err != nil {
		return err
	}

	windowRef := strconv.FormatUint(uint64(window.HWND), 10)
	logx.Infof("已根据进程名定位窗口: process=%s pid=%d hwnd=%d title=%s", cfg.ProcessName, window.PID, window.HWND, window.Title)

	clientRect, err := procwin.ClientRectOnScreen(window.HWND)
	if err != nil {
		return err
	}
	region, confirmed, err := regionselect.Select(ctx, clientRect, cfg.MinimapRegion)
	if err != nil {
		return err
	}
	if !confirmed {
		return fmtCanceledSelection()
	}
	cfg.MinimapRegion = region
	if err := appconfig.Save(appconfig.DefaultConfigPath, cfg); err != nil {
		logx.Warnf("保存小地图范围失败: %v", err)
	}
	live := &liveMatcher{cfg: cfg.MapMatching}
	var overlay OverlaySink
	if win, err := mapoverlay.Open(ctx, mapoverlay.Options{
		WorldMapPath: cfg.MapMatching.WorldMapPath,
		Resources:    maplibrary.RuntimeState(cfg),
		Width:        cfg.Overlay.Width,
		Height:       cfg.Overlay.Height,
		Zoom:         cfg.Overlay.Zoom,
		Opacity:      cfg.Overlay.Opacity,
		Topmost:      cfg.Overlay.Topmost,
		ShowLog:      cfg.Overlay.ShowLog,
		OnSettingsChanged: func(settings mapoverlay.Settings) {
			if err := saveOverlaySettings(settings); err != nil {
				logx.Warnf("保存实时地图窗口设置失败: %v", err)
			}
		},
		OnCloseRequested: runOpts.StopWorkflow,
		OnImportMap: func() (mapoverlay.ResourceState, error) {
			if runOpts.SelectMapFile == nil {
				return maplibrary.RuntimeState(cfg), nil
			}
			path, err := runOpts.SelectMapFile()
			if err != nil || path == "" {
				return maplibrary.RuntimeState(cfg), err
			}
			if err := maplibrary.AddMap(&cfg, path); err != nil {
				return maplibrary.RuntimeState(cfg), err
			}
			if err := appconfig.Save(appconfig.DefaultConfigPath, cfg); err != nil {
				return maplibrary.RuntimeState(cfg), err
			}
			state := maplibrary.RuntimeState(cfg)
			startMatcherAsync(live, cfg.MapMatching, "地图已导入并启用")
			return state, nil
		},
		OnImportPins: func() (mapoverlay.ResourceState, error) {
			if runOpts.SelectPinFiles == nil {
				return maplibrary.RuntimeState(cfg), nil
			}
			paths, err := runOpts.SelectPinFiles()
			if err != nil {
				return maplibrary.RuntimeState(cfg), err
			}
			for _, path := range paths {
				if path == "" {
					continue
				}
				if err := maplibrary.AddPin(&cfg, path); err != nil {
					return maplibrary.RuntimeState(cfg), err
				}
			}
			if err := appconfig.Save(appconfig.DefaultConfigPath, cfg); err != nil {
				return maplibrary.RuntimeState(cfg), err
			}
			return maplibrary.RuntimeState(cfg), nil
		},
		OnSelectMap: func(path string) (mapoverlay.ResourceState, error) {
			maplibrary.SelectMap(&cfg, path)
			if err := appconfig.Save(appconfig.DefaultConfigPath, cfg); err != nil {
				return maplibrary.RuntimeState(cfg), err
			}
			state := maplibrary.RuntimeState(cfg)
			startMatcherAsync(live, cfg.MapMatching, "地图已切换并启用")
			return state, nil
		},
		OnTogglePins: func(path string) (mapoverlay.ResourceState, error) {
			maplibrary.TogglePin(&cfg, path)
			if err := appconfig.Save(appconfig.DefaultConfigPath, cfg); err != nil {
				return maplibrary.RuntimeState(cfg), err
			}
			return maplibrary.RuntimeState(cfg), nil
		},
	}); err != nil {
		logx.Warnf("打开实时地图窗口失败: %v", err)
	} else {
		overlay = win
		defer overlay.Close()
		logx.Infof("实时地图窗口已打开")
	}
	if cfg.MapMatching.WorldMapPath != "" {
		startMatcherAsync(live, cfg.MapMatching, "地图匹配资源已启用")
	}
	logx.Infof(
		"地图匹配节点已启动: world=%s miniScale=1/%d mapScale=1/%d threshold=%.2f globalTimeout=%dms",
		cfg.MapMatching.WorldMapPath,
		cfg.MapMatching.GlobalMinimapScale,
		cfg.MapMatching.GlobalMapScale,
		cfg.MapMatching.MatchThreshold,
		cfg.MapMatching.GlobalTimeoutMS,
	)

	reader, writer := io.Pipe()
	captureErrCh := make(chan error, 1)
	streamErrCh := make(chan error, 1)
	go func() {
		err := capture.StartSupervisorToContext(ctx, windowRef, cfg.Backend, cfg.FPS, writer)
		if err != nil {
			_ = writer.CloseWithError(err)
			captureErrCh <- err
			return
		}
		_ = writer.Close()
		captureErrCh <- nil
	}()
	go func() {
		streamErrCh <- processFrameStream(ctx, reader, output, sink, overlay, region, live)
	}()

	select {
	case err := <-streamErrCh:
		return err
	case err := <-captureErrCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func saveOverlaySettings(settings mapoverlay.Settings) error {
	cfg, err := appconfig.Load(appconfig.DefaultConfigPath)
	if err != nil {
		return err
	}
	cfg.Overlay.Zoom = settings.Zoom
	cfg.Overlay.Width = settings.Width
	cfg.Overlay.Height = settings.Height
	cfg.Overlay.Opacity = settings.Opacity
	cfg.Overlay.Topmost = settings.Topmost
	cfg.Overlay.ShowLog = settings.ShowLog
	return appconfig.Save(appconfig.DefaultConfigPath, cfg)
}

func processFrameStream(ctx context.Context, reader io.Reader, output io.Writer, sink DebugSink, overlay OverlaySink, region appconfig.MinimapRegion, live *liveMatcher) error {
	logx.Infof("开始输出小地图 ROI 视频流: x=%d y=%d size=%d", region.X, region.Y, region.Size)
	waitingLogged := false
	var lastSuccessLog time.Time
	var lastAutoGlobal time.Time
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		frame, err := view.ReadFrame(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		img := viewFrameToRGBA(frame)
		roi, err := cropToRegion(img, region)
		if err != nil {
			return err
		}

		if output != nil {
			outFrame := capture.NewFrame(roi)
			outFrame.FrameID = frame.FrameID
			outFrame.Timestamp = frame.Timestamp
			if err := capture.WriteFrame(output, outFrame); err != nil {
				return err
			}
		}

		if sink != nil {
			sink.Publish(view.DebugFrame{
				Image:     roi,
				FrameID:   frame.FrameID,
				Timestamp: frame.Timestamp,
			})
		}
		live.mu.RLock()
		matcher, matchingCfg := live.matcher, live.cfg
		if matcher == nil || matchingCfg.WorldMapPath == "" {
			live.mu.RUnlock()
			if overlay != nil {
				overlay.Publish(mapoverlay.Update{State: "nomap"})
			}
			continue
		}
		forceGlobal := false
		if matchingCfg.AutoGlobalSearch && matcher.WaitingForGlobalSearch() && time.Since(lastAutoGlobal) >= time.Second {
			forceGlobal = true
			lastAutoGlobal = time.Now()
		}
		if !forceGlobal {
			forceGlobal = isGlobalSearchHotkeyPressed(matchingCfg.GlobalSearchHotkey)
		}
		if forceGlobal {
			logx.Infof("收到全局搜索热键: %s", matchingCfg.GlobalSearchHotkey)
		}
		needsGlobal := matcher.NeedsGlobalSearch()
		if needsGlobal && (forceGlobal || !matcher.WaitingForGlobalSearch()) {
			logx.Infof("地图全局搜索开始: timeout=%dms", matchingCfg.GlobalTimeoutMS)
			if overlay != nil {
				overlay.Publish(mapoverlay.Update{State: "searching", Hotkey: matchingCfg.GlobalSearchHotkey})
			}
		}
		matchStart := time.Now()
		result := matcher.Match(roi, forceGlobal)
		waitingForGlobal := matcher.WaitingForGlobalSearch()
		live.mu.RUnlock()
		elapsedMS := time.Since(matchStart).Milliseconds()
		if result.Found {
			waitingLogged = false
			mode := "local"
			if result.Global {
				mode = "global"
			}
			if time.Since(lastSuccessLog) >= time.Second || result.Global {
				logx.Infof("地图匹配成功: mode=%s x=%d y=%d score=%.3f elapsed=%dms", mode, result.X, result.Y, result.Score, elapsedMS)
				lastSuccessLog = time.Now()
			}
			if overlay != nil {
				overlay.Publish(mapoverlay.Update{State: "found", X: result.X, Y: result.Y, Score: result.Score, Hotkey: matchingCfg.GlobalSearchHotkey, ElapsedMS: elapsedMS})
			}
		} else if result.TimedOut {
			waitingLogged = true
			logx.Warnf("地图全局搜索超时: timeout=%dms，等待全局搜索热键: %s", matchingCfg.GlobalTimeoutMS, matchingCfg.GlobalSearchHotkey)
			if overlay != nil {
				overlay.Publish(mapoverlay.Update{State: "lost", Hotkey: matchingCfg.GlobalSearchHotkey, ElapsedMS: elapsedMS})
			}
		} else if waitingForGlobal && !waitingLogged {
			waitingLogged = true
			logx.Warnf("地图匹配未找到，等待全局搜索热键: %s", matchingCfg.GlobalSearchHotkey)
			if overlay != nil {
				overlay.Publish(mapoverlay.Update{State: "lost", Hotkey: matchingCfg.GlobalSearchHotkey, ElapsedMS: elapsedMS})
			}
		}
	}
}

func isGlobalSearchHotkeyPressed(key string) bool {
	vk := hotkeyVK(key)
	if vk == 0 {
		return false
	}
	state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return state&0x8000 != 0
}

func hotkeyVK(key string) uintptr {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "delete", "del":
		return 0x2E
	case "insert", "ins":
		return 0x2D
	case "home":
		return 0x24
	case "end":
		return 0x23
	case "f5":
		return 0x74
	case "f6":
		return 0x75
	default:
		return 0
	}
}

func viewFrameToRGBA(frame *view.Frame) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, frame.Width, frame.Height))
	for y := 0; y < frame.Height; y++ {
		srcStart := y * frame.Stride
		srcEnd := srcStart + frame.Width*4
		dstStart := y * img.Stride
		copy(img.Pix[dstStart:dstStart+frame.Width*4], frame.Pix[srcStart:srcEnd])
	}
	return img
}

func cropToRegion(img *image.RGBA, region appconfig.MinimapRegion) (*image.RGBA, error) {
	rect := image.Rect(region.X, region.Y, region.X+region.Size, region.Y+region.Size).Intersect(img.Bounds())
	if rect.Empty() {
		return nil, fmtInvalidRegion(region)
	}
	cropped := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := 0; y < rect.Dy(); y++ {
		srcStart := img.PixOffset(rect.Min.X, rect.Min.Y+y)
		dstStart := cropped.PixOffset(0, y)
		copy(cropped.Pix[dstStart:dstStart+rect.Dx()*4], img.Pix[srcStart:srcStart+rect.Dx()*4])
	}
	return cropped, nil
}

func fmtCanceledSelection() error {
	return &workflowError{message: "用户取消了小地图范围选择"}
}

func fmtInvalidRegion(region appconfig.MinimapRegion) error {
	return &workflowError{message: "小地图范围无效: x=" + strconv.Itoa(region.X) + " y=" + strconv.Itoa(region.Y) + " size=" + strconv.Itoa(region.Size)}
}

type workflowError struct {
	message string
}

func (e *workflowError) Error() string {
	return e.message
}

func runWorker() error {
	windowRef := ""
	backend := ""
	fps := capture.DefaultFPS

	if len(os.Args) > 2 {
		windowRef = os.Args[2]
	}
	if len(os.Args) > 3 {
		backend = os.Args[3]
	}
	if len(os.Args) > 4 {
		if parsed, err := strconv.Atoi(os.Args[4]); err == nil {
			fps = parsed
		}
	}

	return capture.RunBackendTo(windowRef, backend, fps, os.Stdout)
}

func isInteractiveStdout() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
