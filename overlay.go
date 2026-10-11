package main

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/muthuishere/windowctl"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// This file implements the overlay window picker: a transparent,
// frameless, always-on-top window spanning all monitors. The frontend
// draws one outline per external window; clicking one records its size.
//
// Coordinate systems (verified by a live probe on macOS): windowctl
// Monitor/Window bounds and Wails' Screen bounds / SetPosition all use
// the SAME space — top-left origin, logical points (already
// scale-normalized on Retina). So overlay placement is just the desktop
// union, and window rects need no Y flip or scale conversion.
//
// Placement clamp (verified by pixel-level screenshot probe): macOS
// runs NSWindow constrainFrameRect on setFrame, which pushes the window
// down so its top sits at the menu bar's lower edge (Y+33 on the test
// machine) — both at creation and via SetPosition. Since the frontend
// draws rects relative to the window's own top-left, every outline would
// be drawn that many pixels too low. Fix: after showing the window, read
// its ACTUAL origin via Position() and rebase the rects to it
// (rebaseOverlay), self-correcting whatever the OS did to the frame.
//
// NOTE (macOS): true webview transparency requires building with
// -tags private_mac_apis, otherwise the overlay renders opaque.
// Task builds accept it via: task build EXTRA_TAGS=private_mac_apis

// OverlayRect is one window outline in overlay-local CSS pixels.
// X/Y are relative to the desktop union's top-left; W/H follow the
// window's outer frame (includes titlebar), matching what the user sees.
type OverlayRect struct {
	ID    string `json:"id"`
	App   string `json:"app"`
	Title string `json:"title"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	W     int    `json:"w"`
	H     int    `json:"h"`
	// IsRecorded marks a window whose current size is already saved for its
	// app. The picker renders these differently and refuses to select them:
	// RecordWindow would only report the duplicate after the fact.
	IsRecorded bool `json:"isRecorded"`
}

// OverlayPlacement positions the overlay window itself. X/Y follow Wails'
// convention (top-left origin); W/H is the desktop union size.
type OverlayPlacement struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// OverlayData is everything the overlay page needs in one call.
type OverlayData struct {
	Placement OverlayPlacement `json:"placement"`
	Windows   []OverlayRect    `json:"windows"`
}

// systemApps are OS shell owners whose "windows" (menu bar extras, Dock
// tiles, notification panels) must never be offered as record targets.
var systemApps = map[string]bool{
	"Dock":           true,
	"SystemUIServer": true,
	"Window Server":  true,
	"Control Centre": true,
	"Spotlight":      true,
}

// computeOverlayData converts monitors + windows into overlay placement
// + union-local rects. Windows owned by our own process (selfPID) or by
// OS shell processes are dropped — the overlay must never offer to
// record itself or the Dock. sizes flags windows already carrying a
// recorded size so the picker can mark them instead of offering an
// action that could only come back as a duplicate.
func computeOverlayData(monitors []windowctl.Monitor, windows []windowctl.Window, selfPID int, sizes SizeIndex) (OverlayData, error) {
	if len(monitors) == 0 {
		return OverlayData{}, fmt.Errorf("no monitors found")
	}
	// Desktop union (same coordinate space as the windows).
	minX, minY := monitors[0].X, monitors[0].Y
	maxX, maxY := monitors[0].X+monitors[0].Width, monitors[0].Y+monitors[0].Height
	for _, m := range monitors {
		if x := m.X; x < minX {
			minX = x
		}
		if y := m.Y; y < minY {
			minY = y
		}
		if x := m.X + m.Width; x > maxX {
			maxX = x
		}
		if y := m.Y + m.Height; y > maxY {
			maxY = y
		}
	}
	w, h := maxX-minX, maxY-minY
	if w <= 0 || h <= 0 {
		return OverlayData{}, fmt.Errorf("degenerate desktop union %dx%d", w, h)
	}
	data := OverlayData{
		Placement: OverlayPlacement{X: minX, Y: minY, W: w, H: h},
		Windows:   []OverlayRect{},
	}
	for _, win := range windows {
		if win.PID == selfPID {
			continue
		}
		if systemApps[win.App] {
			continue
		}
		b := win.Bounds
		if b.W <= 0 || b.H <= 0 {
			continue
		}
		data.Windows = append(data.Windows, OverlayRect{
			ID:         win.ID,
			App:        win.App,
			Title:      win.Title,
			X:          b.X - minX,
			Y:          b.Y - minY,
			W:          b.W,
			H:          b.H,
			IsRecorded: sizes.Has(win.App, b.W, b.H),
		})
	}
	return data, nil
}

// rebaseOverlay shifts overlay-local rect coordinates so that CSS (0,0)
// maps to the window's ACTUAL on-screen top-left (actualX, actualY)
// instead of the desktop union origin. The delta absorbs whatever macOS
// did to the frame when placing it (e.g. the menu bar clamp that moves a
// requested Y of 0 down to the menu bar's lower edge).
func rebaseOverlay(data *OverlayData, actualX, actualY int) {
	dx := data.Placement.X - actualX
	dy := data.Placement.Y - actualY
	if dx == 0 && dy == 0 {
		return
	}
	for i := range data.Windows {
		data.Windows[i].X += dx
		data.Windows[i].Y += dy
	}
	data.Placement.X = actualX
	data.Placement.Y = actualY
}

// GetOverlayData lists monitors + windows and returns overlay-ready data.
// Rects are rebased to the overlay window's actual on-screen origin (when
// the window exists) so polling updates stay aligned even though macOS
// clamped the frame away from the requested placement.
func (s *ResizerService) GetOverlayData() (OverlayData, error) {
	monitors, err := windowctl.ListMonitors()
	if err != nil {
		return OverlayData{}, fmt.Errorf("list monitors: %w", err)
	}
	windows, err := windowctl.ListWindows(windowctl.Filter{})
	if err != nil {
		return OverlayData{}, fmt.Errorf("list windows: %w", err)
	}
	s.mu.Lock()
	sizes := NewSizeIndex(s.cfg.Apps)
	ov := s.overlay
	s.mu.Unlock()
	data, err := computeOverlayData(monitors, windows, os.Getpid(), sizes)
	if err != nil {
		return OverlayData{}, err
	}
	if ov != nil {
		x, y := ov.Position()
		rebaseOverlay(&data, x, y)
	}
	return data, nil
}

// overlayWindowTitle is the picker window's Wails title; the anti-drag
// pin (overlay_pin_darwin.m) locates the NSWindow by this name.
const overlayWindowTitle = "WinResizer Picker"

// overlayAnchor remembers where the overlay window actually sits on
// screen so any user-initiated move can be undone. The WindowDidMove hook
// runs synchronously on the main thread while OpenOverlay may still be
// mid-move, so the state is atomic and a suspend flag brackets our own
// positioning — the hook must never block on the service lock.
type overlayAnchor struct {
	x, y    atomic.Int64
	armed   atomic.Bool
	suspend atomic.Bool
}

func (a *overlayAnchor) suspendTracking() { a.suspend.Store(true) }

func (a *overlayAnchor) resumeTracking() { a.suspend.Store(false) }

func (a *overlayAnchor) set(x, y int) {
	a.x.Store(int64(x))
	a.y.Store(int64(y))
	a.armed.Store(true)
}

// snapBackTarget decides whether the overlay must be moved back to its
// anchor after a WindowDidMove event. ok is false while tracking is
// suspended (OpenOverlay is positioning the window itself), before the
// first anchor is known, or when the window still sits exactly at the
// anchor (our own SetPosition fired the event) — callers skip the move.
func snapBackTarget(a *overlayAnchor, curX, curY int) (x, y int, ok bool) {
	if a.suspend.Load() || !a.armed.Load() {
		return 0, 0, false
	}
	ax, ay := int(a.x.Load()), int(a.y.Load())
	if curX == ax && curY == ay {
		return 0, 0, false
	}
	return ax, ay, true
}

// OpenOverlay shows the picker overlay, hiding the console first (it is
// restored when the overlay closes if it was visible). Safe to call from
// the tray, the console, or the hotkey path.
func (s *ResizerService) OpenOverlay() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.app == nil || s.console == nil {
		return fmt.Errorf("app not initialised")
	}
	monitors, err := windowctl.ListMonitors()
	if err != nil {
		return fmt.Errorf("list monitors: %w", err)
	}
	windows, err := windowctl.ListWindows(windowctl.Filter{})
	if err != nil {
		return fmt.Errorf("list windows: %w", err)
	}
	// Only the placement is used here; the rects are re-fetched by
	// GetOverlayData, which is where the recorded flags are filled in.
	data, err := computeOverlayData(monitors, windows, os.Getpid(), nil)
	if err != nil {
		return err
	}
	p := data.Placement
	// Bracket our own window moves so the WindowDidMove hook ignores
	// them; tracking is re-armed once the real anchor is known below.
	s.ovAnchor.suspendTracking()
	if s.overlay == nil {
		ov := s.app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:             "overlay",
			Title:            overlayWindowTitle,
			Width:            p.W,
			Height:           p.H,
			X:                p.X,
			Y:                p.Y,
			InitialPosition:  application.WindowXY,
			Frameless:        true,
			AlwaysOnTop:      true,
			DisableResize:    true,
			Hidden:           true,
			URL:              "/#overlay",
			BackgroundType:   application.BackgroundTypeTransparent,
			BackgroundColour: application.RGBA{Red: 0, Green: 0, Blue: 0, Alpha: 0},
			Mac: application.MacWindow{
				Backdrop:      application.MacBackdropTransparent,
				DisableShadow: true,
				// ScreenSaver level floats above the menu bar (NSStatusWindowLevel)
				// and Dock, so the picker is never occluded by either.
				WindowLevel: application.MacWindowLevelScreenSaver,
				// The picker must cover fullscreen apps and follow the
				// user across Spaces.
				CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
					application.MacWindowCollectionBehaviorFullScreenAuxiliary |
					application.MacWindowCollectionBehaviorStationary,
			},
		})
		// Reuse the window across picks: hide instead of destroying.
		ov.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			ov.Hide()
			e.Cancel()
		})
		// Double insurance against the overlay being dragged away (the
		// cgo pin in overlay_pin_darwin.m is the primary fix): any move
		// that is not ours snaps straight back to the anchor. Hooks run
		// synchronously on the main thread, so this must only touch the
		// atomic anchor — never s.mu, which OpenOverlay holds across
		// window operations dispatched to that same thread.
		ov.RegisterHook(events.Common.WindowDidMove, func(_ *application.WindowEvent) {
			cx, cy := ov.Position()
			if ax, ay, ok := snapBackTarget(&s.ovAnchor, cx, cy); ok {
				ov.SetPosition(ax, ay)
			}
		})
		s.overlay = ov
	} else {
		s.overlay.SetPosition(p.X, p.Y)
		s.overlay.SetSize(p.W, p.H)
	}
	s.consoleWasVisible = s.console.IsVisible()
	if s.consoleWasVisible {
		s.console.Hide()
	}
	// Freeze the window against user dragging. Primary anti-drag fix:
	// a cgo shim (overlay_pin_darwin.m) that calls setMovable:NO — cheap
	// and idempotent, so run it on every open.
	pinOverlayWindow(overlayWindowTitle)
	s.overlay.Show()
	s.overlay.Focus()
	// macOS may have clamped the frame (menu bar clamp): read back where
	// the window ACTUALLY landed and rebase the outlines to it, so CSS
	// (0,0) is the window's true top-left on screen.
	x, y := s.overlay.Position()
	s.ovAnchor.set(x, y)
	s.ovAnchor.resumeTracking()
	rebaseOverlay(&data, x, y)
	// Tell the overlay page to (re)load: the page persists across
	// Show/Hide cycles, so mount alone is not enough. The payload carries
	// the fresh snapshot so rendering never depends on a second call.
	s.overlay.EmitEvent("overlay-open", data)
	return nil
}

// CloseOverlay hides the picker and restores the console if it was
// visible when the overlay opened.
func (s *ResizerService) CloseOverlay() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.overlay != nil {
		s.overlay.Hide()
	}
	if s.consoleWasVisible && s.console != nil {
		s.console.Show()
		s.console.Focus()
		s.consoleWasVisible = false
	}
}
