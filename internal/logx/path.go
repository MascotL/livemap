package logx

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procSetDllDirectory = kernel32.NewProc("SetDllDirectoryW")
	dllDirMu            sync.Mutex
	currentDLLDir       string
)

// FindLibrary 查找 DLL 文件的绝对路径。
// 它会按以下顺序查找：
// 1. 可执行文件所在目录下的 libs 文件夹 (bin/libs)
// 2. 可执行文件所在目录
// 3. 当前工作目录下的 libs 文件夹
// 4. 当前工作目录
func FindLibrary(name string) string {
	var candidates []string

	// 1. Based on executable path (Standard deployment)
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(exeDir, "libs", name))
		candidates = append(candidates, filepath.Join(exeDir, name))
	}

	// 2. Based on current working directory (Development environment)
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "libs", name))
		candidates = append(candidates, filepath.Join(cwd, name))

		// Search up to 2 levels (For deep project structures)
		p1 := filepath.Dir(cwd)
		candidates = append(candidates, filepath.Join(p1, "libs", name))
		candidates = append(candidates, filepath.Join(p1, name))

		p2 := filepath.Dir(p1)
		candidates = append(candidates, filepath.Join(p2, "libs", name))
		candidates = append(candidates, filepath.Join(p2, name))
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			RegisterDLLDirectory(filepath.Dir(path))
			return path
		}
	}

	// 如果都没找到，返回原名，依赖系统搜索路径
	return name
}

// RegisterDLLDirectory adds a directory to the process DLL search path.
// Loading a DLL by absolute path does not reliably make Windows search that
// same directory for its transitive dependencies, so bundled native libraries
// need their containing directory registered before LoadLibrary runs.
func RegisterDLLDirectory(dir string) bool {
	if dir == "" || dir == "." {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return false
	}

	dllDirMu.Lock()
	if currentDLLDir == abs {
		dllDirMu.Unlock()
		return true
	}
	dllDirMu.Unlock()

	ptr, err := syscall.UTF16PtrFromString(abs)
	if err != nil {
		return false
	}
	ok, _, _ := procSetDllDirectory.Call(uintptr(unsafe.Pointer(ptr)))
	if ok == 0 {
		return false
	}

	dllDirMu.Lock()
	currentDLLDir = abs
	dllDirMu.Unlock()
	return true
}
