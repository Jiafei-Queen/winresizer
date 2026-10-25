package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jiafei-Queen/windowctl"
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
	tr.update([]windowctl.Window{ext}, true)
	if got, ok := tr.get(); !ok || got.ID != "1" {
		t.Fatalf("get = %+v,%v, want ID=1,true", got, ok)
	}

	// Only our own window focused: target must stay on the external one.
	own := windowctl.Window{ID: "2", App: "WinResizer", PID: self, Focused: true}
	tr.update([]windowctl.Window{own}, true)
	if got, ok := tr.get(); !ok || got.ID != "1" {
		t.Fatalf("get after self-focus = %+v,%v, want ID=1,true", got, ok)
	}

	// A newer external focus replaces the target.
	ext2 := windowctl.Window{ID: "3", App: "Code", PID: 300, Focused: true}
	tr.update([]windowctl.Window{own, ext2}, true)
	if got, ok := tr.get(); !ok || got.ID != "3" {
		t.Fatalf("get after switch = %+v,%v, want ID=3,true", got, ok)
	}
}

func TestTrackerEmptyInitially(t *testing.T) {
	tr := newTargetTracker(func() ([]windowctl.Window, error) { return nil, nil }, 100, targetPollInterval)
	defer tr.stopLoop()
	// No focused external window seen: no target.
	tr.update([]windowctl.Window{{ID: "9", App: "X", PID: 200}}, true)
	if _, ok := tr.get(); ok {
		t.Fatal("get should report ok=false before any external focus")
	}
}

func TestTrackerSetOverwrites(t *testing.T) {
	tr := newTargetTracker(func() ([]windowctl.Window, error) { return nil, nil }, 100, targetPollInterval)
	defer tr.stopLoop()

	// set() pins a target even though nothing was ever focused — this is
	// how a window the user picked by hand becomes the target.
	tr.set(windowctl.Window{ID: "7", App: "Preview", PID: 700})
	got, ok := tr.get()
	if !ok || got.ID != "7" {
		t.Fatalf("get after set = %+v,%v, want ID=7,true", got, ok)
	}

	// A later poll still wins, so set() does not wedge the tracker.
	tr.update([]windowctl.Window{{ID: "8", App: "Safari", PID: 800, Focused: true}}, true)
	got, ok = tr.get()
	if !ok || got.ID != "8" {
		t.Fatalf("get after poll = %+v,%v, want ID=8,true", got, ok)
	}
}

// The UI refresh path relies on the tracker announcing changes: Linux's
// Wails GTK3 backend never forwards focus-in, so the console cannot count
// on a focus event to re-read GetTarget. set() must announce a new
// target or an in-place change, but not every identical poll.
func TestTrackerNotifiesOnTargetChange(t *testing.T) {
	// The list func returns no windows, so the background ticker never
	// stores anything and the calls below are the only writer. The mutex
	// keeps the counter safe should that ever change.
	var (
		mu        sync.Mutex
		announced int
	)
	tr := newTargetTracker(func() ([]windowctl.Window, error) { return nil, nil }, 100, targetPollInterval)
	defer tr.stopLoop()

	tr.setOnChange(func() {
		mu.Lock()
		announced++
		mu.Unlock()
	})

	base := windowctl.Window{ID: "1", App: "Safari", PID: 200, Focused: true}
	base.Bounds.W, base.Bounds.H = 800, 600
	tr.set(base)
	tr.set(base) // identical poll snapshot: no announcement

	resized := base
	resized.Bounds.W = 900
	tr.set(resized) // same window, new bounds: announcement

	tr.set(windowctl.Window{ID: "2", App: "Code", PID: 300, Focused: true})

	mu.Lock()
	got := announced
	mu.Unlock()
	if got != 3 {
		t.Fatalf("announcements = %d, want 3", got)
	}
}

// A window that only moved is not a change worth a round-trip: the
// console shows the size, not the coordinates.
func TestTrackerIgnoresPureMove(t *testing.T) {
	tr := newTargetTracker(func() ([]windowctl.Window, error) { return nil, nil }, 100, targetPollInterval)
	defer tr.stopLoop()

	var (
		mu        sync.Mutex
		announced int
	)
	tr.setOnChange(func() {
		mu.Lock()
		announced++
		mu.Unlock()
	})

	base := windowctl.Window{ID: "1", App: "Safari", PID: 200, Focused: true}
	base.Bounds.W, base.Bounds.H = 800, 600
	tr.set(base)

	moved := base
	moved.Bounds.X = 120
	tr.set(moved)

	mu.Lock()
	got := announced
	mu.Unlock()
	if got != 1 {
		t.Fatalf("announcements = %d, want 1 (the move is not a change)", got)
	}
}

// GetTarget must not announce the change it detects: the response
// already carries the fresh target, and an announcement would make the
// console read it again — and again, for as long as the target keeps
// changing. Only the background ticker may wake the console.
func TestGetTargetDoesNotAnnounce(t *testing.T) {
	win := windowctl.Window{ID: "1", App: "Safari", PID: 200, Focused: true}

	// The list stays empty until the tracker's startup poll is over, so
	// the ticker cannot announce anything; only GetTarget's own poll
	// writes to the tracker. The hour-long interval keeps it that way.
	var (
		mu       sync.Mutex
		armed    bool
		polled   = make(chan struct{})
		firstOne sync.Once
	)
	tr := newTargetTracker(func() ([]windowctl.Window, error) {
		mu.Lock()
		defer mu.Unlock()
		if !armed {
			firstOne.Do(func() { close(polled) })
			return nil, nil
		}
		return []windowctl.Window{win}, nil
	}, 100, time.Hour)
	defer tr.stopLoop()
	<-polled

	mu.Lock()
	armed = true
	mu.Unlock()

	announced := make(chan struct{}, 4)
	tr.setOnChange(func() { announced <- struct{}{} })

	svc := &ResizerService{tracker: tr, cfg: DefaultConfig()}
	got, err := svc.GetTarget()
	if err != nil || got.ID != "1" {
		t.Fatalf("GetTarget = %+v,%v, want ID=1,nil", got, err)
	}

	select {
	case <-announced:
		t.Fatal("GetTarget announced a target change; the console would read it again")
	default:
	}
}

// GetTarget must poll on read, not just serve the background ticker:
// focus returning to the console is precisely when a switch the ticker
// missed would still be the answer.
func TestGetTargetPollsOnRead(t *testing.T) {
	// The tracker's background goroutine calls the list func, so the
	// test must hand snapshots across the goroutine boundary safely.
	var (
		liveMu sync.Mutex
		live   []windowctl.Window
	)
	setLive := func(ws []windowctl.Window) {
		liveMu.Lock()
		live = ws
		liveMu.Unlock()
	}
	tr := newTargetTracker(func() ([]windowctl.Window, error) {
		liveMu.Lock()
		defer liveMu.Unlock()
		return live, nil
	}, 100, targetPollInterval)
	defer tr.stopLoop()
	svc := &ResizerService{tracker: tr, cfg: DefaultConfig()}

	setLive([]windowctl.Window{{ID: "1", App: "Safari", PID: 200, Focused: true}})
	first, err := svc.GetTarget()
	if err != nil || first.ID != "1" {
		t.Fatalf("first GetTarget = %+v,%v, want ID=1,nil", first, err)
	}

	// A switch the background ticker never saw: the next read must
	// still pick it up.
	setLive([]windowctl.Window{{ID: "2", App: "Code", PID: 300, Focused: true}})
	second, err := svc.GetTarget()
	if err != nil || second.ID != "2" {
		t.Fatalf("second GetTarget = %+v,%v, want ID=2,nil", second, err)
	}
}

// GetTarget must tell the console when the window already carries a saved
// size, so Record can be disabled instead of failing after the fact.
func TestGetTargetFlagsRecordedSize(t *testing.T) {
	win := windowctl.Window{ID: "1", App: "Safari", PID: 200, Focused: true}
	win.Bounds.W, win.Bounds.H = 800, 600
	tr := newTargetTracker(func() ([]windowctl.Window, error) { return []windowctl.Window{win}, nil }, 100, targetPollInterval)
	defer tr.stopLoop()
	svc := &ResizerService{tracker: tr, cfg: DefaultConfig()}

	got, err := svc.GetTarget()
	if err != nil {
		t.Fatalf("GetTarget: %v", err)
	}
	if got.IsRecorded {
		t.Fatal("IsRecorded = true for an app with no presets, want false")
	}

	svc.cfg.Apps = []AppPreset{{ID: "safari", AppName: "Safari", Sizes: []Size{{W: 800, H: 600}}}}
	got, err = svc.GetTarget()
	if err != nil {
		t.Fatalf("GetTarget: %v", err)
	}
	if !got.IsRecorded {
		t.Fatal("IsRecorded = false for a saved 800x600, want true")
	}
}

// recordLocked is the one write path every record entry point shares. It
// must persist the size, report a duplicate instead of re-recording, and
// stay usable before a console window is wired (notifyPresetsChanged is a
// no-op then).
func TestRecordLocked(t *testing.T) {
	t.Setenv("WINRESIZER_CONFIG", t.TempDir()+"/config.json")
	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	svc := &ResizerService{cfg: cfg}

	w := windowctl.Window{App: "Safari"}
	w.Bounds.W, w.Bounds.H = 800, 600
	if _, err := svc.recordLocked(w); err != nil {
		t.Fatalf("recordLocked: %v", err)
	}
	p := cfg.Find("safari")
	if p == nil || len(p.Sizes) != 1 || p.Sizes[0] != (Size{W: 800, H: 600}) {
		t.Fatalf("preset after record = %+v", p)
	}
	msg, err := svc.recordLocked(w)
	if err != nil {
		t.Fatalf("duplicate recordLocked: %v", err)
	}
	if !strings.Contains(msg, "already recorded") {
		t.Fatalf("duplicate message = %q, want it to report an already recorded size", msg)
	}

	// The write survives a reload.
	re, _, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := re.Find("safari"); got == nil || len(got.Sizes) != 1 {
		t.Fatalf("preset after reload = %+v", got)
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
