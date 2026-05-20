package mapoverlay

import (
	"math"
	"sync"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"syscall"
	"unsafe"
)

const (
	className  = "LiveMapDirect2DOverlay"
	wmUpdate   = win.WM_APP + 41
	wmSave     = win.WM_APP + 42
	wmResource = win.WM_APP + 43

	hoverTimerID  = 1
	frameTimerID  = 2
	hoverTimerMS  = 80
	frameTimerMS  = 16
	toolbarFadeMS = 140

	hotkeyUnlockID = 0x524f434f
	vkEnd          = 0x23

	borderWidth = 3.0
	cornerR     = 20.0
	edgeGrip    = 14
	minWidth    = 280
	minHeight   = 240

	maNoActivate          = 3
	lwaAlpha              = 0x2
	dwmNCRenderingPolicy  = 2
	dwmNCRenderingEnabled = 2

	wmMouseWheel = 0x020A
	wmSetCursor  = 0x0020

	d2dFactorySingleThreaded = 0
	d2dRenderTargetDefault   = 0
	d2dRenderTargetGDICompat = 2
	d2dFeatureLevelDefault   = 0
	d2dAlphaIgnore           = 3
	d2dAlphaPremultiplied    = 1
	dxgiFormatUnknown        = 0
	dxgiFormatB8G8R8A8UNorm  = 87
	d2dPresentNone           = 0
	d2dAntialiasPerPrimitive = 0
	d2dBitmapInterpLinear    = 1
	dwriteFactoryShared      = 0
	dwriteWeightNormal       = 400
	dwriteStyleNormal        = 0
	dwriteStretchNormal      = 5
	dwriteMeasuringNatural   = 0
	dwriteTextAlignLeading   = 0
	dwriteParaAlignNear      = 0
	dwriteWordWrapNoWrap     = 1
	wicDecodeMetadataCache   = 0
	wicBitmapDitherNone      = 0
	wicPaletteCustom         = 0
	genericRead              = 0x80000000

	minOpacity     = 51
	maxOpacity     = 255
	defaultOpacity = 235
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")
	d2d1     = windows.NewLazySystemDLL("d2d1.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	dwrite   = windows.NewLazySystemDLL("dwrite.dll")
	oleAut   = windows.NewLazySystemDLL("oleaut32.dll")
	wincodec = windows.NewLazySystemDLL("WindowsCodecs.dll")
	current  *Window
	once     sync.Once

	procCreateRoundRectRgn        = windows.NewLazySystemDLL("gdi32.dll").NewProc("CreateRoundRectRgn")
	procSetWindowRgn              = user32.NewProc("SetWindowRgn")
	procSetLayeredWindowAttribute = user32.NewProc("SetLayeredWindowAttributes")
	procDwmSetWindowAttribute     = dwmapi.NewProc("DwmSetWindowAttribute")
	procDwmExtendFrame            = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
	procRegisterHotKey            = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey          = user32.NewProc("UnregisterHotKey")
	procAddFontResourceEx         = windows.NewLazySystemDLL("gdi32.dll").NewProc("AddFontResourceExW")
	procRemoveFontResourceEx      = windows.NewLazySystemDLL("gdi32.dll").NewProc("RemoveFontResourceExW")
	procCoInitializeEx            = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance          = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree             = ole32.NewProc("CoTaskMemFree")
	procD2D1CreateFactory         = d2d1.NewProc("D2D1CreateFactory")
	procDWriteCreateFactory       = dwrite.NewProc("DWriteCreateFactory")
	procWICConvertBitmapSource    = wincodec.NewProc("WICConvertBitmapSource")
	procSysFreeString             = oleAut.NewProc("SysFreeString")

	iidID2D1Factory        = windows.GUID{Data1: 0x06152247, Data2: 0x6f50, Data3: 0x465a, Data4: [8]byte{0x92, 0x45, 0x11, 0x8b, 0xfd, 0x3b, 0x60, 0x07}}
	iidIDWriteFactory      = windows.GUID{Data1: 0xb859ee5a, Data2: 0xd838, Data3: 0x4b5b, Data4: [8]byte{0xa2, 0xe8, 0x1a, 0xdc, 0x7d, 0x93, 0xdb, 0x48}}
	clsidWICFactory        = windows.GUID{Data1: 0xcacaf262, Data2: 0x9370, Data3: 0x4615, Data4: [8]byte{0xa1, 0x3b, 0x9f, 0x55, 0x39, 0xda, 0x4c, 0x0a}}
	iidIWICImagingFactory  = windows.GUID{Data1: 0xec5ec8a9, Data2: 0xc395, Data3: 0x4314, Data4: [8]byte{0x9c, 0x77, 0x54, 0xd7, 0xa9, 0x35, 0xff, 0x70}}
	guidWICPixel32bppPBGRA = windows.GUID{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe, Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x10}}
)

const frPrivate = 0x10

type comObject struct {
	ptr uintptr
}

type d2dColorF struct {
	R float32
	G float32
	B float32
	A float32
}

type d2dPixelFormat struct {
	Format    uint32
	AlphaMode uint32
}

type d2dRenderTargetProps struct {
	Type        uint32
	PixelFormat d2dPixelFormat
	DpiX        float32
	DpiY        float32
	Usage       uint32
	MinLevel    uint32
}

type d2dHwndRenderTargetProps struct {
	Hwnd           win.HWND
	PixelSizeWidth uint32
	PixelSizeH     uint32
	PresentOptions uint32
}

type d2dBitmapProps struct {
	PixelFormat d2dPixelFormat
	DpiX        float32
	DpiY        float32
}

type d2dSizeU struct {
	Width  uint32
	Height uint32
}

type d2dPoint2F struct {
	X float32
	Y float32
}

type d2dRectF struct {
	Left   float32
	Top    float32
	Right  float32
	Bottom float32
}

type d2dRoundedRect struct {
	Rect    d2dRectF
	RadiusX float32
	RadiusY float32
}

type d2dEllipse struct {
	Point   d2dPoint2F
	RadiusX float32
	RadiusY float32
}

type d2dMatrix3x2F struct {
	M11 float32
	M12 float32
	M21 float32
	M22 float32
	DX  float32
	DY  float32
}

type dwmMargins struct {
	Left   int32
	Right  int32
	Top    int32
	Bottom int32
}

type wicRect struct {
	X      int32
	Y      int32
	Width  int32
	Height int32
}

func (o *comObject) call(index int, args ...uintptr) uintptr {
	vtbl := *(**uintptr)(unsafe.Pointer(o.ptr))
	fn := *(*uintptr)(unsafe.Pointer(uintptr(unsafe.Pointer(vtbl)) + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, o.ptr)
	all = append(all, args...)
	ret, _, _ := syscall.SyscallN(fn, all...)
	return ret
}

func (o *comObject) release() {
	if o == nil || o.ptr == 0 {
		return
	}
	o.call(2)
	o.ptr = 0
}

func failed(hr uintptr) bool {
	return int32(hr) < 0
}

func packSizeU(size d2dSizeU) uintptr {
	return uintptr(size.Width) | uintptr(size.Height)<<32
}

func packPoint2F(p d2dPoint2F) uintptr {
	return uintptr(math.Float32bits(p.X)) | uintptr(math.Float32bits(p.Y))<<32
}
