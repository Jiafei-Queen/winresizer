//go:build linux && cgo && gtk3

package main

// Wails' GTK3 backend implements Frameless as gtk_window_set_decorated
// (FALSE), which only removes GTK's own client-side chrome. GTK's realize
// path still tells the Wayland compositor to decorate the window — it
// announces MODE_SERVER via the org_kde_kwin_server_decoration protocol
// whenever the window has no custom titlebar widget, regardless of the
// decorated flag — so compositors that draw server-side decorations (KWin)
// put a system title bar above the app's drawn one.
//
// gdk_wayland_window_announce_csd() flips GDK's stored preference to
// CLIENT. Announced before the window's first map, the compositor never
// draws the bar at all; announced later, the bar appears first and the
// window then grows by the bar's height when it is removed, so windows
// are always prepped between creation and their first Show. GDK keeps
// the preference on the window, so later re-maps (Show after Hide)
// announce client-side decorations on their own.

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>
#include <gdk/gdkwayland.h>

static void prep_csd(void *window) {
	GtkWidget *widget = GTK_WIDGET(window);
	if (!GDK_IS_WAYLAND_DISPLAY(gtk_widget_get_display(widget))) {
		return; // X11 negotiates decorations via Motif hints instead
	}
	if (!gtk_widget_get_realized(widget)) {
		// Create the GdkWindow without mapping the surface, so the
		// announcement lands before the compositor sees the first map.
		gtk_widget_realize(widget);
	}
	gdk_wayland_window_announce_csd(gtk_widget_get_window(widget));
}
*/
import "C"

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// prepClientDecorations makes a frameless window announce client-side
// decorations to the compositor before it is first shown, avoiding the
// system title bar Wayland compositors would otherwise draw over it.
// Runs on the GTK main thread; a no-op off Wayland or before the window's
// underlying widget exists.
func prepClientDecorations(w *application.WebviewWindow) {
	if w == nil {
		return
	}
	application.InvokeSync(func() {
		native := w.NativeWindow() // the GtkWindow
		if native != nil {
			C.prep_csd(native)
		}
	})
}
