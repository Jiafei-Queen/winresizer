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

	// Wire the app into the service; the console window itself is created
	// lazily on first show (see showConsole). Creation, the Wayland
	// client-side decoration prep, and the first Show then always run back
	// to back, so a system title bar never gets the chance to appear over
	// the console's own — and a tray-only startup (production, not first
	// run) never builds the console webview at all.
	svc.app = app

	setupTray(app, svc)
	registerHotkey(app, svc, cfg.Hotkey)

	// Show the console on first run so the user discovers the app; dev
	// builds always show it (see buildmode_dev.go / buildmode_production.go).
	// Every later start stays quietly in the tray until opened.
	if firstRun || devBuild {
		app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(_ *application.ApplicationEvent) {
			svc.showConsole()
		})
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// createConsoleWindow builds the console window. It is always created
// hidden — showConsole owns every show, which is what leaves room for the
// Wayland decoration prep before the first one — and closing it hides it
// instead of quitting the app.
func createConsoleWindow(app *application.App) *application.WebviewWindow {
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "WinResizer Console",
		Width:  520,
		Height: 640,
		// macOS keeps a native window frame so the traffic lights, rounded
		// corners and zoom/double-click behaviour stay AppKit's own; only the
		// title bar surface is made transparent (see Mac.TitleBar below) so it
		// shows the page's frosted background instead of a separate strip of
		// chrome. Windows and Linux drop the system title bar entirely and let
		// the frontend draw its own — there the native bar is a solid colour
		// that visibly clashes with the translucent webview beneath it.
		Frameless:        runtime.GOOS != "darwin",
		BackgroundType:   application.BackgroundTypeTranslucent,
		BackgroundColour: application.RGBA{Alpha: 0},
		Windows: application.WindowsWindow{
			BackdropType: application.Acrylic,
			// Let WebView2 handle the drawn title bar's caption/min/max/close
			// regions natively, which keeps the Windows 11 snap-layouts menu on
			// hover. Harmless if the installed WebView2 runtime is too old: the
			// frontend buttons still work through the JS Window API.
			NonClientRegionSupport: true,
		},
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropTranslucent,
			// Transparent title bar with the title text hidden, content runs
			// edge to edge underneath it. The native traffic lights remain.
			TitleBar: application.MacTitleBarHidden,
		},
		Hidden: true,
		URL:    "/",
	})
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})
	return window
}

func setupTray(app *application.App, svc *ResizerService) {
	tray := app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(icons.SystrayMacTemplate)
	}
	tray.SetTooltip("WinResizer")

	menu := app.NewMenu()
	menu.Add("Console").OnClick(func(_ *application.Context) {
		svc.showConsole()
	})
	// Named for what it does, not for what it records: the console's own
	// "Record" button records the tracked target without any picking.
	menu.Add("Pick window…").OnClick(func(_ *application.Context) {
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
		svc.showConsole()
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
