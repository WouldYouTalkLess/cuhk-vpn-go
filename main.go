package main

import (
	"context"
	"cuhk-vpn-go/internal/broker"
	"cuhk-vpn-go/internal/core"
	"cuhk-vpn-go/internal/platform"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"io/fs"
	"os"
	"runtime"
)

//go:embed all:frontend
var frontend embed.FS

func main() {
	args := os.Args[1:]
	if platform.RunSignalMode(args) {
		return
	}
	if len(args) == 1 && (args[0] == "--prepare-core" || args[0] == "--core-info") {
		var info core.Info
		var e error
		if args[0] == "--prepare-core" {
			info, e = core.Prepare(context.Background(), func(s string) { fmt.Fprintln(os.Stderr, s) })
		} else {
			info = core.Find("")
		}
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		json.NewEncoder(os.Stdout).Encode(info)
		if !info.Ready {
			os.Exit(1)
		}
		return
	}
	if len(args) > 0 && args[0] == "--vpn-helper" {
		if e := broker.Helper(args[1:]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}
	app := NewApp()
	assets, _ := fs.Sub(frontend, "frontend")
	var m *menu.Menu
	if runtime.GOOS == "darwin" {
		m = menu.NewMenu()
		m.Append(menu.AppMenu())
	}
	err := wails.Run(&options.App{Title: "CUHK VPN · CRT", Width: 520, Height: 820, MinWidth: 400, MinHeight: 620,
		BackgroundColour: options.NewRGB(9, 19, 13), AssetServer: &assetserver.Options{Assets: assets}, Menu: m,
		Windows: &windows.Options{Theme: windows.Dark, CustomTheme: &windows.ThemeSettings{
			DarkModeTitleBar: windows.RGB(9, 19, 13), DarkModeTitleBarInactive: windows.RGB(9, 19, 13),
			DarkModeTitleText: windows.RGB(151, 234, 174), DarkModeTitleTextInactive: windows.RGB(133, 172, 140),
			DarkModeBorder: windows.RGB(43, 70, 49), DarkModeBorderInactive: windows.RGB(27, 38, 28),
		}},
		OnStartup: app.startup, OnBeforeClose: app.beforeClose, OnShutdown: app.shutdown, Bind: []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "cuhk-vpn-go-desktop-v1", OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
			if app.ctx != nil {
				wruntime.WindowShow(app.ctx)
				wruntime.WindowUnminimise(app.ctx)
			}
		}},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
