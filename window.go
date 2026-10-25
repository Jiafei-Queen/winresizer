package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Jiafei-Queen/windowctl"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// targetPollInterval is how often the tracker refreshes the "last external
// focused window" snapshot.
const targetPollInterval = time.Second

// WindowInfo is the bindings-friendly view of a window shown in the UI.
// ID is the OS window handle (macOS: CGWindowID) and identifies one window
// instance; Self marks our own settings window so the UI can grey it out.
// IsRecorded is set by GetTarget only: this window's current size is already
// saved for its app, so the console offers no record action for it.
type WindowInfo struct {
	ID         string `json:"id"`
	App        string `json:"app"`
	Title      string `json:"title"`
	W          int    `json:"w"`
	H          int    `json:"h"`
	Monitor    int    `json:"monitor"`
	Self       bool   `json:"self"`
	IsRecorded bool   `json:"isRecorded"`
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

// currentDesktopWindows lists windows on the user's current virtual
// desktop/Space. The picker, focused-window and tracker paths only act
// on what the user can see right now.
func currentDesktopWindows() ([]windowctl.Window, error) {
	return windowctl.ListWindows(windowctl.Filter{CurrentDesktopOnly: true})
}

// focusedWindow returns the frontmost window. windowctl stamps Focused on
// the topmost window of the focused monitor.
func focusedWindow() (windowctl.Window, error) {
	windows, err := currentDesktopWindows()
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
	return WindowInfo{ID: w.ID, App: w.App, Title: w.Title, W: w.Bounds.W, H: w.Bounds.H, Monitor: w.Monitor}
}

// listWindowsFunc is injectable for tests.
type listWindowsFunc func() ([]windowctl.Window, error)

func liveListWindows() ([]windowctl.Window, error) {
	return currentDesktopWindows()
}

// targetTracker remembers the most recently focused window that does NOT
// belong to our own process. Rationale: clicking our settings UI steals
// focus, so "currently focused window" is useless for Record/Cycle from
// the UI — the tracker lets the UI act on the window the user was in
// just before they clicked us.
type targetTracker struct {
	mu       sync.Mutex
	self     int
	list     listWindowsFunc
	target   *windowctl.Window
	onChange func()
	stop     chan struct{}
	done     chan struct{}
}

func newTargetTracker(list listWindowsFunc, selfPID int, interval time.Duration) *targetTracker {
	t := &targetTracker{self: selfPID, list: list, stop: make(chan struct{}), done: make(chan struct{})}
	go t.loop(interval)
	return t
}

func (t *targetTracker) loop(interval time.Duration) {
	defer close(t.done)
	t.poll()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			t.poll()
		}
	}
}

// poll refreshes the snapshot from the window list and announces a
// changed target to onChange. This is the background ticker: it is the
// only path allowed to wake the console, because nothing else tells it
// that the user moved on to another window.
func (t *targetTracker) poll() {
	t.pollWith(true)
}

// pollQuiet refreshes the snapshot without announcing anything. The
// read path (GetTarget) uses it: that caller receives the fresh
// snapshot in its response anyway, so an announcement would only make
// the console read the very same target all over again — and again, as
// long as the target keeps changing.
func (t *targetTracker) pollQuiet() {
	t.pollWith(false)
}

func (t *targetTracker) pollWith(notify bool) {
	windows, err := t.list()
	if err != nil {
		return
	}
	t.update(windows, notify)
}

// update records the focused external window, if any. When nothing
// external is focused (e.g. our own settings window has focus, or the
// desktop does), the previous target is kept. notify tells onChange
// about a changed target.
func (t *targetTracker) update(windows []windowctl.Window, notify bool) {
	for _, w := range windows {
		if w.Focused && w.PID != t.self {
			t.store(w, notify)
			return
		}
	}
}

// set pins the tracked target to w and announces a changed target. Used
// after the overlay records a window the user picked by hand, so the
// console's target card shows what was just recorded instead of the
// auto-tracked one.
func (t *targetTracker) set(w windowctl.Window) {
	t.store(w, true)
}

// store writes the snapshot and, when the snapshot actually differs
// from the previous one, tells onChange that the tracked target
// changed. The poller re-reads the same window every tick, so the
// comparison is what keeps a steady target from waking the console on
// every tick.
func (t *targetTracker) store(w windowctl.Window, notify bool) {
	snap := w
	t.mu.Lock()
	prev := t.target
	changed := prev == nil || !sameTarget(*prev, snap)
	t.target = &snap
	onChange := t.onChange
	t.mu.Unlock()
	// Run the callback outside the lock: it emits a UI event, and set is
	// also called from service methods that already hold s.mu.
	if changed && notify && onChange != nil {
		onChange()
	}
}

// setOnChange installs the callback run whenever the tracked target
// changes. Callbacks must not acquire the service lock: set can be
// called while that lock is held.
func (t *targetTracker) setOnChange(fn func()) {
	t.mu.Lock()
	t.onChange = fn
	t.mu.Unlock()
}

// sameTarget reports whether two snapshots describe the same target
// state. ID identifies the window; the remaining fields catch in-place
// changes (manual resize, retitle) the UI should re-read. The position
// is left out on purpose: the console shows the size, not the
// coordinates, so a window that only moved is not a change worth a
// round-trip.
func sameTarget(a, b windowctl.Window) bool {
	return a.ID == b.ID &&
		a.App == b.App &&
		a.Title == b.Title &&
		a.Monitor == b.Monitor &&
		a.Bounds.W == b.Bounds.W &&
		a.Bounds.H == b.Bounds.H
}

// get returns the tracked target snapshot, or ok=false when nothing has
// been tracked yet.
func (t *targetTracker) get() (w windowctl.Window, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.target == nil {
		return windowctl.Window{}, false
	}
	return *t.target, true
}

// stopLoop terminates the background goroutine (used in tests).
func (t *targetTracker) stopLoop() {
	close(t.stop)
	<-t.done
}

// ResizerService is the Wails-bound backend. All methods are safe for
// concurrent use: global-shortcut callbacks fire on their own goroutine.
// app/console/overlay are wired by main after startup (same package, so
// they stay unexported and out of the JS bindings).
type ResizerService struct {
	mu      sync.Mutex
	cfg     *Config
	tracker *targetTracker

	app               *application.App
	console           *application.WebviewWindow
	overlay           *application.WebviewWindow
	consoleWasVisible bool

	// ovAnchor tracks the overlay's real on-screen position so the
	// WindowDidMove hook can snap it back if dragged. Atomic (see
	// overlayAnchor): touched from the main thread without s.mu.
	ovAnchor overlayAnchor
}

// NewResizerService loads (or creates) the config. firstRun is true when
// no config file existed, so the UI can show the settings window once.
func NewResizerService() (svc *ResizerService, firstRun bool, err error) {
	cfg, firstRun, err := Load()
	if err != nil {
		return nil, false, err
	}
	svc = &ResizerService{cfg: cfg}
	svc.tracker = newTargetTracker(liveListWindows, os.Getpid(), targetPollInterval)
	return svc, firstRun, nil
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

// findWindowByID re-lists windows and returns the one with the given OS
// window handle. A fresh listing is deliberate: the tracker snapshot may
// hold stale bounds, and the window may have closed since.
func findWindowByID(id string) (windowctl.Window, error) {
	if strings.TrimSpace(id) == "" {
		return windowctl.Window{}, fmt.Errorf("empty window id")
	}
	// Deliberately unfiltered: the user may switch desktops between
	// picking a window and acting on it, and windowctl.Resize matches
	// across all desktops.
	windows, err := windowctl.ListWindows(windowctl.Filter{})
	if err != nil {
		return windowctl.Window{}, fmt.Errorf("list windows: %w", err)
	}
	for _, w := range windows {
		if w.ID == id {
			return w, nil
		}
	}
	return windowctl.Window{}, fmt.Errorf("window %q no longer exists (closed?)", id)
}

// recordLocked stores w's current size. Caller must hold s.mu.
func (s *ResizerService) recordLocked(w windowctl.Window) (string, error) {
	if strings.TrimSpace(w.App) == "" {
		return "", fmt.Errorf("window has no app name; cannot record")
	}
	if w.Bounds.W <= 0 || w.Bounds.H <= 0 {
		return "", fmt.Errorf("window has invalid size %dx%d", w.Bounds.W, w.Bounds.H)
	}
	id := normalizeAppID(w.App)
	p := s.cfg.FindOrCreate(id, w.App)
	_, isNew := p.RecordSize(w.Bounds.W, w.Bounds.H)
	if err := s.cfg.Save(); err != nil {
		return "", err
	}
	// The record may have happened behind the console's back (picker,
	// hotkey), so poke its page to re-read state — see notifyPresetsChanged.
	s.notifyPresetsChanged()
	if isNew {
		return fmt.Sprintf("Recorded %dx%d for %s (preset #%d of %d)", w.Bounds.W, w.Bounds.H, w.App, len(p.Sizes), len(p.Sizes)), nil
	}
	return fmt.Sprintf("%dx%d already recorded for %s", w.Bounds.W, w.Bounds.H, w.App), nil
}

// notifyPresetsChanged pokes the console page that presets were written
// behind its back. The console re-reads state when focus returns to it, but
// Wails' GTK3 backend never forwards focus-in on Linux, so records made
// outside the console (picker, hotkey) would leave the Presets list stale
// until a manual reload. Harmless duplicate refresh where focus events work.
func (s *ResizerService) notifyPresetsChanged() {
	if s.console == nil {
		return
	}
	s.console.EmitEvent("presets-changed")
}

// showConsole raises the console window, creating it on first use (see
// createConsoleWindow). Creation, the Wayland client-side decoration prep
// and the first Show always run back to back here, which is what keeps
// compositors from ever drawing a system title bar over the console's
// own (see prepClientDecorations). Reached from the tray, the overlay
// close, and the first-run startup show in main.go.
func (s *ResizerService) showConsole() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.app == nil {
		return
	}
	if s.console == nil {
		s.console = createConsoleWindow(s.app)
		prepClientDecorations(s.console)
		// Push target changes to the console instead of waiting for a
		// focus event: Wails' GTK3 backend never forwards focus-in on
		// Linux (only focus-out is connected), so refocusing an already
		// visible console would otherwise leave its target card stale.
		// Capture the pointer here so the callback never reads s.console
		// off the service lock.
		if s.tracker != nil {
			console := s.console
			s.tracker.setOnChange(func() {
				console.EmitEvent("target-changed")
			})
		}
	}
	s.console.Show()
	s.console.Focus()
}

// cycleLocked resizes w to its app's next recorded size. Caller must hold s.mu.
func (s *ResizerService) cycleLocked(w windowctl.Window) (string, error) {
	if strings.TrimSpace(w.App) == "" {
		return "", fmt.Errorf("window has no app name")
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

// RecordFocused stores the focused window's current size in its app
// preset. Used by the global-hotkey path, which never steals focus.
func (s *ResizerService) RecordFocused() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := focusedWindow()
	if err != nil {
		return "", err
	}
	return s.recordLocked(w)
}

// CycleFocused resizes the focused window to the next recorded size for
// its app. This is what the global hotkey invokes.
func (s *ResizerService) CycleFocused() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := focusedWindow()
	if err != nil {
		return "", err
	}
	return s.cycleLocked(w)
}

// GetTarget returns the tracked target window: the most recently focused
// window outside our own process. The UI acts on this instead of the
// currently focused window (which is us whenever the user clicks our UI).
func (s *ResizerService) GetTarget() (WindowInfo, error) {
	// Poll synchronously first, but silently. This read happens exactly
	// when focus returns to us, i.e. when "the window the user was just
	// in" is the answer — and the background ticker can miss a fast
	// A→B→WinResizer switch that lands inside one interval. The poll
	// must not announce the change: this response already carries the
	// fresh target, and announcing it would make the console read it
	// again — and again, for as long as the target keeps changing.
	s.tracker.pollQuiet()
	snap, ok := s.tracker.get()
	if !ok {
		return WindowInfo{}, fmt.Errorf("no target window yet — focus another app's window first")
	}
	// Refresh bounds from a live listing so the UI shows the current
	// size; fall back to the snapshot if the window vanished mid-poll.
	win := snap
	if fresh, err := findWindowByID(snap.ID); err == nil {
		win = fresh
	}
	info := toWindowInfo(win)
	s.mu.Lock()
	info.IsRecorded = NewSizeIndex(s.cfg.Apps).Has(win.App, win.Bounds.W, win.Bounds.H)
	s.mu.Unlock()
	return info, nil
}

// ListWindows returns the current desktop's windows for the manual
// selector. Our own settings window is flagged via Self so the UI can
// grey it out.
func (s *ResizerService) ListWindows() ([]WindowInfo, error) {
	windows, err := currentDesktopWindows()
	if err != nil {
		return nil, fmt.Errorf("list windows: %w", err)
	}
	self := os.Getpid()
	out := make([]WindowInfo, 0, len(windows))
	for _, w := range windows {
		info := toWindowInfo(w)
		info.Self = w.PID == self
		out = append(out, info)
	}
	return out, nil
}

// RecordWindow records the current size of the window with the given OS
// window ID (from GetTarget or ListWindows). The recorded window also
// becomes the tracked target: the user picked it deliberately, so the
// console should keep acting on it until focus moves elsewhere.
func (s *ResizerService) RecordWindow(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := findWindowByID(id)
	if err != nil {
		return "", err
	}
	msg, err := s.recordLocked(w)
	if err != nil {
		return "", err
	}
	s.tracker.set(w)
	return msg, nil
}

// CycleWindow resizes the window with the given OS window ID to its
// app's next recorded size.
func (s *ResizerService) CycleWindow(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := findWindowByID(id)
	if err != nil {
		return "", err
	}
	return s.cycleLocked(w)
}

// RecordTarget records the tracked target window's current size. This is
// the UI/tray path — it never depends on current focus.
func (s *ResizerService) RecordTarget() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.tracker.get()
	if !ok {
		return "", fmt.Errorf("no target window yet — focus another app's window first")
	}
	w, err := findWindowByID(snap.ID)
	if err != nil {
		return "", err
	}
	return s.recordLocked(w)
}

// CycleTarget resizes the tracked target window to its app's next
// recorded size. This is the tray-menu path.
func (s *ResizerService) CycleTarget() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.tracker.get()
	if !ok {
		return "", fmt.Errorf("no target window yet — focus another app's window first")
	}
	w, err := findWindowByID(snap.ID)
	if err != nil {
		return "", err
	}
	return s.cycleLocked(w)
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
// Console on first call).
func (s *ResizerService) RequestAX() error {
	return windowctl.RequestAccessibility()
}
