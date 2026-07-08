package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	tray := NewTrayManager(app)
	app.SetTrayManager(tray)

	err := wails.Run(&options.App{
		Title:     "Hindsight Local Manager",
		Width:     1330,
		Height:    1010,
		MinWidth:  920,
		MinHeight: 620,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 19, G: 19, B: 19, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.BeforeClose,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "hindsight-local-manager-v1",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				app.ShowWindow()
			},
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
