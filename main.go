package main

import (
	"embed"
	"log"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// Emitted on the overlay window every time it opens, carrying a fresh
	// OverlayData snapshot so the picker page renders current window state
	// without a second round-trip.
	application.RegisterEvent[OverlayData]("overlay-open")
}

// main function serves as the application's entry point. WinResizer is a
// menu-bar/tray app: it lives in the background, resizes the focused window
// when the global hotkey fires, and opens a small console window on demand.
func main() {
	svc, firstRun, err := NewResizerService()
	if err != nil {
		log.Fatal(err)
	}
	cfg := svc.GetConfig()

	if !svc.AXGranted() {
		log.Print("Accessibility permission not granted — Move/Resize need it. Open Console to grant.")
	}

	app := application.New(application.Options{
		Name:        "WinResizer",
		Description: "Hotkey-driven window resizer",
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			// Must stay false (default): with a tray icon + global hotkeys the
			// app lives in the background, so hiding the last window must NOT
			// terminate the process. Setting this to true would make a
			// hide-via-hotkey quit the app instead.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
			// No dock icon: this is a menu-bar app, reached via the tray.
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "WinResizer Console",
		Width:  520,
		Height: 640,
		// Production: show once on first run so the user discovers the app;
		// afterwards it starts quietly in the tray. Dev builds always show
		// the window (see buildmode_dev.go / buildmode_production.go).
		Hidden: !(firstRun || devBuild),
		URL:    "/",
	})

	// Closing the console window hides it instead of quitting the app.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})

	// Wire window handles into the service for overlay management.
	// Same package, so these stay unexported and out of the JS bindings.
	svc.app = app
	svc.console = window

	setupTray(app, window, svc)
	registerHotkey(app, svc, cfg.Hotkey)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func setupTray(app *application.App, window *application.WebviewWindow, svc *ResizerService) {
	tray := app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(icons.SystrayMacTemplate)
	}
	tray.SetTooltip("WinResizer")

	menu := app.NewMenu()
	menu.Add("Console").OnClick(func(_ *application.Context) {
		window.Show()
		window.Focus()
	})
	menu.Add("Record").OnClick(func(_ *application.Context) {
		if err := svc.OpenOverlay(); err != nil {
			log.Printf("overlay: %v", err)
		}
	})
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(_ *application.Context) {
		app.Quit()
	})
	tray.SetMenu(menu)

	tray.OnClick(func() {
		window.Show()
		window.Focus()
	})
}

func registerHotkey(app *application.App, svc *ResizerService, hotkey string) {
	if err := app.GlobalShortcut.Register(hotkey, func() {
		// Callbacks run on their own goroutine; ResizerService is mutex-guarded.
		if msg, err := svc.CycleFocused(); err != nil {
			log.Printf("hotkey %q: %v", hotkey, err)
		} else {
			log.Print(msg)
		}
	}); err != nil {
		log.Printf("global hotkey %q not registered: %v (change it in Console, then restart)", hotkey, err)
	}
}
