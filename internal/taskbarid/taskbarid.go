package taskbarid

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const (
	OverlayGroupID = "mascotl.livemap.overlay"
	vtLPWSTR       = 31
)

var (
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procSHGetPropertyStoreForWindow = shell32.NewProc("SHGetPropertyStoreForWindow")

	iidIPropertyStore  = windows.GUID{Data1: 0x886d8eeb, Data2: 0x8cf2, Data3: 0x4446, Data4: [8]byte{0x8d, 0x02, 0xcd, 0xba, 0x1d, 0xbd, 0xcf, 0x99}}
	pkeyAppUserModelID = propertyKey{
		Fmtid: windows.GUID{Data1: 0x9f4c2855, Data2: 0x9f79, Data3: 0x4b39, Data4: [8]byte{0xa8, 0xd0, 0xe1, 0xd4, 0x2d, 0xe1, 0xd5, 0xf3}},
		Pid:   5,
	}
)

type propertyKey struct {
	Fmtid windows.GUID
	Pid   uint32
}

type propVariant struct {
	VT        uint16
	Reserved1 uint16
	Reserved2 uint16
	Reserved3 uint16
	Value     uintptr
	Reserved4 uintptr
}

type propertyStore struct {
	ptr uintptr
}

func Set(hwnd win.HWND, appID string) error {
	if hwnd == 0 || appID == "" {
		return nil
	}
	var storePtr uintptr
	hr, _, _ := procSHGetPropertyStoreForWindow.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&iidIPropertyStore)),
		uintptr(unsafe.Pointer(&storePtr)),
	)
	if failed(hr) {
		return fmt.Errorf("SHGetPropertyStoreForWindow 失败: 0x%x", hr)
	}
	store := &propertyStore{ptr: storePtr}
	defer store.release()

	value := syscall.StringToUTF16Ptr(appID)
	pv := propVariant{VT: vtLPWSTR, Value: uintptr(unsafe.Pointer(value))}
	if hr := store.call(6, uintptr(unsafe.Pointer(&pkeyAppUserModelID)), uintptr(unsafe.Pointer(&pv))); failed(hr) {
		return fmt.Errorf("IPropertyStore.SetValue 失败: 0x%x", hr)
	}
	if hr := store.call(7); failed(hr) {
		return fmt.Errorf("IPropertyStore.Commit 失败: 0x%x", hr)
	}
	return nil
}

func (s *propertyStore) call(index int, args ...uintptr) uintptr {
	vtbl := *(**uintptr)(unsafe.Pointer(s.ptr))
	fn := *(*uintptr)(unsafe.Pointer(uintptr(unsafe.Pointer(vtbl)) + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, s.ptr)
	all = append(all, args...)
	ret, _, _ := syscall.SyscallN(fn, all...)
	return ret
}

func (s *propertyStore) release() {
	if s == nil || s.ptr == 0 {
		return
	}
	s.call(2)
	s.ptr = 0
}

func failed(hr uintptr) bool {
	return int32(hr) < 0
}
