package main

import (
	"context"
	"cuhk-vpn-go/internal/broker"
	"cuhk-vpn-go/internal/core"
	"cuhk-vpn-go/internal/platform"
	"cuhk-vpn-go/internal/tray"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"io/fs"
	"os"
	"runtime"
)

//go:embed all:frontend
var frontend embed.FS

//go:embed build/windows/icon.ico
var trayWindowsIcon []byte

//go:embed build/appicon.png
var trayPNGIcon []byte

// Overridden only for an isolated UI test build; helpers never create a tray.
var desktopInstanceID = "cuhk-vpn-go-desktop-v1"

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
	icon := trayPNGIcon
	if runtime.GOOS == "windows" {
		icon = trayWindowsIcon
	}
	template, _ := frontend.ReadFile("frontend/icons/network.png")
	trayController := tray.New(icon, template, tray.Actions{Show: app.ShowWindow, Toggle: app.toggleFromTray, Quit: app.Quit, Ready: app.emit})
	app.tray = trayController
	if e := trayController.Start(); e != nil {
		app.log("托盘不可用：" + e.Error())
	}
	defer trayController.Stop()
	assets, _ := fs.Sub(frontend, "frontend")
	var m *menu.Menu
	if runtime.GOOS == "darwin" {
		m = menu.NewMenu()
		applicationMenu := m.AddSubmenu("CUHK VPN")
		applicationMenu.AddText("显示主界面", nil, func(_ *menu.CallbackData) { app.ShowWindow() })
		applicationMenu.AddSeparator()
		applicationMenu.AddText("退出 CUHK VPN", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) { app.Quit() })
		m.Append(menu.EditMenu())
	}
	err := wails.Run(&options.App{Title: "CUHK VPN · CRT", Width: 520, Height: 820, MinWidth: 400, MinHeight: 620,
		BackgroundColour: options.NewRGB(9, 19, 13), AssetServer: &assetserver.Options{Assets: assets}, Menu: m,
		Windows: &windows.Options{Theme: windows.Dark, CustomTheme: &windows.ThemeSettings{
			DarkModeTitleBar: windows.RGB(9, 19, 13), DarkModeTitleBarInactive: windows.RGB(9, 19, 13),
			DarkModeTitleText: windows.RGB(151, 234, 174), DarkModeTitleTextInactive: windows.RGB(133, 172, 140),
			DarkModeBorder: windows.RGB(43, 70, 49), DarkModeBorderInactive: windows.RGB(27, 38, 28),
		}},
		OnStartup: app.startup, OnBeforeClose: app.beforeClose, OnShutdown: app.shutdown, Bind: []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: desktopInstanceID, OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
			app.ShowWindow()
		}},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
