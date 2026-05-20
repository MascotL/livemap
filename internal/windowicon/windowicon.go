package windowicon

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"

	"github.com/lxn/win"
	appassets "github.com/mascotl/livemap/assets"
)

const (
	iconSmall = 0
	iconBig   = 1
)

var overlayIconPath string

func SetOverlay(hwnd win.HWND) {
	path, err := materializeOverlayIcon()
	if err != nil {
		return
	}
	ptr := syscall.StringToUTF16Ptr(path)
	small := win.LoadImage(0, ptr, win.IMAGE_ICON, 16, 16, win.LR_LOADFROMFILE)
	if small != 0 {
		win.SendMessage(hwnd, win.WM_SETICON, uintptr(iconSmall), uintptr(small))
	}
	large := win.LoadImage(0, ptr, win.IMAGE_ICON, 32, 32, win.LR_LOADFROMFILE)
	if large != 0 {
		win.SendMessage(hwnd, win.WM_SETICON, uintptr(iconBig), uintptr(large))
	}
}

func materializeOverlayIcon() (string, error) {
	if overlayIconPath != "" {
		if _, err := os.Stat(overlayIconPath); err == nil {
			return overlayIconPath, nil
		}
	}
	data, err := appassets.FS.ReadFile("overlay-logo.ico")
	if err != nil {
		return "", err
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "livemap", "icons")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "overlay-logo.ico")
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		overlayIconPath = path
		return path, nil
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	overlayIconPath = path
	return path, nil
}
