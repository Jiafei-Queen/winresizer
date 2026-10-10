//go:build !darwin

package main

// pinOverlayWindow is a no-op off macOS. GTK (Linux) frameless windows
// are not background-draggable by default, and Windows is deferred; if a
// platform turns out to allow dragging, add its pin implementation there
// and keep this stub for the rest.
func pinOverlayWindow(title string) {}
