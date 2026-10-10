package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/muthuishere/windowctl"
)

// WindowInfo is the bindings-friendly view of a window shown in the UI.
type WindowInfo struct {
	App     string `json:"app"`
	Title   string `json:"title"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Monitor int    `json:"monitor"`
}

// normalizeAppID maps the OS-reported app name to a stable lookup key.
// windowctl matches App case-insensitively, so lowercase+trim keeps our
// keys consistent with what the backend will match later.
//
// TODO: prefer a platform-stable identity (macOS bundle ID, Linux
// WM_CLASS/app_id) via a per-OS resolver; the AppPreset.BundleID field
// is already reserved for it.
func normalizeAppID(appName string) string {
	return strings.ToLower(strings.TrimSpace(appName))
}

// focusedWindow returns the frontmost window. windowctl stamps Focused on
// the topmost window of the focused monitor.
func focusedWindow() (windowctl.Window, error) {
	windows, err := windowctl.ListWindows(windowctl.Filter{})
	if err != nil {
		return windowctl.Window{}, fmt.Errorf("list windows: %w", err)
	}
	for _, w := range windows {
		if w.Focused {
			return w, nil
		}
	}
	if len(windows) == 0 {
		return windowctl.Window{}, fmt.Errorf("no windows found")
	}
	return windowctl.Window{}, fmt.Errorf("no focused window (is the desktop focused?)")
}

// matchFor builds the tightest Match for w. The focused window's full
// title is used first so multi-window apps (two browser windows, etc.)
// resize the right one — title matching is substring based, so the exact
// current title always matches itself. Falls back to App matching.
func matchFor(w windowctl.Window) windowctl.Match {
	if strings.TrimSpace(w.Title) != "" {
		return windowctl.Match{Title: w.Title}
	}
	return windowctl.Match{App: w.App}
}

func toWindowInfo(w windowctl.Window) WindowInfo {
	return WindowInfo{App: w.App, Title: w.Title, W: w.Bounds.W, H: w.Bounds.H, Monitor: w.Monitor}
}

// ResizerService is the Wails-bound backend. All methods are safe for
// concurrent use: global-shortcut callbacks fire on their own goroutine.
type ResizerService struct {
	mu  sync.Mutex
	cfg *Config
}

// NewResizerService loads (or creates) the config. firstRun is true when
// no config file existed, so the UI can show the settings window once.
func NewResizerService() (svc *ResizerService, firstRun bool, err error) {
	cfg, firstRun, err := Load()
	if err != nil {
		return nil, false, err
	}
	return &ResizerService{cfg: cfg}, firstRun, nil
}

// GetConfig returns a snapshot of the current config for the settings UI.
func (s *ResizerService) GetConfig() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.cfg
}

// SetHotkey persists a new global hotkey. It takes effect after an app
// restart (the OS binding happens once at startup).
func (s *ResizerService) SetHotkey(hotkey string) error {
	hotkey = strings.TrimSpace(hotkey)
	if hotkey == "" {
		return fmt.Errorf("hotkey must not be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Hotkey = hotkey
	return s.cfg.Save()
}

// FocusedInfo describes the currently focused window (for the UI).
func (s *ResizerService) FocusedInfo() (WindowInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := focusedWindow()
	if err != nil {
		return WindowInfo{}, err
	}
	return toWindowInfo(w), nil
}

// RecordFocused stores the focused window's current size in its app
// preset. This is the "teach" half of the workflow: focus a window,
// size it by hand, then record it.
func (s *ResizerService) RecordFocused() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := focusedWindow()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(w.App) == "" {
		return "", fmt.Errorf("focused window has no app name; cannot record")
	}
	if w.Bounds.W <= 0 || w.Bounds.H <= 0 {
		return "", fmt.Errorf("focused window has invalid size %dx%d", w.Bounds.W, w.Bounds.H)
	}
	id := normalizeAppID(w.App)
	p := s.cfg.FindOrCreate(id, w.App)
	_, isNew := p.RecordSize(w.Bounds.W, w.Bounds.H)
	if err := s.cfg.Save(); err != nil {
		return "", err
	}
	if isNew {
		return fmt.Sprintf("Recorded %dx%d for %s (preset #%d of %d)", w.Bounds.W, w.Bounds.H, w.App, len(p.Sizes), len(p.Sizes)), nil
	}
	return fmt.Sprintf("%dx%d already recorded for %s", w.Bounds.W, w.Bounds.H, w.App), nil
}

// CycleFocused resizes the focused window to the next recorded size for
// its app. This is what the global hotkey (and the tray item) invoke.
func (s *ResizerService) CycleFocused() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := focusedWindow()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(w.App) == "" {
		return "", fmt.Errorf("focused window has no app name")
	}
	p := s.cfg.Find(normalizeAppID(w.App))
	if p == nil || len(p.Sizes) == 0 {
		name := w.App
		if name == "" {
			name = "this app"
		}
		return "", fmt.Errorf("no sizes recorded for %s — focus it, size it, then Record", name)
	}
	size, _ := p.NextSize()
	if err := windowctl.Resize(matchFor(w), size.W, size.H); err != nil {
		// Resize reports OS clamps (e.g. minimum window width) as
		// errors carrying the actual landed bounds — surface them.
		return "", fmt.Errorf("resize %s to %dx%d: %w", w.App, size.W, size.H, err)
	}
	if err := s.cfg.Save(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s → %dx%d (%d/%d)", w.App, size.W, size.H, p.LastIndex+1, len(p.Sizes)), nil
}

// RemoveApp deletes the preset for id.
func (s *ResizerService) RemoveApp(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cfg.Remove(id) {
		return fmt.Errorf("no preset for %q", id)
	}
	return s.cfg.Save()
}

// AXGranted reports the macOS Accessibility permission state. On other
// platforms windowctl reports granted.
func (s *ResizerService) AXGranted() bool {
	return windowctl.CheckAccessibility()
}

// RequestAX triggers the OS permission prompt (macOS: opens System
// Settings on first call).
func (s *ResizerService) RequestAX() error {
	return windowctl.RequestAccessibility()
}
