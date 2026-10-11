package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNextSizeCycles(t *testing.T) {
	p := &AppPreset{Sizes: []Size{{W: 800, H: 600}, {W: 1200, H: 800}}}
	// LastIndex 0 -> next is 1, then wraps to 0.
	s, ok := p.NextSize()
	if !ok || s != (Size{W: 1200, H: 800}) {
		t.Fatalf("first NextSize = %+v,%v, want {1200 800},true", s, ok)
	}
	s, _ = p.NextSize()
	if s != (Size{W: 800, H: 600}) {
		t.Fatalf("wrapped NextSize = %+v, want {800 600}", s)
	}
}

func TestNextSizeEmpty(t *testing.T) {
	p := &AppPreset{}
	if _, ok := p.NextSize(); ok {
		t.Fatal("NextSize on empty preset should report ok=false")
	}
}

func TestNextSizeClampsBadIndex(t *testing.T) {
	p := &AppPreset{Sizes: []Size{{W: 1, H: 1}, {W: 2, H: 2}}, LastIndex: 99}
	s, ok := p.NextSize()
	if !ok || s != (Size{W: 2, H: 2}) {
		t.Fatalf("NextSize with bad index = %+v,%v, want {2 2},true", s, ok)
	}
}

func TestSizeIndexHas(t *testing.T) {
	ix := NewSizeIndex([]AppPreset{
		{ID: "safari", Sizes: []Size{{W: 800, H: 600}, {W: 1200, H: 800}}},
		{ID: "code"}, // app known, no sizes yet
	})

	if !ix.Has("Safari", 800, 600) || !ix.Has("Safari", 1200, 800) {
		t.Fatal("Has missed a recorded size")
	}
	// Lookup takes an OS-reported name, so it must normalize the same way
	// RecordSize's preset id does.
	if !ix.Has("  sAfArI  ", 800, 600) {
		t.Fatal("Has did not normalize the app name")
	}
	if ix.Has("Safari", 999, 999) {
		t.Fatal("Has matched a size that was never recorded")
	}
	if ix.Has("Code", 800, 600) {
		t.Fatal("Has matched an app that has no sizes")
	}
	if ix.Has("Firefox", 800, 600) {
		t.Fatal("Has matched an app with no preset at all")
	}
}

func TestSizeIndexEmptyConfig(t *testing.T) {
	if NewSizeIndex(nil).Has("Safari", 800, 600) {
		t.Fatal("empty index reported a hit")
	}
}

func TestRecordSizeDedupes(t *testing.T) {
	p := &AppPreset{}
	i, isNew := p.RecordSize(800, 600)
	if !isNew || i != 0 || len(p.Sizes) != 1 {
		t.Fatalf("first record = (%d,%v), sizes=%+v", i, isNew, p.Sizes)
	}
	i, isNew = p.RecordSize(800, 600)
	if isNew || i != 0 || len(p.Sizes) != 1 {
		t.Fatalf("duplicate record = (%d,%v), sizes=%+v", i, isNew, p.Sizes)
	}
	if _, isNew := p.RecordSize(1200, 800); !isNew {
		t.Fatal("new size should report isNew=true")
	}
	// LastIndex must point at the recorded size so the hotkey advances.
	if p.LastIndex != 1 {
		t.Fatalf("LastIndex = %d, want 1", p.LastIndex)
	}
}

func TestFindOrCreate(t *testing.T) {
	c := DefaultConfig()
	a := c.FindOrCreate("firefox", "Firefox")
	b := c.FindOrCreate("firefox", "Firefox")
	if a != b {
		t.Fatal("FindOrCreate should return the same preset for the same id")
	}
	if c.Find("missing") != nil {
		t.Fatal("Find should return nil for unknown id")
	}
	if !c.Remove("firefox") || c.Remove("firefox") {
		t.Fatal("Remove should report true once, then false")
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WINRESIZER_CONFIG", filepath.Join(dir, "config.json"))

	c, firstRun, err := Load()
	if err != nil || !firstRun {
		t.Fatalf("Load missing = (%v,%v,%v), want defaults,firstRun", c, firstRun, err)
	}
	p := c.FindOrCreate("code", "Code")
	p.RecordSize(1400, 900)
	p.RecordSize(1800, 1000)
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, firstRun, err := Load()
	if err != nil || firstRun {
		t.Fatalf("Load existing = (%v,%v,%v)", loaded, firstRun, err)
	}
	got := loaded.Find("code")
	if got == nil || len(got.Sizes) != 2 || got.AppName != "Code" {
		t.Fatalf("roundtrip preset = %+v", got)
	}
	if loaded.Hotkey != DefaultHotkey {
		t.Fatalf("roundtrip hotkey = %q, want %q", loaded.Hotkey, DefaultHotkey)
	}
}

func TestLoadRejectsCorrupt(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	t.Setenv("WINRESIZER_CONFIG", p)
	if err := os.WriteFile(p, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(); err == nil {
		t.Fatal("Load should reject corrupt JSON")
	}
}

func TestNormalizeDropsInvalidSizes(t *testing.T) {
	c := &Config{Version: 1, Apps: []AppPreset{
		{ID: "x", Sizes: []Size{{W: 0, H: 5}, {W: 100, H: 100}}, LastIndex: 7},
	}}
	c.normalize()
	p := c.Find("x")
	if len(p.Sizes) != 1 || p.LastIndex != 0 {
		t.Fatalf("normalized = %+v", p)
	}
	if c.Hotkey != DefaultHotkey {
		t.Fatalf("empty hotkey should default, got %q", c.Hotkey)
	}
}
