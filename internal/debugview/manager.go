package debugview

import (
	"context"
	"sync"

	"github.com/mascotl/livemap/internal/logx"
	"github.com/mascotl/livemap/internal/view"
)

type Manager struct {
	mu       sync.Mutex
	frames   chan view.DebugFrame
	cancel   context.CancelFunc
	open     bool
	onClosed func()
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Open(parent context.Context, onClosed func()) bool {
	m.mu.Lock()
	if m.open {
		m.mu.Unlock()
		return false
	}

	ctx, cancel := context.WithCancel(parent)
	frames := make(chan view.DebugFrame, 2)
	m.frames = frames
	m.cancel = cancel
	m.open = true
	m.onClosed = onClosed
	m.mu.Unlock()

	go func() {
		err := view.RunDebugContext(ctx, frames, "LiveMap Debug Viewer")
		if err != nil && ctx.Err() == nil {
			logx.Warnf("debug 检视窗口已关闭: %v", err)
		}

		m.mu.Lock()
		if m.frames == frames {
			m.frames = nil
			m.cancel = nil
			m.open = false
			close(frames)
		}
		callback := m.onClosed
		m.mu.Unlock()

		if callback != nil {
			callback()
		}
	}()

	return true
}

func (m *Manager) Close() {
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (m *Manager) IsOpen() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.open
}

func (m *Manager) Publish(frame view.DebugFrame) {
	m.mu.Lock()
	frames := m.frames
	m.mu.Unlock()

	if frames == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	select {
	case frames <- frame:
	default:
	}
}
