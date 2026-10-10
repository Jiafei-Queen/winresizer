//go:build production

package main

// devBuild is false for production builds (`wails3 build` passes
// -tags production). The settings window then follows the tray-app
// behavior: shown on first run only, hidden afterwards.
const devBuild = false
