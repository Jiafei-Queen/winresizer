package main

import (
	"testing"

	"github.com/muthuishere/windowctl"
)

func TestOverlaySingleMonitor(t *testing.T) {
	monitors := []windowctl.Monitor{{ID: 1, X: 0, Y: 0, Width: 1440, Height: 900, Primary: true}}
	w := windowctl.Window{ID: "7", App: "Safari", Title: "Start", PID: 500}
	w.Bounds.X, w.Bounds.Y, w.Bounds.W, w.Bounds.H = 100, 200, 800, 600

	data, err := computeOverlayData(monitors, []windowctl.Window{w}, 999, nil)
	if err != nil {
		t.Fatalf("computeOverlayData: %v", err)
	}
	if data.Placement != (OverlayPlacement{X: 0, Y: 0, W: 1440, H: 900}) {
		t.Fatalf("placement = %+v", data.Placement)
	}
	if len(data.Windows) != 1 {
		t.Fatalf("windows = %+v", data.Windows)
	}
	// Same top-left space (probe-verified on macOS): local == global - union origin.
	want := OverlayRect{ID: "7", App: "Safari", Title: "Start", X: 100, Y: 200, W: 800, H: 600}
	if data.Windows[0] != want {
		t.Fatalf("rect = %+v, want %+v", data.Windows[0], want)
	}
}

func TestOverlayDualMonitorNegativeOffset(t *testing.T) {
	monitors := []windowctl.Monitor{
		{ID: 1, X: 0, Y: 0, Width: 1440, Height: 900, Primary: true},
		{ID: 2, X: -1440, Y: 0, Width: 1440, Height: 900},
	}
	w := windowctl.Window{ID: "9", App: "Code", PID: 500}
	w.Bounds.X, w.Bounds.Y, w.Bounds.W, w.Bounds.H = -1300, 100, 800, 600

	data, err := computeOverlayData(monitors, []windowctl.Window{w}, 999, nil)
	if err != nil {
		t.Fatalf("computeOverlayData: %v", err)
	}
	if data.Placement != (OverlayPlacement{X: -1440, Y: 0, W: 2880, H: 900}) {
		t.Fatalf("placement = %+v", data.Placement)
	}
	got := data.Windows[0]
	if got.X != 140 || got.Y != 100 || got.W != 800 || got.H != 600 {
		t.Fatalf("rect = %+v", got)
	}
}

func TestOverlaySkipsSelfSystemAndDegenerate(t *testing.T) {
	monitors := []windowctl.Monitor{{ID: 1, X: 0, Y: 0, Width: 1440, Height: 900, Primary: true}}
	self := windowctl.Window{ID: "1", App: "WinResizer", PID: 999}
	self.Bounds.W, self.Bounds.H = 520, 640
	dock := windowctl.Window{ID: "2", App: "Dock", PID: 200}
	dock.Bounds.W, dock.Bounds.H = 100, 100
	flat := windowctl.Window{ID: "3", App: "X", PID: 100} // zero bounds
	good := windowctl.Window{ID: "4", App: "Y", PID: 100}
	good.Bounds.W, good.Bounds.H = 100, 100

	data, err := computeOverlayData(monitors, []windowctl.Window{self, dock, flat, good}, 999, nil)
	if err != nil {
		t.Fatalf("computeOverlayData: %v", err)
	}
	if len(data.Windows) != 1 || data.Windows[0].ID != "4" {
		t.Fatalf("windows = %+v, want only ID=4", data.Windows)
	}
}

func TestOverlayNoMonitors(t *testing.T) {
	if _, err := computeOverlayData(nil, nil, 1, nil); err == nil {
		t.Fatal("expected error with no monitors")
	}
}

func TestOverlaySecondaryOnlyUnion(t *testing.T) {
	// No monitor flagged primary: union is still well-defined.
	monitors := []windowctl.Monitor{{ID: 5, X: 100, Y: 50, Width: 1440, Height: 900}}
	data, err := computeOverlayData(monitors, nil, 1, nil)
	if err != nil {
		t.Fatalf("computeOverlayData: %v", err)
	}
	if data.Placement != (OverlayPlacement{X: 100, Y: 50, W: 1440, H: 900}) {
		t.Fatalf("placement = %+v", data.Placement)
	}
}

// A window carrying an already-saved size must be flagged so the picker
// can mark it instead of offering a record that could only be a duplicate.
func TestOverlayFlagsRecordedSizes(t *testing.T) {
	monitors := []windowctl.Monitor{{ID: 1, X: 0, Y: 0, Width: 1440, Height: 900, Primary: true}}
	saved := windowctl.Window{ID: "1", App: "Safari", PID: 500}
	saved.Bounds.W, saved.Bounds.H = 800, 600
	other := windowctl.Window{ID: "2", App: "Safari", PID: 500}
	other.Bounds.W, other.Bounds.H = 1024, 768
	foreign := windowctl.Window{ID: "3", App: "Code", PID: 500}
	foreign.Bounds.W, foreign.Bounds.H = 800, 600

	ix := NewSizeIndex([]AppPreset{{
		ID:    "safari",
		Sizes: []Size{{W: 800, H: 600}},
	}})
	data, err := computeOverlayData(monitors, []windowctl.Window{saved, other, foreign}, 999, ix)
	if err != nil {
		t.Fatalf("computeOverlayData: %v", err)
	}
	want := map[string]bool{"1": true, "2": false, "3": false}
	for _, r := range data.Windows {
		if r.IsRecorded != want[r.ID] {
			t.Fatalf("window %s IsRecorded = %v, want %v", r.ID, r.IsRecorded, want[r.ID])
		}
	}
}

func TestRebaseCompensatesMenuBarClamp(t *testing.T) {
	// macOS constrainFrameRect moves a requested top of 0 down to the
	// menu bar's lower edge (Y=33 on the probe machine). Rebasing to the
	// actual origin must shift outlines up by exactly that delta.
	data := OverlayData{
		Placement: OverlayPlacement{X: 0, Y: 0, W: 1728, H: 1117},
		Windows:   []OverlayRect{{ID: "1", App: "A", Title: "T", X: 100, Y: 200, W: 500, H: 300}},
	}
	rebaseOverlay(&data, 0, 33)
	want := OverlayRect{ID: "1", App: "A", Title: "T", X: 100, Y: 167, W: 500, H: 300}
	if data.Windows[0] != want {
		t.Fatalf("rect = %+v, want %+v", data.Windows[0], want)
	}
	if data.Placement != (OverlayPlacement{X: 0, Y: 33, W: 1728, H: 1117}) {
		t.Fatalf("placement = %+v", data.Placement)
	}
}

func TestRebaseNoopWhenUnclamped(t *testing.T) {
	data := OverlayData{
		Placement: OverlayPlacement{X: -1440, Y: 0, W: 2880, H: 900},
		Windows:   []OverlayRect{{ID: "1", X: 100, Y: 200, W: 500, H: 300}},
	}
	rebaseOverlay(&data, -1440, 0)
	if data.Windows[0].X != 100 || data.Windows[0].Y != 200 {
		t.Fatalf("rect = %+v, want unchanged", data.Windows[0])
	}
	if data.Placement != (OverlayPlacement{X: -1440, Y: 0, W: 2880, H: 900}) {
		t.Fatalf("placement = %+v", data.Placement)
	}
}

func TestRebaseHandlesBothAxes(t *testing.T) {
	data := OverlayData{
		Placement: OverlayPlacement{X: 0, Y: 0, W: 1728, H: 1117},
		Windows:   []OverlayRect{{ID: "1", X: 300, Y: 400, W: 500, H: 300}},
	}
	rebaseOverlay(&data, 12, 33)
	if data.Windows[0].X != 288 || data.Windows[0].Y != 367 {
		t.Fatalf("rect = %+v", data.Windows[0])
	}
}

func TestSnapBackTargetSnapsDrift(t *testing.T) {
	// A dragged (or OS-nudged) overlay must be told to return to anchor.
	var a overlayAnchor
	a.set(0, 33)
	x, y, ok := snapBackTarget(&a, 250, 180)
	if !ok || x != 0 || y != 33 {
		t.Fatalf("snapBackTarget = (%d,%d,%v), want (0,33,true)", x, y, ok)
	}
}

func TestSnapBackTargetIgnoresOwnPositioning(t *testing.T) {
	var a overlayAnchor
	a.set(0, 33)
	// Still at the anchor: our own SetPosition fired the event.
	if _, _, ok := snapBackTarget(&a, 0, 33); ok {
		t.Fatal("want no snap-back while at anchor")
	}
	// Suspended: OpenOverlay is mid-move (e.g. re-placement after a
	// monitor layout change); the old anchor must not win.
	a.suspendTracking()
	if _, _, ok := snapBackTarget(&a, 250, 180); ok {
		t.Fatal("want no snap-back while suspended")
	}
}

func TestSnapBackTargetBeforeFirstAnchor(t *testing.T) {
	var a overlayAnchor
	// Not armed yet: no anchor recorded, nothing to snap back to.
	if _, _, ok := snapBackTarget(&a, 10, 10); ok {
		t.Fatal("want no snap-back before the first anchor")
	}
}
