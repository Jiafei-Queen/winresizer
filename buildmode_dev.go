//go:build !production

package main

// devBuild marks development builds — everything except `wails3 build`
// (which passes -tags production on every platform). While iterating, the
// settings window is always shown on startup so the app is reachable even
// though it runs as a tray app.
const devBuild = true
