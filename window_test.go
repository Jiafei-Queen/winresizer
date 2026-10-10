package main

import (
	"testing"

	"github.com/muthuishere/windowctl"
)

func TestNormalizeAppID(t *testing.T) {
	cases := map[string]string{
		"Google Chrome": "google chrome",
		"  Code  ":      "code",
		"":              "",
	}
	for in, want := range cases {
		if got := normalizeAppID(in); got != want {
			t.Errorf("normalizeAppID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatchForPrefersTitle(t *testing.T) {
	w := windowctl.Window{App: "Code", Title: "main.go - winresizer"}
	m := matchFor(w)
	if m.Title != w.Title || m.App != "" {
		t.Fatalf("matchFor = %+v, want title-only match", m)
	}
	w.Title = "   "
	m = matchFor(w)
	if m.App != "Code" || m.Title != "" {
		t.Fatalf("matchFor blank title = %+v, want app-only match", m)
	}
}

func TestToWindowInfo(t *testing.T) {
	w := windowctl.Window{ID: "42", App: "Firefox", Title: "Inbox", Monitor: 2}
	w.Bounds.W, w.Bounds.H = 1200, 800
	info := toWindowInfo(w)
	if info.ID != "42" || info.App != "Firefox" || info.W != 1200 || info.H != 800 || info.Monitor != 2 {
		t.Fatalf("toWindowInfo = %+v", info)
	}
}

func TestTrackerIgnoresSelf(t *testing.T) {
	const self = 100
	tr := newTargetTracker(func() ([]windowctl.Window, error) { return nil, nil }, self, targetPollInterval)
	defer tr.stopLoop()

	ext := windowctl.Window{ID: "1", App: "Safari", PID: 200, Focused: true}
	tr.update([]windowctl.Window{ext})
	if got, ok := tr.get(); !ok || got.ID != "1" {
		t.Fatalf("get = %+v,%v, want ID=1,true", got, ok)
	}

	// Only our own window focused: target must stay on the external one.
	own := windowctl.Window{ID: "2", App: "WinResizer", PID: self, Focused: true}
	tr.update([]windowctl.Window{own})
	if got, ok := tr.get(); !ok || got.ID != "1" {
		t.Fatalf("get after self-focus = %+v,%v, want ID=1,true", got, ok)
	}

	// A newer external focus replaces the target.
	ext2 := windowctl.Window{ID: "3", App: "Code", PID: 300, Focused: true}
	tr.update([]windowctl.Window{own, ext2})
	if got, ok := tr.get(); !ok || got.ID != "3" {
		t.Fatalf("get after switch = %+v,%v, want ID=3,true", got, ok)
	}
}

func TestTrackerEmptyInitially(t *testing.T) {
	tr := newTargetTracker(func() ([]windowctl.Window, error) { return nil, nil }, 100, targetPollInterval)
	defer tr.stopLoop()
	// No focused external window seen: no target.
	tr.update([]windowctl.Window{{ID: "9", App: "X", PID: 200}})
	if _, ok := tr.get(); ok {
		t.Fatal("get should report ok=false before any external focus")
	}
}

// Cycle/Record touch the live window manager, so they stay behind an
// integration build tag. Run with: go test -tags integration
// (requires Accessibility permission on macOS).
func TestServiceRequiresConfig(t *testing.T) {
	t.Setenv("WINRESIZER_CONFIG", t.TempDir()+"/config.json")
	svc, firstRun, err := NewResizerService()
	if err != nil {
		t.Fatalf("NewResizerService: %v", err)
	}
	defer svc.tracker.stopLoop()
	if !firstRun {
		t.Fatal("fresh config should report firstRun=true")
	}
	if got := svc.GetConfig(); got.Hotkey != DefaultHotkey {
		t.Fatalf("fresh config hotkey = %q", got.Hotkey)
	}
	if err := svc.SetHotkey("  "); err == nil {
		t.Fatal("SetHotkey should reject empty hotkey")
	}
	if err := svc.SetHotkey("Ctrl+Alt+T"); err != nil {
		t.Fatalf("SetHotkey: %v", err)
	}
	if got := svc.GetConfig(); got.Hotkey != "Ctrl+Alt+T" {
		t.Fatalf("hotkey not persisted, got %q", got.Hotkey)
	}
}
