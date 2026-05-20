package gui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	core "github.com/mascotl/livemap/internal"
	"github.com/mascotl/livemap/internal/appconfig"
	"github.com/mascotl/livemap/internal/debugview"
	"github.com/mascotl/livemap/internal/logx"
	"github.com/mascotl/livemap/internal/matchtest"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx context.Context

	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	running bool
	debug   *debugview.Manager

	testCancel  context.CancelFunc
	testRunning bool
}

type Status struct {
	Running bool   `json:"running"`
	Message string `json:"message"`
}

type DebugStatus struct {
	InspectWindow bool `json:"inspectWindow"`
}

type MatchTestStatus struct {
	Running bool   `json:"running"`
	Message string `json:"message"`
}

type MatchTestResult struct {
	Running        bool    `json:"running"`
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

func NewApp() *App {
	return &App{debug: debugview.NewManager()}
}

func (a *App) OnStartup(ctx context.Context) {
	a.ctx = ctx
	logCh, unsubscribe := logx.Subscribe()

	go func() {
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-logCh:
				if !ok {
					return
				}
				runtime.EventsEmit(ctx, "log:line", line)
			}
		}
	}()
}

func (a *App) LoadConfig() (appconfig.Config, error) {
	cfg, err := appconfig.Load(appconfig.DefaultConfigPath)
	if err != nil {
		logx.Warnf("加载配置文件失败，使用默认配置: %v", err)
		return appconfig.Default(), nil
	}
	if err := appconfig.Save(appconfig.DefaultConfigPath, cfg); err != nil {
		logx.Warnf("保存清理后的配置失败: %v", err)
	}
	return cfg, nil
}

func (a *App) SaveConfig(cfg appconfig.Config) error {
	if existing, err := appconfig.Load(appconfig.DefaultConfigPath); err == nil {
		cfg.MinimapRegion = existing.MinimapRegion
		cfg.Overlay = existing.Overlay
		cfg.Resources = existing.Resources
		cfg.MapMatching.WorldMapPath = existing.MapMatching.WorldMapPath
	}
	if err := appconfig.Save(appconfig.DefaultConfigPath, cfg); err != nil {
		return err
	}
	logx.Infof("配置已保存: %s", appconfig.ResolvePath(appconfig.DefaultConfigPath))
	return nil
}

func (a *App) Start() (Status, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return Status{Running: true, Message: "workflow 已在运行"}, nil
	}
	a.mu.Unlock()

	cfg, err := appconfig.Load(appconfig.DefaultConfigPath)
	if err != nil {
		logx.Errorf("读取配置失败，workflow 未启动: %v", err)
		return Status{Running: false, Message: "读取配置失败"}, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	a.mu.Lock()
	a.cancel = cancel
	a.done = done
	a.running = true
	a.mu.Unlock()

	a.emitStatus("running", "workflow 正在运行")
	logx.Infof("workflow 启动中: process=%s backend=%s fps=%d", cfg.ProcessName, cfg.Backend, cfg.FPS)

	go func() {
		defer close(done)
		err := core.RunWithDebugSinkOptions(ctx, cfg, a.debug,
			core.WithStopWorkflow(cancel),
			core.WithResourceDialogs(a.selectMapFromOverlay, a.selectPinsFromOverlay),
		)
		if err != nil && !errors.Is(err, context.Canceled) {
			logx.Errorf("workflow 已停止: %v", err)
		} else {
			logx.Infof("workflow 已停止")
		}

		a.mu.Lock()
		a.running = false
		a.cancel = nil
		a.done = nil
		a.mu.Unlock()
		a.emitStatus("stopped", "workflow 已停止")
	}()

	return Status{Running: true, Message: "workflow 正在运行"}, nil
}

func (a *App) Stop() (Status, error) {
	done := a.requestStop()
	if done == nil {
		return Status{Running: false, Message: "workflow 未运行"}, nil
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		logx.Warnf("workflow 停止等待超时，后台仍在退出中")
	}

	return a.Status(), nil
}

func (a *App) Restart() (Status, error) {
	done := a.requestStop()
	if done != nil {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			logx.Warnf("workflow 重启等待超时，继续启动新流程")
		}
	}
	return a.Start()
}

func (a *App) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return Status{Running: true, Message: "workflow 正在运行"}
	}
	return Status{Running: false, Message: "workflow 未运行"}
}

func (a *App) Logs() []string {
	return logx.History()
}

func (a *App) ClearLogs() {
	logx.Clear()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "log:cleared")
	}
}

func (a *App) DebugStatus() DebugStatus {
	return DebugStatus{InspectWindow: a.debug.IsOpen()}
}

func (a *App) SetInspectWindow(enabled bool) DebugStatus {
	if enabled {
		a.debug.Open(context.Background(), a.emitDebugStatus)
	} else {
		a.debug.Close()
	}
	a.emitDebugStatus()
	return a.DebugStatus()
}

func (a *App) SelectMatchTestImage() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("窗口尚未初始化")
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择小地图测试图片",
		Filters: []runtime.FileFilter{
			{DisplayName: "图片文件 (*.png;*.jpg;*.jpeg)", Pattern: "*.png;*.jpg;*.jpeg"},
			{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
		},
	})
}

func (a *App) SelectWorldMapFile() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("窗口尚未初始化")
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择大地图文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "地图包 (*.map)", Pattern: "*.map"},
		},
	})
}

func (a *App) selectMapFromOverlay() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("窗口尚未初始化")
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "导入地图文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "地图包 (*.map)", Pattern: "*.map"},
		},
	})
}

func (a *App) selectPinsFromOverlay() ([]string, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("窗口尚未初始化")
	}
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "导入标点文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "标点文件 (*.gmp)", Pattern: "*.gmp"},
		},
	})
}

func (a *App) StartMapMatchTest(imagePath string, timeoutMS int) (MatchTestStatus, error) {
	if imagePath == "" {
		return MatchTestStatus{Running: false, Message: "请选择测试图片"}, fmt.Errorf("测试图片路径为空")
	}
	if timeoutMS <= 0 {
		timeoutMS = 30000
	}

	a.mu.Lock()
	if a.testRunning {
		a.mu.Unlock()
		return MatchTestStatus{Running: true, Message: "地图匹配测试正在运行"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.testCancel = cancel
	a.testRunning = true
	a.mu.Unlock()

	logx.Infof("地图匹配测试开始: image=%s timeout=%dms", imagePath, timeoutMS)
	a.emitMatchTestStatus(MatchTestStatus{Running: true, Message: "地图匹配测试正在运行"})

	go a.runMapMatchTest(ctx, imagePath, timeoutMS)
	return MatchTestStatus{Running: true, Message: "地图匹配测试正在运行"}, nil
}

func (a *App) StopMapMatchTest() MatchTestStatus {
	a.mu.Lock()
	cancel := a.testCancel
	a.mu.Unlock()
	if cancel != nil {
		logx.Infof("正在请求停止地图匹配测试")
		cancel()
		return MatchTestStatus{Running: true, Message: "正在停止地图匹配测试"}
	}
	return MatchTestStatus{Running: false, Message: "地图匹配测试未运行"}
}

func (a *App) MatchTestStatus() MatchTestStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.testRunning {
		return MatchTestStatus{Running: true, Message: "地图匹配测试正在运行"}
	}
	return MatchTestStatus{Running: false, Message: "地图匹配测试未运行"}
}

func (a *App) runMapMatchTest(ctx context.Context, imagePath string, timeoutMS int) {
	start := time.Now()
	result := MatchTestResult{
		Running:   false,
		ImagePath: imagePath,
		TimeoutMS: timeoutMS,
	}
	defer func() {
		a.mu.Lock()
		a.testCancel = nil
		a.testRunning = false
		a.mu.Unlock()
		a.emitMatchTestResult(result)
		a.emitMatchTestStatus(MatchTestStatus{Running: false, Message: result.Message})
	}()

	cfg, err := appconfig.Load(appconfig.DefaultConfigPath)
	if err != nil {
		result.Message = "读取配置失败"
		logx.Errorf("地图匹配测试失败: %v", err)
		return
	}
	cfg.MapMatching.GlobalTimeoutMS = timeoutMS
	testResult := matchtest.Run(ctx, matchtest.Options{
		Config:    cfg,
		ImagePath: imagePath,
		TimeoutMS: timeoutMS,
	})
	result.ElapsedMS = testResult.ElapsedMS
	result.Found = testResult.Found
	result.X = testResult.X
	result.Y = testResult.Y
	result.Score = testResult.Score
	result.TimedOut = testResult.TimedOut
	result.Canceled = testResult.Canceled
	result.Message = testResult.Message
	result.WorldMap = testResult.WorldMap
	result.PreviewDataURL = testResult.PreviewDataURL
	if result.ElapsedMS == 0 {
		result.ElapsedMS = time.Since(start).Milliseconds()
	}

	switch {
	case result.Canceled:
		logx.Warnf("地图匹配测试已停止: elapsed=%dms", result.ElapsedMS)
	case result.Found:
		logx.Infof("地图匹配测试成功: x=%d y=%d score=%.3f elapsed=%dms", result.X, result.Y, result.Score, result.ElapsedMS)
	case result.TimedOut:
		logx.Warnf("地图匹配测试超时: timeout=%dms elapsed=%dms", timeoutMS, result.ElapsedMS)
	default:
		logx.Warnf("地图匹配测试未找到: elapsed=%dms", result.ElapsedMS)
	}
}

func (a *App) MinimizeWindow() {
	if a.ctx != nil {
		runtime.WindowMinimise(a.ctx)
	}
}

func (a *App) CloseWindow() {
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

func (a *App) requestStop() chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running || a.cancel == nil {
		return nil
	}

	logx.Infof("正在请求停止 workflow")
	a.cancel()
	return a.done
}

func (a *App) status(message string) Status {
	current := a.Status()
	current.Message = message
	return current
}

func (a *App) emitStatus(state, message string) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "workflow:status", map[string]any{
		"state":   state,
		"message": message,
	})
}

func (a *App) emitDebugStatus() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "debug:status", a.DebugStatus())
}

func (a *App) emitMatchTestStatus(status MatchTestStatus) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "match-test:status", status)
}

func (a *App) emitMatchTestResult(result MatchTestResult) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "match-test:result", result)
}
