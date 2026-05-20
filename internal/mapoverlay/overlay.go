package mapoverlay

import (
	"context"
	"math"
	"runtime"

	"github.com/lxn/win"
	"github.com/mascotl/livemap/internal/logx"
	"image"
	"sync"
	"time"
)

type Options struct {
	WorldMapPath      string
	Resources         ResourceState
	Width             int
	Height            int
	Zoom              float64
	Opacity           int
	Topmost           bool
	ShowLog           bool
	OnSettingsChanged func(Settings)
	OnCloseRequested  func()
	OnImportMap       func() (ResourceState, error)
	OnImportPins      func() (ResourceState, error)
	OnSelectMap       func(string) (ResourceState, error)
	OnTogglePins      func(string) (ResourceState, error)
}

type Settings struct {
	Width   int
	Height  int
	Zoom    float64
	Opacity int
	Topmost bool
	ShowLog bool
}

type Update struct {
	State     string
	X         int
	Y         int
	Score     float64
	Hotkey    string
	ElapsedMS int64
}

type ResourceState struct {
	Maps       []MapChoice
	Pins       []PinChoice
	Categories []PinCategory
	Markers    []PinMarker
}

type MapChoice struct {
	Path       string
	Game       string
	MapVersion string
	Selected   bool
}

type PinChoice struct {
	Path       string
	Name       string
	Game       string
	MapVersion string
	Enabled    bool
}

type PinCategory struct {
	Name      string
	Image     []byte
	Thumb     []byte
	HasVisual bool
}

type PinMarker struct {
	Category string
	X        int
	Y        int
	Desc     string
}

type Window struct {
	hwnd win.HWND

	mu        sync.RWMutex
	opts      Options
	update    Update
	closeOnce sync.Once
	renderer  *renderer
	initErr   error
	ready     chan struct{}
	closed    chan struct{}

	hover       bool
	tracking    bool
	hoverButton string
	pressed     string
	alphaMode   bool
	menuMode    string
	topmost     bool
	showLog     bool
	locked      bool
	opacity     byte

	toolbarAlpha float64
	frameActive  bool
	lastFrame    time.Time
	saveTimer    *time.Timer

	hasFix        bool
	displayX      float64
	displayY      float64
	targetX       float64
	targetY       float64
	lastFoundX    float64
	lastFoundY    float64
	zoom          float64
	targetZoom    float64
	panX          float64
	panY          float64
	draggingMap   bool
	draggingWin   bool
	draggingAlpha bool
	lastMouse     image.Point
	lastDragEnd   time.Time
	loadingRot    float64
	loadingMap    bool
	resources     ResourceState
}

type renderer struct {
	hwnd win.HWND

	factory *comObject
	wic     *comObject
	dwrite  *comObject
	target  *comObject

	mapBitmap *comObject
	mapPath   string
	mapWidth  uint32
	mapHeight uint32

	brushes   map[string]*comObject
	icons     map[string]*comObject
	pinIcons  map[string]*comObject
	textUI    *comObject
	textSmall *comObject
	textLog   *comObject
	logFont   win.HFONT
	fonts     []string
}

type buttonHit struct {
	id    string
	title string
	rect  image.Rectangle
}

func Open(ctx context.Context, opts Options) (*Window, error) {
	if opts.Width <= 0 {
		opts.Width = 420
	}
	if opts.Height <= 0 {
		opts.Height = opts.Width
	}
	if opts.Zoom <= 0 {
		opts.Zoom = 1.5
	}
	if opts.Opacity <= 0 {
		opts.Opacity = defaultOpacity
	}
	if opts.Opacity < minOpacity {
		opts.Opacity = minOpacity
	}
	if opts.Opacity > maxOpacity {
		opts.Opacity = maxOpacity
	}
	w := &Window{
		opts:       opts,
		ready:      make(chan struct{}),
		closed:     make(chan struct{}),
		topmost:    opts.Topmost,
		showLog:    opts.ShowLog,
		opacity:    byte(opts.Opacity),
		zoom:       opts.Zoom,
		targetZoom: opts.Zoom,
		resources:  opts.Resources,
	}
	go w.run(ctx)
	<-w.ready
	if w.initErr != nil {
		return nil, w.initErr
	}
	return w, nil
}

func (w *Window) Publish(update Update) {
	w.mu.Lock()
	old := w.update
	w.update = update
	w.mu.Unlock()
	if w.hwnd != 0 && (old.State != update.State || old.X != update.X || old.Y != update.Y || old.ElapsedMS != update.ElapsedMS) {
		win.PostMessage(w.hwnd, wmUpdate, 0, 0)
	}
}

func (w *Window) UpdateResources(state ResourceState) {
	w.mu.Lock()
	w.resources = state
	w.opts.Resources = state
	w.opts.WorldMapPath = ""
	w.loadingMap = false
	for _, item := range state.Maps {
		if item.Selected {
			w.opts.WorldMapPath = item.Path
			break
		}
	}
	w.hasFix = false
	w.mu.Unlock()
	if w.hwnd != 0 {
		win.PostMessage(w.hwnd, wmResource, 0, 0)
	}
}

func (w *Window) beginMapLoading() {
	w.mu.Lock()
	w.loadingMap = true
	w.hasFix = false
	w.menuMode = ""
	w.hoverButton = ""
	w.pressed = ""
	w.mu.Unlock()
	w.ensureFrameTimer()
	w.requestPaint()
}

func (w *Window) Close() {
	w.closeOnce.Do(func() {
		if w.hwnd != 0 {
			win.PostMessage(w.hwnd, win.WM_CLOSE, 0, 0)
		}
	})
}

func (w *Window) Done() <-chan struct{} {
	return w.closed
}

func (w *Window) run(ctx context.Context) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(w.closed)
	procCoInitializeEx.Call(0, 2)

	current = w
	defer func() { current = nil }()

	if err := registerClass(); err != nil {
		w.initErr = err
		close(w.ready)
		return
	}
	if err := w.create(); err != nil {
		w.initErr = err
		close(w.ready)
		return
	}
	close(w.ready)

	go func() {
		<-ctx.Done()
		w.Close()
	}()

	var msg win.MSG
	for {
		ret := win.GetMessage(&msg, 0, 0, 0)
		switch ret {
		case 0:
			return
		case -1:
			logx.Errorf("实时地图窗口 GetMessage 失败")
			return
		default:
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}
	}
}

func (w *Window) applyPublishedUpdate() {
	w.mu.RLock()
	update := w.update
	w.mu.RUnlock()
	if update.State != "found" {
		return
	}
	w.targetX = float64(update.X)
	w.targetY = float64(update.Y)
	w.lastFoundX = w.targetX
	w.lastFoundY = w.targetY
	if !w.hasFix {
		w.displayX = w.targetX
		w.displayY = w.targetY
		w.hasFix = true
	}
}

func (w *Window) stepAnimation(dt float64, now time.Time) bool {
	needs := false
	targetToolbar := 0.0
	if w.hover && !w.locked {
		targetToolbar = 1
	}
	oldToolbar := w.toolbarAlpha
	w.toolbarAlpha = approach(w.toolbarAlpha, targetToolbar, dt/(float64(toolbarFadeMS)/1000))
	if math.Abs(w.toolbarAlpha-oldToolbar) > 0.001 {
		needs = true
	}
	if !w.hasFix || w.loadingMap {
		w.loadingRot = math.Mod(w.loadingRot+dt*math.Pi*2, math.Pi*2)
		return true
	}

	dx := w.targetX - w.displayX
	dy := w.targetY - w.displayY
	dist := math.Hypot(dx, dy)
	speed := 8.0
	if dist > 800 {
		speed = 14
	}
	k := 1 - math.Exp(-speed*dt)
	w.displayX += dx * k
	w.displayY += dy * k
	if dist > 0.05 {
		needs = true
	}
	zOld := w.zoom
	w.zoom += (w.targetZoom - w.zoom) * (1 - math.Exp(-10*dt))
	if math.Abs(w.zoom-zOld) > 0.001 {
		needs = true
	}
	if !w.draggingMap && !w.lastDragEnd.IsZero() && now.Sub(w.lastDragEnd) > 5*time.Second {
		oldPanX, oldPanY := w.panX, w.panY
		kp := 1 - math.Exp(-4*dt)
		w.panX += (0 - w.panX) * kp
		w.panY += (0 - w.panY) * kp
		if math.Hypot(oldPanX-w.panX, oldPanY-w.panY) > 0.02 {
			needs = true
		}
	}
	return needs
}
