package mapmatch

import (
	"fmt"
	"image"
	"syscall"
	"unsafe"

	"github.com/mascotl/livemap/internal/appconfig"
	"github.com/mascotl/livemap/internal/logx"
)

type nativeMatcher struct {
	dll           *syscall.LazyDLL
	procCreate    *syscall.LazyProc
	procDestroy   *syscall.LazyProc
	procGlobal    *syscall.LazyProc
	procLocal     *syscall.LazyProc
	handle        uintptr
	globalWorkers int
	localWorkers  int
	threshold     float64
}

type nativeResult struct {
	Found    int32
	TimedOut int32
	X        int32
	Y        int32
	Score    float64
}

func newNativeMatcher(world *image.RGBA, cfg appconfig.MapMatching) (*nativeMatcher, error) {
	dllPath := logx.FindLibrary("mapmatch.dll")
	dll := syscall.NewLazyDLL(dllPath)
	m := &nativeMatcher{
		dll:           dll,
		procCreate:    dll.NewProc("MapMatchCreate"),
		procDestroy:   dll.NewProc("MapMatchDestroy"),
		procGlobal:    dll.NewProc("MapMatchGlobal"),
		procLocal:     dll.NewProc("MapMatchLocal"),
		globalWorkers: max(1, cfg.GlobalWorkers),
		localWorkers:  max(1, cfg.LocalWorkers),
		threshold:     cfg.MatchThreshold,
	}
	if err := m.procCreate.Find(); err != nil {
		return nil, fmt.Errorf("加载 mapmatch.dll 失败: path=%s err=%w", dllPath, err)
	}
	if err := m.procGlobal.Find(); err != nil {
		return nil, fmt.Errorf("加载 mapmatch.dll 失败: path=%s err=%w", dllPath, err)
	}
	if err := m.procLocal.Find(); err != nil {
		return nil, fmt.Errorf("加载 mapmatch.dll 失败: path=%s err=%w", dllPath, err)
	}
	if err := m.procDestroy.Find(); err != nil {
		return nil, fmt.Errorf("加载 mapmatch.dll 失败: path=%s err=%w", dllPath, err)
	}

	handle, _, callErr := m.procCreate.Call(
		uintptr(unsafe.Pointer(&world.Pix[0])),
		uintptr(world.Rect.Dx()),
		uintptr(world.Rect.Dy()),
		uintptr(world.Stride),
	)
	if handle == 0 {
		return nil, fmt.Errorf("MapMatchCreate 失败: %v", callErr)
	}
	m.handle = handle
	return m, nil
}

func (m *nativeMatcher) close() {
	if m == nil || m.handle == 0 {
		return
	}
	m.procDestroy.Call(m.handle)
	m.handle = 0
}

func (m *nativeMatcher) matchGlobal(minimap *image.RGBA, timeoutMS int) Result {
	if m == nil || m.handle == 0 {
		return Result{}
	}
	var out nativeResult
	ok, _, callErr := m.procGlobal.Call(
		m.handle,
		uintptr(unsafe.Pointer(&minimap.Pix[0])),
		uintptr(minimap.Rect.Dx()),
		uintptr(minimap.Rect.Dy()),
		uintptr(minimap.Stride),
		uintptr(m.globalWorkers),
		uintptr(int(m.threshold*1000000.0+0.5)),
		uintptr(timeoutMS),
		uintptr(unsafe.Pointer(&out)),
	)
	if ok == 0 {
		_ = callErr
		return Result{}
	}
	return Result{
		Found:    out.Found != 0,
		X:        int(out.X),
		Y:        int(out.Y),
		Score:    out.Score,
		Global:   true,
		TimedOut: out.TimedOut != 0,
	}
}

func (m *nativeMatcher) matchLocal(minimap *image.RGBA, centerX, centerY, radius, timeoutMS int) Result {
	if m == nil || m.handle == 0 {
		return Result{}
	}
	var out nativeResult
	ok, _, callErr := m.procLocal.Call(
		m.handle,
		uintptr(unsafe.Pointer(&minimap.Pix[0])),
		uintptr(minimap.Rect.Dx()),
		uintptr(minimap.Rect.Dy()),
		uintptr(minimap.Stride),
		uintptr(centerX),
		uintptr(centerY),
		uintptr(radius),
		uintptr(m.localWorkers),
		uintptr(int(m.threshold*1000000.0+0.5)),
		uintptr(timeoutMS),
		uintptr(unsafe.Pointer(&out)),
	)
	if ok == 0 {
		_ = callErr
		return Result{}
	}
	return Result{
		Found:    out.Found != 0,
		X:        int(out.X),
		Y:        int(out.Y),
		Score:    out.Score,
		Global:   false,
		TimedOut: out.TimedOut != 0,
	}
}
