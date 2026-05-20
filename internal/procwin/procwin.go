package procwin

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
)

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procEnumWindows          = user32.NewProc("EnumWindows")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procGetWindowThreadPID   = user32.NewProc("GetWindowThreadProcessId")
	procGetClientRect        = user32.NewProc("GetClientRect")
	procClientToScreen       = user32.NewProc("ClientToScreen")
	procCreateSnapshot       = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW      = kernel32.NewProc("Process32FirstW")
	procProcess32NextW       = kernel32.NewProc("Process32NextW")
	procCloseHandle          = kernel32.NewProc("CloseHandle")
)

const (
	th32csSnapProcess = 0x00000002
	maxPath           = 260
	invalidHandle     = ^uintptr(0)
)

type ProcessInfo struct {
	PID     uint32
	ExeName string
}

type WindowInfo struct {
	HWND      win.HWND
	PID       uint32
	ExeName   string
	Title     string
	IsVisible bool
}

type Rect struct {
	X int
	Y int
	W int
	H int
}

type processEntry32 struct {
	Size              uint32
	CntUsage          uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	Threads           uint32
	ParentProcessID   uint32
	PriorityClassBase int32
	Flags             uint32
	ExeFile           [maxPath]uint16
}

func FindProcessesByName(name string) ([]ProcessInfo, error) {
	target := strings.TrimSpace(strings.ToLower(name))
	if target == "" {
		return nil, fmt.Errorf("进程名不能为空")
	}

	all, err := listProcesses()
	if err != nil {
		return nil, err
	}

	matches := make([]ProcessInfo, 0)
	for _, proc := range all {
		if strings.EqualFold(proc.ExeName, target) {
			matches = append(matches, proc)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("未找到进程: %q", name)
	}
	return matches, nil
}

func FindWindowsByProcessName(name string) ([]WindowInfo, error) {
	processes, err := FindProcessesByName(name)
	if err != nil {
		return nil, err
	}

	pidToName := make(map[uint32]string, len(processes))
	for _, proc := range processes {
		pidToName[proc.PID] = proc.ExeName
	}

	return enumerateWindows(func(hwnd win.HWND, pid uint32, title string, visible bool) bool {
		_, ok := pidToName[pid]
		return ok
	}, pidToName)
}

func FindMainWindowByProcessName(name string) (*WindowInfo, error) {
	windows, err := FindWindowsByProcessName(name)
	if err != nil {
		return nil, err
	}

	for _, item := range windows {
		if item.IsVisible && strings.TrimSpace(item.Title) != "" {
			copy := item
			return &copy, nil
		}
	}
	if len(windows) > 0 {
		copy := windows[0]
		return &copy, nil
	}
	return nil, fmt.Errorf("进程 %q 没有关联窗口", name)
}

func FindWindowHandlesByProcessName(name string) ([]win.HWND, error) {
	windows, err := FindWindowsByProcessName(name)
	if err != nil {
		return nil, err
	}

	handles := make([]win.HWND, 0, len(windows))
	for _, item := range windows {
		handles = append(handles, item.HWND)
	}
	return handles, nil
}

func ClientRectOnScreen(hwnd win.HWND) (Rect, error) {
	var rect win.RECT
	ret, _, callErr := procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))
	if ret == 0 {
		if callErr != syscall.Errno(0) && callErr != nil {
			return Rect{}, fmt.Errorf("GetClientRect 失败: %v", callErr)
		}
		return Rect{}, fmt.Errorf("GetClientRect 失败")
	}

	pt := struct {
		X int32
		Y int32
	}{}
	ret, _, callErr = procClientToScreen.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pt)))
	if ret == 0 {
		if callErr != syscall.Errno(0) && callErr != nil {
			return Rect{}, fmt.Errorf("ClientToScreen 失败: %v", callErr)
		}
		return Rect{}, fmt.Errorf("ClientToScreen 失败")
	}

	return Rect{
		X: int(pt.X),
		Y: int(pt.Y),
		W: int(rect.Right - rect.Left),
		H: int(rect.Bottom - rect.Top),
	}, nil
}

func listProcesses() ([]ProcessInfo, error) {
	snapshot, _, callErr := procCreateSnapshot.Call(th32csSnapProcess, 0)
	if snapshot == invalidHandle {
		if callErr != syscall.Errno(0) && callErr != nil {
			return nil, fmt.Errorf("CreateToolhelp32Snapshot 失败: %v", callErr)
		}
		return nil, fmt.Errorf("CreateToolhelp32Snapshot 失败")
	}
	defer procCloseHandle.Call(snapshot)

	entry := processEntry32{Size: uint32(unsafe.Sizeof(processEntry32{}))}
	ret, _, callErr := procProcess32FirstW.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		if callErr != syscall.Errno(0) && callErr != nil {
			return nil, fmt.Errorf("Process32FirstW 失败: %v", callErr)
		}
		return nil, fmt.Errorf("Process32FirstW 失败")
	}

	processes := make([]ProcessInfo, 0, 64)
	for {
		processes = append(processes, ProcessInfo{
			PID:     entry.ProcessID,
			ExeName: syscall.UTF16ToString(entry.ExeFile[:]),
		})

		entry.Size = uint32(unsafe.Sizeof(processEntry32{}))
		ret, _, _ = procProcess32NextW.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
		if ret == 0 {
			break
		}
	}

	return processes, nil
}

func enumerateWindows(match func(hwnd win.HWND, pid uint32, title string, visible bool) bool, pidToName map[uint32]string) ([]WindowInfo, error) {
	results := make([]WindowInfo, 0, 16)

	callback := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		handle := win.HWND(hwnd)
		title := getWindowTitle(handle)
		visible := win.IsWindowVisible(handle)
		pid := windowPID(handle)

		if !match(handle, pid, title, visible) {
			return 1
		}

		results = append(results, WindowInfo{
			HWND:      handle,
			PID:       pid,
			ExeName:   pidToName[pid],
			Title:     title,
			IsVisible: visible,
		})
		return 1
	})

	ret, _, callErr := procEnumWindows.Call(callback, 0)
	if ret == 0 {
		if callErr != syscall.Errno(0) && callErr != nil {
			return nil, fmt.Errorf("EnumWindows 失败: %v", callErr)
		}
		return nil, fmt.Errorf("EnumWindows 失败")
	}

	return results, nil
}

func windowPID(hwnd win.HWND) uint32 {
	var pid uint32
	procGetWindowThreadPID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
	return pid
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
