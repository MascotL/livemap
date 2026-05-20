package main

import (
	"embed"
	"os"

	core "github.com/mascotl/livemap/internal"
	"github.com/mascotl/livemap/internal/gui"
	"github.com/mascotl/livemap/internal/logx"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:cmd/livemap/frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--worker" {
		if err := core.Run(); err != nil {
			logx.Errorf("启动失败: %v", err)
			os.Exit(1)
		}
		return
	}

	uiApp := gui.NewApp()
	if err := wails.Run(&options.App{
		Title:     "LiveMap",
		Width:     1120,
		Height:    720,
		Frameless: true,
		MinWidth:  920,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour:         &options.RGBA{R: 17, G: 24, B: 39, A: 255},
		OnStartup:                uiApp.OnStartup,
		EnableDefaultContextMenu: true,
		Bind: []interface{}{
			uiApp,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
	}); err != nil {
		logx.Errorf("启动失败: %v", err)
		os.Exit(1)
	}
}
