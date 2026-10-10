package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ConfigVersion is the current on-disk schema version. Bump it when the
// JSON shape changes and add a migration in Load.
const ConfigVersion = 1

// DefaultHotkey is the global shortcut registered on startup.
// Deliberately NOT Meta/Cmd+S: that collides with "Save" in almost every
// app. Changing it via SetHotkey takes effect after an app restart.
const DefaultHotkey = "Ctrl+Alt+R"

// Size is a recorded window size (dimensions only; position is left alone
// by Resize, which keeps the window's current X/Y).
type Size struct {
	W int `json:"w"`
	H int `json:"h"`
}

// AppPreset groups the recorded sizes for one application.
// ID is the normalized lookup key (see normalizeAppID); AppName is the
// display name as reported by the OS. BundleID is reserved for a future
// platform resolver (macOS bundle id / Linux WM_CLASS) and left empty by
// the MVP name-based resolver.
type AppPreset struct {
	ID       string `json:"id"`
	AppName  string `json:"appName"`
	BundleID string `json:"bundleId,omitempty"`
	Sizes    []Size `json:"sizes"`
	// LastIndex is the index into Sizes that was last applied (or last
	// recorded). The hotkey advances from here.
	LastIndex int `json:"lastIndex"`
}

// Config is the persisted user configuration.
type Config struct {
	Version int         `json:"version"`
	Hotkey  string      `json:"hotkey"`
	Apps    []AppPreset `json:"apps"`
}

// DefaultConfig returns a fresh config with sane defaults.
func DefaultConfig() *Config {
	return &Config{Version: ConfigVersion, Hotkey: DefaultHotkey, Apps: []AppPreset{}}
}

// ConfigPath returns the config file location:
// $XDG_CONFIG_HOME/winresizer/config.json, falling back to
// ~/.config/winresizer/config.json. WINRESIZER_CONFIG overrides it
// (useful for tests).
func ConfigPath() (string, error) {
	if p := os.Getenv("WINRESIZER_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	return filepath.Join(dir, "winresizer", "config.json"), nil
}

// Load reads the config from disk. A missing file yields defaults (and
// reports firstRun=true so the UI can show the settings window once).
// Corrupt or future-version files are rejected with an error instead of
// being silently overwritten.
func Load() (cfg *Config, firstRun bool, err error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, false, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultConfig(), true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read config: %w", err)
	}
	cfg = DefaultConfig()
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, false, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.Version > ConfigVersion {
		return nil, false, fmt.Errorf("config version %d is newer than supported %d", cfg.Version, ConfigVersion)
	}
	cfg.normalize()
	return cfg, false, nil
}

// Save persists the config atomically (temp file + rename).
func (c *Config) Save() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "config-*.json")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// normalize clamps LastIndex into range and drops invalid sizes so a
// hand-edited file can't panic the cycler.
func (c *Config) normalize() {
	if c.Hotkey == "" {
		c.Hotkey = DefaultHotkey
	}
	for i := range c.Apps {
		p := &c.Apps[i]
		kept := p.Sizes[:0]
		for _, s := range p.Sizes {
			if s.W > 0 && s.H > 0 {
				kept = append(kept, s)
			}
		}
		p.Sizes = kept
		if len(p.Sizes) == 0 {
			p.LastIndex = 0
		} else if p.LastIndex < 0 || p.LastIndex >= len(p.Sizes) {
			p.LastIndex = 0
		}
	}
}

// Find returns the preset for id, or nil.
func (c *Config) Find(id string) *AppPreset {
	for i := range c.Apps {
		if c.Apps[i].ID == id {
			return &c.Apps[i]
		}
	}
	return nil
}

// FindOrCreate returns the preset for id, creating it (with appName) when
// absent.
func (c *Config) FindOrCreate(id, appName string) *AppPreset {
	if p := c.Find(id); p != nil {
		if p.AppName == "" {
			p.AppName = appName
		}
		return p
	}
	c.Apps = append(c.Apps, AppPreset{ID: id, AppName: appName})
	return &c.Apps[len(c.Apps)-1]
}

// Remove deletes the preset for id. It reports whether one existed.
func (c *Config) Remove(id string) bool {
	for i := range c.Apps {
		if c.Apps[i].ID == id {
			c.Apps = append(c.Apps[:i], c.Apps[i+1:]...)
			return true
		}
	}
	return false
}

// NextSize advances LastIndex and returns the size now due. ok=false when
// no sizes are recorded yet.
func (p *AppPreset) NextSize() (s Size, ok bool) {
	if len(p.Sizes) == 0 {
		return Size{}, false
	}
	if p.LastIndex < 0 || p.LastIndex >= len(p.Sizes) {
		p.LastIndex = 0
	}
	p.LastIndex = (p.LastIndex + 1) % len(p.Sizes)
	return p.Sizes[p.LastIndex], true
}

// RecordSize appends (w,h) unless already recorded, and points LastIndex
// at it so the next hotkey press moves on to the following size.
func (p *AppPreset) RecordSize(w, h int) (index int, isNew bool) {
	for i, s := range p.Sizes {
		if s.W == w && s.H == h {
			p.LastIndex = i
			return i, false
		}
	}
	p.Sizes = append(p.Sizes, Size{W: w, H: h})
	p.LastIndex = len(p.Sizes) - 1
	return p.LastIndex, true
}
