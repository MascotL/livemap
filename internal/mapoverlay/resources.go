package mapoverlay

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	appassets "github.com/mascotl/livemap/assets"
	"github.com/mascotl/livemap/internal/logx"
	"github.com/mascotl/livemap/internal/mapbundle"
	"golang.org/x/sys/windows"
)

func newRenderer(hwnd win.HWND, opts Options) (*renderer, error) {
	logx.Infof("实时地图 renderer 开始创建: hwnd=0x%x map=%s categories=%d markers=%d", uintptr(hwnd), resolvePath(opts.WorldMapPath), len(opts.Resources.Categories), len(opts.Resources.Markers))
	r := &renderer{hwnd: hwnd, brushes: make(map[string]*comObject), icons: make(map[string]*comObject)}
	r.loadPrivateFonts()
	if err := r.createFactories(); err != nil {
		r.release()
		return nil, err
	}
	if err := r.createTarget(clientSize(hwnd)); err != nil {
		r.release()
		return nil, err
	}
	if err := r.createDeviceResources(); err != nil {
		r.release()
		return nil, err
	}
	r.loadIcons()
	if opts.WorldMapPath != "" {
		if err := r.loadMap(opts.WorldMapPath); err != nil {
			r.release()
			return nil, err
		}
		logx.Infof("实时地图底图已加载到 Direct2D: path=%s size=%dx%d", resolvePath(opts.WorldMapPath), r.mapWidth, r.mapHeight)
	}
	r.loadPinResources(opts.Resources.Categories)
	logx.Infof("实时地图 renderer 创建完成: hwnd=0x%x target=0x%x", uintptr(hwnd), r.target.ptr)
	return r, nil
}

func (r *renderer) createFactories() error {
	var factory uintptr
	hr, _, _ := procD2D1CreateFactory.Call(d2dFactorySingleThreaded, uintptr(unsafe.Pointer(&iidID2D1Factory)), 0, uintptr(unsafe.Pointer(&factory)))
	if failed(hr) {
		return fmt.Errorf("D2D1CreateFactory 失败: 0x%x", hr)
	}
	r.factory = &comObject{ptr: factory}

	var wic uintptr
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidWICFactory)),
		0,
		1,
		uintptr(unsafe.Pointer(&iidIWICImagingFactory)),
		uintptr(unsafe.Pointer(&wic)),
	)
	if failed(hr) {
		return fmt.Errorf("创建 WIC factory 失败: 0x%x", hr)
	}
	r.wic = &comObject{ptr: wic}

	var dw uintptr
	hr, _, _ = procDWriteCreateFactory.Call(dwriteFactoryShared, uintptr(unsafe.Pointer(&iidIDWriteFactory)), uintptr(unsafe.Pointer(&dw)))
	if failed(hr) {
		return fmt.Errorf("DWriteCreateFactory 失败: 0x%x", hr)
	}
	r.dwrite = &comObject{ptr: dw}
	return nil
}

func (r *renderer) loadPrivateFonts() {
	for _, name := range []string{
		"Inter_18pt-Medium.ttf",
		"JetBrainsMono-Regular.ttf",
		"SourceHanSansCN-Medium.otf",
	} {
		path, err := materializeEmbeddedFont(name)
		if err != nil {
			logx.Warnf("释放内置字体失败: font=%s err=%v", name, err)
			continue
		}
		ptr, err := windows.UTF16PtrFromString(path)
		if err != nil {
			continue
		}
		added, _, _ := procAddFontResourceEx.Call(uintptr(unsafe.Pointer(ptr)), frPrivate, 0)
		if added == 0 {
			logx.Warnf("私有字体加载失败: %s", path)
			continue
		}
		r.fonts = append(r.fonts, path)
		logx.Infof("私有字体已加载: %s", path)
	}
}

func materializeEmbeddedFont(name string) (string, error) {
	data, err := appassets.FS.ReadFile(filepath.ToSlash(filepath.Join("fonts", name)))
	if err != nil {
		return "", err
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "livemap", "fonts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return path, nil
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
}

func (r *renderer) unloadPrivateFonts() {
	for _, path := range r.fonts {
		ptr, err := windows.UTF16PtrFromString(path)
		if err != nil {
			continue
		}
		procRemoveFontResourceEx.Call(uintptr(unsafe.Pointer(ptr)), frPrivate, 0)
	}
	r.fonts = nil
}

func (r *renderer) createTarget(size image.Point) error {
	if size.X <= 0 || size.Y <= 0 {
		size = image.Pt(1, 1)
	}
	logx.Infof("实时地图 Direct2D target 开始创建: size=%dx%d", size.X, size.Y)
	props := d2dRenderTargetProps{
		Type:        d2dRenderTargetDefault,
		PixelFormat: d2dPixelFormat{Format: dxgiFormatUnknown, AlphaMode: d2dAlphaPremultiplied},
		DpiX:        0,
		DpiY:        0,
		Usage:       d2dRenderTargetGDICompat,
		MinLevel:    d2dFeatureLevelDefault,
	}
	hwndProps := d2dHwndRenderTargetProps{
		Hwnd:           r.hwnd,
		PixelSizeWidth: uint32(size.X),
		PixelSizeH:     uint32(size.Y),
		PresentOptions: d2dPresentNone,
	}
	var target uintptr
	hr := r.factory.call(14, uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&hwndProps)), uintptr(unsafe.Pointer(&target)))
	if failed(hr) {
		return fmt.Errorf("CreateHwndRenderTarget 失败: 0x%x", hr)
	}
	r.target = &comObject{ptr: target}
	logx.Infof("实时地图 Direct2D HWND target 已创建: target=0x%x", target)
	r.target.call(32, d2dAntialiasPerPrimitive)
	return nil
}

func (r *renderer) createDeviceResources() error {
	colors := map[string]d2dColorF{
		"bg":              colorF(26, 26, 26, 0.96),
		"border":          colorF(255, 255, 255, 0.98),
		"shadow":          colorF(0, 0, 0, 0.22),
		"shadow2":         colorF(255, 255, 255, 0.16),
		"innerShadow":     colorF(0, 0, 0, 0.20),
		"innerShadowSoft": colorF(0, 0, 0, 0.10),
		"marker":          colorF(17, 143, 238, 1),
		"markerOuter":     colorF(245, 252, 255, 0.98),
		"markerLost":      colorF(135, 144, 155, 0.96),
		"pinBg":           colorF(15, 42, 77, 0.96),
		"pinBorder":       colorF(255, 255, 255, 0.98),
		"pill":            colorF(12, 17, 26, 0.84),
		"pillEdge":        colorF(145, 208, 255, 0.28),
		"import":          colorF(51, 51, 51, 1),
		"importHover":     colorF(68, 68, 68, 1),
		"importPress":     colorF(38, 38, 38, 1),
		"importEdge":      colorF(85, 85, 85, 1),
		"importEdgeHover": colorF(119, 119, 119, 1),
		"button":          colorF(31, 41, 55, 0.78),
		"hover":           colorF(66, 82, 108, 0.86),
		"active":          colorF(20, 133, 230, 0.96),
		"text":            colorF(235, 245, 255, 0.98),
		"muted":           colorF(180, 190, 202, 0.95),
		"green":           colorF(48, 220, 132, 1),
		"yellow":          colorF(245, 190, 60, 1),
		"red":             colorF(255, 86, 86, 1),
		"logBg":           colorF(0, 0, 0, 0.55),
	}
	for name, c := range colors {
		b, err := r.createBrush(c)
		if err != nil {
			return err
		}
		r.brushes[name] = b
	}
	ui, err := r.createTextFormat("Inter 18pt Medium", 14)
	if err != nil {
		ui, err = r.createTextFormat("Source Han Sans CN Medium", 14)
	}
	if err != nil {
		ui, err = r.createTextFormat("Microsoft YaHei UI", 14)
		if err != nil {
			return err
		}
	}
	logFmt, err := r.createTextFormat("JetBrains Mono", 14)
	if err != nil {
		logFmt, err = r.createTextFormat("Consolas", 14)
		if err != nil {
			return err
		}
	}
	r.textUI = ui
	if small, err := r.createTextFormat("Inter 18pt Medium", 12); err == nil {
		r.textSmall = small
	} else if small, err := r.createTextFormat("Microsoft YaHei UI", 12); err == nil {
		r.textSmall = small
	}
	r.textLog = logFmt
	r.logFont = createGdiFont("JetBrains Mono", 15, win.FW_NORMAL)
	return nil
}

func (r *renderer) createTextFormat(familyName string, size float32) (*comObject, error) {
	var fmtPtr uintptr
	family := syscall.StringToUTF16Ptr(familyName)
	locale := syscall.StringToUTF16Ptr("zh-CN")
	hr := r.dwrite.call(15,
		uintptr(unsafe.Pointer(family)),
		0,
		dwriteWeightNormal,
		dwriteStyleNormal,
		dwriteStretchNormal,
		uintptr(math.Float32bits(size)),
		uintptr(unsafe.Pointer(locale)),
		uintptr(unsafe.Pointer(&fmtPtr)),
	)
	if failed(hr) {
		return nil, fmt.Errorf("CreateTextFormat(%s) 失败: 0x%x", familyName, hr)
	}
	format := &comObject{ptr: fmtPtr}
	format.call(3, dwriteTextAlignLeading)
	format.call(4, dwriteParaAlignNear)
	format.call(5, dwriteWordWrapNoWrap)
	return format, nil
}

func (r *renderer) createBrush(c d2dColorF) (*comObject, error) {
	var brush uintptr
	hr := r.target.call(8, uintptr(unsafe.Pointer(&c)), 0, uintptr(unsafe.Pointer(&brush)))
	if failed(hr) {
		return nil, fmt.Errorf("CreateSolidColorBrush 失败: 0x%x", hr)
	}
	return &comObject{ptr: brush}, nil
}

func (r *renderer) loadMap(path string) error {
	resolved := resolvePath(path)
	r.mapPath = resolved
	if strings.EqualFold(filepath.Ext(resolved), ".map") {
		bmp, w, h, err := r.loadMapWithGoImage(resolved)
		if err != nil {
			return err
		}
		r.mapBitmap = bmp
		r.mapWidth = w
		r.mapHeight = h
		return nil
	}
	bmp, w, h, err := r.loadMapWithWIC(resolved)
	if err == nil {
		r.mapBitmap = bmp
		r.mapWidth = w
		r.mapHeight = h
		return nil
	}
	logx.Warnf("WIC 加载地图失败，回退 Go 解码: %v", err)
	bmp, w, h, err = r.loadMapWithGoImage(resolved)
	if err != nil {
		return err
	}
	r.mapBitmap = bmp
	r.mapWidth = w
	r.mapHeight = h
	return nil
}

func (r *renderer) loadIcons() {
	iconFiles := map[string]string{
		"move":     "mdi--arrow-all.png",
		"pin":      "mdi--pin.png",
		"pin-off":  "mdi--pin-outline.png",
		"alpha":    "mdi--opacity.png",
		"map":      "mdi--map.png",
		"pins":     "mdi--map-marker.png",
		"map-plus": "mdi--map-plus.png",
		"plus":     "mdi--plus.png",
		"check":    "mdi--check.png",
		"question": "mdi--question-mark.png",
		"log":      "mdi--file-document-box.png",
		"log-off":  "mdi--file-document-box-outline.png",
		"lock":     "mdi--lock.png",
		"lock-off": "mdi--lock-outline.png",
		"back":     "mdi--chevron-left.png",
		"loading":  "mdi--loading.png",
		"close":    "mdi--power.png",
	}
	for id, name := range iconFiles {
		bmp, err := r.loadIconBitmap(filepath.Join("assets", "icons", name))
		if err != nil {
			logx.Warnf("加载实时地图 PNG 图标失败: icon=%s err=%v", name, err)
			continue
		}
		r.icons[id] = bmp
	}
}

func (r *renderer) loadPinResources(categories []PinCategory) {
	for name, icon := range r.pinIcons {
		if icon != nil {
			icon.release()
		}
		delete(r.pinIcons, name)
	}
	if r.pinIcons == nil {
		r.pinIcons = make(map[string]*comObject)
	}
	for _, c := range categories {
		data := c.Thumb
		if len(data) == 0 {
			data = c.Image
		}
		if len(data) == 0 {
			continue
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			logx.Warnf("解析标点分类图片失败: name=%s err=%v", c.Name, err)
			continue
		}
		bmp, _, _, err := r.createBitmapFromImage(img, true)
		if err != nil {
			logx.Warnf("创建标点分类 bitmap 失败: name=%s err=%v", c.Name, err)
			continue
		}
		r.pinIcons[c.Name] = bmp
	}
}

func (r *renderer) loadIconBitmap(path string) (*comObject, error) {
	data, err := appassets.FS.ReadFile(filepath.ToSlash(strings.TrimPrefix(path, "assets"+string(filepath.Separator))))
	if err != nil {
		data, err = appassets.FS.ReadFile(filepath.ToSlash(strings.TrimPrefix(path, "assets/")))
	}
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bmp, _, _, err := r.createBitmapFromImage(img, true)
	return bmp, err
}

func (r *renderer) loadMapWithWIC(path string) (*comObject, uint32, uint32, error) {
	return r.loadBitmapWithWIC(path)
}

func (r *renderer) loadBitmapWithWIC(path string) (*comObject, uint32, uint32, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, 0, 0, err
	}
	var decoder uintptr
	hr := r.wic.call(3,
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		genericRead,
		wicDecodeMetadataCache,
		uintptr(unsafe.Pointer(&decoder)),
	)
	if failed(hr) {
		return nil, 0, 0, fmt.Errorf("CreateDecoderFromFilename 失败: 0x%x", hr)
	}
	dec := &comObject{ptr: decoder}
	defer dec.release()

	var frame uintptr
	hr = dec.call(13, 0, uintptr(unsafe.Pointer(&frame)))
	if failed(hr) {
		return nil, 0, 0, fmt.Errorf("GetFrame 失败: 0x%x", hr)
	}
	src := &comObject{ptr: frame}
	defer src.release()

	var converted uintptr
	hr, _, _ = procWICConvertBitmapSource.Call(
		uintptr(unsafe.Pointer(&guidWICPixel32bppPBGRA)),
		src.ptr,
		uintptr(unsafe.Pointer(&converted)),
	)
	if failed(hr) {
		return nil, 0, 0, fmt.Errorf("WICConvertBitmapSource 失败: 0x%x", hr)
	}
	conv := &comObject{ptr: converted}
	defer conv.release()

	var width, height uint32
	hr = conv.call(3, uintptr(unsafe.Pointer(&width)), uintptr(unsafe.Pointer(&height)))
	if failed(hr) {
		return nil, 0, 0, fmt.Errorf("WIC GetSize 失败: 0x%x", hr)
	}
	var bitmap uintptr
	hr = r.target.call(5, conv.ptr, 0, uintptr(unsafe.Pointer(&bitmap)))
	if failed(hr) {
		return nil, 0, 0, fmt.Errorf("CreateBitmapFromWicBitmap 失败: 0x%x", hr)
	}
	return &comObject{ptr: bitmap}, width, height, nil
}

func (r *renderer) loadMapWithGoImage(path string) (*comObject, uint32, uint32, error) {
	reader, err := openMapImageReader(path)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("打开实时地图底图失败: %w", err)
	}
	defer reader.Close()
	img, _, err := image.Decode(reader)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("解析实时地图底图失败: %w", err)
	}
	bitmap, w, h, err := r.createBitmapFromImage(img, false)
	return bitmap, w, h, err
}

func openMapImageReader(path string) (io.ReadCloser, error) {
	if strings.EqualFold(filepath.Ext(path), ".map") {
		bundle, err := mapbundle.Open(path)
		if err != nil {
			return nil, err
		}
		return bundle.Image()
	}
	return os.Open(path)
}

func (r *renderer) createBitmapFromImage(img image.Image, premultiply bool) (*comObject, uint32, uint32, error) {
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	bg := make([]byte, rgba.Rect.Dx()*rgba.Rect.Dy()*4)
	for y := 0; y < rgba.Rect.Dy(); y++ {
		row := rgba.Pix[y*rgba.Stride : y*rgba.Stride+rgba.Rect.Dx()*4]
		for x := 0; x < rgba.Rect.Dx(); x++ {
			s := x * 4
			d := y*rgba.Rect.Dx()*4 + s
			a := row[s+3]
			if premultiply {
				bg[d+0] = byte((uint16(row[s+2]) * uint16(a)) / 255)
				bg[d+1] = byte((uint16(row[s+1]) * uint16(a)) / 255)
				bg[d+2] = byte((uint16(row[s+0]) * uint16(a)) / 255)
				bg[d+3] = a
			} else {
				bg[d+0] = row[s+2]
				bg[d+1] = row[s+1]
				bg[d+2] = row[s+0]
				bg[d+3] = 255
			}
		}
	}
	alpha := uint32(d2dAlphaIgnore)
	if premultiply {
		alpha = d2dAlphaPremultiplied
	}
	props := d2dBitmapProps{
		PixelFormat: d2dPixelFormat{Format: dxgiFormatB8G8R8A8UNorm, AlphaMode: alpha},
		DpiX:        96,
		DpiY:        96,
	}
	size := d2dSizeU{Width: uint32(rgba.Rect.Dx()), Height: uint32(rgba.Rect.Dy())}
	var bitmap uintptr
	hr := r.target.call(4, packSizeU(size), uintptr(unsafe.Pointer(&bg[0])), uintptr(uint32(rgba.Rect.Dx()*4)), uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&bitmap)))
	if failed(hr) {
		return nil, 0, 0, fmt.Errorf("CreateBitmap 失败: 0x%x", hr)
	}
	return &comObject{ptr: bitmap}, size.Width, size.Height, nil
}

func (r *renderer) resize(size image.Point) error {
	if r.target == nil || size.X <= 0 || size.Y <= 0 {
		return nil
	}
	s := d2dSizeU{Width: uint32(size.X), Height: uint32(size.Y)}
	hr := r.target.call(58, uintptr(unsafe.Pointer(&s)))
	if failed(hr) {
		return fmt.Errorf("Direct2D Resize 失败: 0x%x", hr)
	}
	return nil
}
