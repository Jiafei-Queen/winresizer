//go:build !(linux && cgo && gtk3)

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// prepareBackgroundBlur is a no-op off Wayland. Windows and macOS get their
// frosted backdrop from the OS (DWM Acrylic / NSVisualEffectView), and
// Wayland compositors without ext-background-effect-v1 have nothing to ask.
func prepareBackgroundBlur() {}

// enableBackgroundBlur is a no-op off Wayland — see prepareBackgroundBlur.
func enableBackgroundBlur(w *application.WebviewWindow) {}
