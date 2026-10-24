//go:build !linux || !cgo || !gtk3

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// prepClientDecorations is a no-op wherever the GTK3 backend is not in
// use: macOS and Windows draw no such extra bar, and Wails' GTK4 backend
// negotiates client-side decorations correctly on Wayland by itself.
func prepClientDecorations(_ *application.WebviewWindow) {}
