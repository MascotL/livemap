package capture

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"github.com/mascotl/livemap/internal/logx"
)

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	procEnumWindows          = user32.NewProc("EnumWindows")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
)

const (
	BackendAll         = ""
	BackendWGC         = "wgc"
	BackendPrintWindow = "printwindow"
	BackendDesktop     = "desktop"
	DefaultFPS         = 30

	childReadyPrefix  = "BACKEND_READY:"
	childReadyTimeout = 8 * time.Second
	minFPS            = 1
	maxFPS            = 240
)

type windowTarget struct {
	hwnd  win.HWND
	title string
}

type backendRunner interface {
	Name() string
	Capture(win.HWND) (*Frame, error)
	Close()
}

func StartSupervisor(windowName, backend string, fps int) error {
	return StartSupervisorTo(windowName, backend, fps, os.Stdout)
}

func StartSupervisorTo(windowName, backend string, fps int, output io.Writer) error {
	return StartSupervisorToContext(context.Background(), windowName, backend, fps, output)
}

func StartSupervisorToContext(ctx context.Context, windowName, backend string, fps int, output io.Writer) error {
	backends, err := orderedBackends(strings.TrimSpace(strings.ToLower(backend)))
	if err != nil {
		return err
	}
	fps = normalizeFPS(fps)

	for i, candidate := range backends {
		if err := ctx.Err(); err != nil {
			return err
		}
		logx.Infof("尝试第%d个截取方式: %s", i+1, displayBackendName(candidate))
		runErr := superviseBackend(ctx, windowName, candidate, fps, output)
		if runErr == nil {
			return nil
		}

		logx.Warnf("第%d个截取方式失败: %s - %v", i+1, displayBackendName(candidate), runErr)
	}

	return fmt.Errorf("所有截取方式都失败了")
}

func RunBackend(windowName, backend string, fps int) error {
	return RunBackendTo(windowName, backend, fps, os.Stdout)
}

func RunBackendTo(windowName, backend string, fps int, output io.Writer) error {
	return RunBackendToContext(context.Background(), windowName, backend, fps, output)
}

func RunBackendToContext(ctx context.Context, windowName, backend string, fps int, output io.Writer) error {
	target, err := resolveWindowTarget(windowName)
	if err != nil {
		return err
	}

	runner, err := buildSingleBackend(strings.TrimSpace(strings.ToLower(backend)))
	if err != nil {
		return err
	}
	defer runner.Close()
	fps = normalizeFPS(fps)

	logx.Infof("已找到窗口: %s (HWND=%d)", target.title, target.hwnd)
	logx.Infof("%s 工作进程已启动, FPS=%d", runner.Name(), fps)

	ticker := time.NewTicker(time.Second / time.Duration(fps))
	defer ticker.Stop()

	ready := false
	frameID := uint64(0)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		frame, err := runner.Capture(target.hwnd)
		if err != nil {
			return err
		}

		if !ready {
			logx.Infof("%s%s", childReadyPrefix, runner.Name())
			ready = true
		}

		frameID++
		frame.FrameID = frameID
		if err := WriteFrame(output, frame); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func orderedBackends(backend string) ([]string, error) {
	switch backend {
	case BackendAll:
		return []string{BackendWGC, BackendPrintWindow, BackendDesktop}, nil
	case BackendWGC, BackendPrintWindow, BackendDesktop:
		return []string{backend}, nil
	default:
		return nil, fmt.Errorf("不支持的截取工具: %q，可选值: wgc, printwindow, desktop", backend)
	}
}

func buildSingleBackend(backend string) (backendRunner, error) {
	switch backend {
	case BackendWGC:
		return newWGCCapturer(), nil
	case BackendPrintWindow:
		return &printWindowCapturer{}, nil
	case BackendDesktop:
		return &desktopCapturer{}, nil
	default:
		return nil, fmt.Errorf("不支持的截取工具: %q", backend)
	}
}

func displayBackendName(backend string) string {
	switch backend {
	case BackendWGC:
		return "Windows.Graphics.Capture"
	case BackendPrintWindow:
		return "PrintWindow"
	case BackendDesktop:
		return "DesktopCopy"
	default:
		return backend
	}
}

func superviseBackend(ctx context.Context, windowName, backend string, fps int, output io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "--worker", windowName, backend, strconv.Itoa(fps))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	readyCh := make(chan string, 1)
	waitCh := make(chan error, 1)
	copyErrCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go proxyBinaryStream(ctx, stdout, output, copyErrCh)
	go streamStderr(stderr, readyCh)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case ready := <-readyCh:
		logx.Infof("子进程已稳定运行: %s", ready)
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-waitCh
			return ctx.Err()
		case err = <-waitCh:
		}
		if err != nil {
			return fmt.Errorf("子进程异常退出: %w", err)
		}
		return fmt.Errorf("子进程正常退出")
	case err := <-copyErrCh:
		_ = cmd.Process.Kill()
		<-waitCh
		return fmt.Errorf("转发截图流失败: %w", err)
	case err := <-waitCh:
		if err != nil {
			return fmt.Errorf("子进程启动失败: %w", err)
		}
		return fmt.Errorf("子进程未进入就绪状态就退出了")
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-waitCh
		return ctx.Err()
	case <-time.After(childReadyTimeout):
		_ = cmd.Process.Kill()
		<-waitCh
		return fmt.Errorf("等待子进程就绪超时")
	}
}

func proxyBinaryStream(ctx context.Context, r io.Reader, output io.Writer, errCh chan<- error) {
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(output, r)
		done <- err
	}()

	select {
	case <-ctx.Done():
		return
	case err := <-done:
		if err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	}
}

func streamStderr(r io.Reader, readyCh chan<- string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(os.Stderr, line)
		if !strings.Contains(line, childReadyPrefix) {
			logx.Raw(line)
		}
		if idx := strings.Index(line, childReadyPrefix); idx >= 0 {
			select {
			case readyCh <- strings.TrimSpace(line[idx+len(childReadyPrefix):]):
			default:
			}
		}
	}
}

func normalizeFPS(fps int) int {
	switch {
	case fps < minFPS:
		return DefaultFPS
	case fps > maxFPS:
		return maxFPS
	default:
		return fps
	}
}

func resolveWindowTarget(input string) (*windowTarget, error) {
	query := strings.TrimSpace(input)
	if query == "" {
		hwnd := win.GetForegroundWindow()
		if hwnd == 0 {
			return nil, fmt.Errorf("当前没有前台窗口")
		}
		return &windowTarget{hwnd: hwnd, title: getWindowTitle(hwnd)}, nil
	}

	if hwnd, ok := parseHWND(query); ok {
		if hwnd == 0 {
			return nil, fmt.Errorf("HWND 不能为 0")
		}
		return &windowTarget{hwnd: hwnd, title: getWindowTitle(hwnd)}, nil
	}

	titlePtr, err := syscall.UTF16PtrFromString(query)
	if err == nil {
		if hwnd := win.FindWindow(nil, titlePtr); hwnd != 0 {
			return &windowTarget{hwnd: hwnd, title: getWindowTitle(hwnd)}, nil
		}
	}

	matches := enumerateWindows(query)
	if len(matches) > 0 {
		return &matches[0], nil
	}

	return nil, fmt.Errorf("未找到窗口: %q", query)
}

func parseHWND(input string) (win.HWND, bool) {
	base := 10
	raw := input
	if strings.HasPrefix(strings.ToLower(raw), "0x") {
		base = 16
		raw = raw[2:]
	}

	value, err := strconv.ParseUint(raw, base, 64)
	if err != nil {
		return 0, false
	}
	return win.HWND(value), true
}

func enumerateWindows(query string) []windowTarget {
	lowered := strings.ToLower(query)
	exact := make([]windowTarget, 0)
	contains := make([]windowTarget, 0)

	callback := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		handle := win.HWND(hwnd)
		if !win.IsWindowVisible(handle) {
			return 1
		}

		title := strings.TrimSpace(getWindowTitle(handle))
		if title == "" {
			return 1
		}

		lowerTitle := strings.ToLower(title)
		target := windowTarget{hwnd: handle, title: title}

		switch {
		case lowerTitle == lowered:
			exact = append(exact, target)
		case strings.Contains(lowerTitle, lowered):
			contains = append(contains, target)
		}

		return 1
	})

	procEnumWindows.Call(callback, 0)
	return append(exact, contains...)
}

func getWindowTitle(hwnd win.HWND) string {
	length, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	if length == 0 {
		return ""
	}

	buf := make([]uint16, length+1)
	procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(length+1))
	return syscall.UTF16ToString(buf)
}
