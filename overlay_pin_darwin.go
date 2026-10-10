//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Cocoa

#include <stdlib.h>

void winresizerPinWindow(const char *title);
*/
import "C"
import "unsafe"

// pinOverlayWindow freezes the named window against user-driven dragging
// (see overlay_pin_darwin.m). title must match the window's Wails Title.
// Cheap and idempotent: safe to call on every open.
func pinOverlayWindow(title string) {
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	C.winresizerPinWindow(cTitle)
}
