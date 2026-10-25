//go:build linux && cgo && gtk3

package main

// Wayland background blur for the console window.
//
// Windows gets its frosted backdrop from DWM (Acrylic) and macOS from
// NSVisualEffectView (vibrancy); both are compositor features the OS applies
// behind a translucent window. Linux has no equivalent in Wails' GTK3
// backend — BackgroundTypeTranslucent only gives us a translucent surface —
// but compositors do expose one: the ext-background-effect-v1 protocol
// (KWin 6.7+, Hyprland 0.56+, niri, Mutter 51+, Cosmic 1.3+). A client binds
// ext_background_effect_manager_v1, asks for a background effect object for
// its wl_surface and hands over a blur region; the compositor then blurs
// whatever sits behind that region of the surface.
//
// The frontend already draws its translucent tint over the native backdrop
// (see frontend/public/style.css), so once the compositor blurs, the console
// looks the same as it does on Windows and macOS.
//
// Two properties of the protocol shape the code below:
//
//   - The blur region is double-buffered surface state: it only takes effect
//     on the next wl_surface.commit. The region is therefore set as one
//     oversized rectangle, which the compositor clips to the surface size
//     (per the protocol text), so a resize never needs a new region.
//   - The capabilities event is sent right after the manager is bound, so the
//     blur may only be requested once that event has arrived. The manager is
//     bound as early as possible (at startup) to leave the compositor time
//     to answer before the console is first shown.
//
// Everything here runs on the GTK main thread — the thread that owns the
// wl_display — and the protocol objects live in file-scope statics so the
// Wayland callbacks can reach them.

/*
#cgo pkg-config: gtk+-3.0 wayland-client
#cgo CFLAGS: -I${SRCDIR}

#include <gtk/gtk.h>
#include <gdk/gdkwayland.h>
#include <stdint.h>
#include <string.h>

#include "protocol/ext-background-effect-v1-client-protocol.h"
#include "protocol/ext-background-effect-v1-protocol.c"

// The protocol's client-side bindings are generated from
// protocol/ext-background-effect-v1.xml by wayland-scanner; the generated
// code is vendored so the build does not need wayland-scanner installed.
//
// Regenerate with:
//   wayland-scanner client-header protocol/ext-background-effect-v1.xml \
//       protocol/ext-background-effect-v1-client-protocol.h
//   wayland-scanner private-code protocol/ext-background-effect-v1.xml \
//       protocol/ext-background-effect-v1-protocol.c

static struct wl_display *bg_display = NULL;
static struct wl_registry *bg_registry = NULL;
static struct wl_compositor *bg_compositor = NULL;
static struct ext_background_effect_manager_v1 *bg_manager = NULL;
static struct ext_background_effect_surface_v1 *bg_effect = NULL;
static struct wl_surface *bg_surface = NULL;
static GtkWidget *bg_widget = NULL;
static uint32_t bg_capabilities = 0;
static int bg_region_set = 0;

// bg_apply_region hands the compositor the region it should blur. The
// rectangle is deliberately oversized: the protocol clips the region to the
// surface size, so one region covers the window at any size.
static void bg_apply_region(void) {
	if (bg_effect == NULL || bg_compositor == NULL || bg_region_set) {
		return;
	}
	struct wl_region *region = wl_compositor_create_region(bg_compositor);
	if (region == NULL) {
		return;
	}
	wl_region_add(region, 0, 0, 100000, 100000);
	ext_background_effect_surface_v1_set_blur_region(bg_effect, region);
	wl_region_destroy(region);
	bg_region_set = 1;
	// The region is published on the next surface commit. When it lands
	// after the window was drawn, nothing would commit it on its own, so
	// ask for a repaint.
	if (bg_widget != NULL) {
		gtk_widget_queue_draw(bg_widget);
	}
}

// bg_attach_effect creates the per-surface background effect object. It may
// only run once the compositor has told us it supports blur.
static void bg_attach_effect(void) {
	if (bg_effect != NULL || bg_surface == NULL) {
		return;
	}
	if (!(bg_capabilities & EXT_BACKGROUND_EFFECT_MANAGER_V1_CAPABILITY_BLUR)) {
		return;
	}
	bg_effect = ext_background_effect_manager_v1_get_background_effect(bg_manager, bg_surface);
	bg_apply_region();
}

static void bg_handle_capabilities(void *data,
	struct ext_background_effect_manager_v1 *manager, uint32_t flags) {
	bg_capabilities = flags;
	bg_attach_effect();
}

static const struct ext_background_effect_manager_v1_listener bg_manager_listener = {
	.capabilities = bg_handle_capabilities,
};

static void bg_handle_global(void *data, struct wl_registry *registry,
	uint32_t name, const char *interface, uint32_t version) {
	if (bg_manager == NULL &&
		strcmp(interface, ext_background_effect_manager_v1_interface.name) == 0) {
		bg_manager = wl_registry_bind(registry, name,
			&ext_background_effect_manager_v1_interface, 1);
		if (bg_manager != NULL) {
			ext_background_effect_manager_v1_add_listener(bg_manager, &bg_manager_listener, NULL);
		}
	} else if (bg_compositor == NULL &&
		strcmp(interface, wl_compositor_interface.name) == 0) {
		bg_compositor = wl_registry_bind(registry, name, &wl_compositor_interface, 1);
	}
	// The compositor global and the capabilities event arrive in no
	// particular order, and the blur region needs both the effect object
	// and the compositor — so try again on every step.
	bg_attach_effect();
	bg_apply_region();
}

static const struct wl_registry_listener bg_registry_listener = {
	.global = bg_handle_global,
};

// bg_bind_manager binds the background effect manager. It is a no-op when
// the compositor does not implement the protocol (the global never shows up)
// or when the display is not Wayland.
static void bg_bind_manager(void) {
	if (bg_registry != NULL || bg_display == NULL) {
		return;
	}
	bg_registry = wl_display_get_registry(bg_display);
	if (bg_registry == NULL) {
		return;
	}
	wl_registry_add_listener(bg_registry, &bg_registry_listener, NULL);
	wl_display_flush(bg_display);
}

// bg_prepare binds the manager ahead of the window that needs it, so the
// compositor's capabilities event has a chance to arrive before the console
// is first shown.
static void bg_prepare(void) {
	GdkDisplay *display = gdk_display_get_default();
	if (display == NULL || !GDK_IS_WAYLAND_DISPLAY(display)) {
		return; // X11 has no such protocol; nothing to prepare
	}
	bg_display = gdk_wayland_display_get_wl_display(display);
	bg_bind_manager();
}

// bg_attach gives the window's surface a blurred background. Called once the
// window is realized, so its wl_surface exists.
static void bg_attach(void *native_window) {
	GtkWidget *widget = GTK_WIDGET(native_window);
	if (widget == NULL) {
		return;
	}
	GdkDisplay *display = gtk_widget_get_display(widget);
	if (display == NULL || !GDK_IS_WAYLAND_DISPLAY(display)) {
		return;
	}
	if (bg_display == NULL) {
		bg_display = gdk_wayland_display_get_wl_display(display);
	}
	bg_bind_manager();

	GdkWindow *gdk_window = gtk_widget_get_window(widget);
	if (gdk_window == NULL) {
		return;
	}
	struct wl_surface *surface = gdk_wayland_window_get_wl_surface(gdk_window);
	if (surface == NULL) {
		return;
	}
	bg_surface = surface;
	bg_widget = widget;

	// The blur may already be available (the manager was bound at
	// startup); otherwise it is attached once the capabilities event
	// arrives.
	bg_attach_effect();
}
*/
import "C"

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// prepareBackgroundBlur binds the compositor's background effect manager so
// the blur is available by the time the console first appears. A no-op on
// compositors without the protocol.
func prepareBackgroundBlur() {
	application.InvokeSync(func() {
		C.bg_prepare()
	})
}

// enableBackgroundBlur asks the compositor to blur behind the given window.
// The window must already be realized. A no-op off Wayland, and on
// compositors that do not implement ext-background-effect-v1.
func enableBackgroundBlur(w *application.WebviewWindow) {
	if w == nil {
		return
	}
	application.InvokeSync(func() {
		native := w.NativeWindow() // the GtkWindow
		if native != nil {
			C.bg_attach(native)
		}
	})
}
