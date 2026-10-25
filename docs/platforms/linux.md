# Linux

The Linux build is not just "the same app on another OS". Three areas need
their own treatment: the global shortcut on Wayland, the window events
Wails' GTK3 backend does not deliver, and the frosted backdrop behind the
console.

## The global shortcut on Wayland

On Wayland a client cannot grab keys. The only sanctioned mechanism is
the XDG Desktop Portal's `org.freedesktop.portal.GlobalShortcuts`
interface, and the portal decides which application a shortcut belongs
to by looking up the **app id** of the caller.

The app id comes from the caller's systemd unit: a scope unit named
`app-<app_id>-<something>.scope` yields the app id `<app_id>`. A build
started from a terminal inherits the terminal's scope — for example
`app-org.kde.konsole-*.scope` — so the shortcut would be registered for
the terminal instead of for WinResizer.

`build/linux/Taskfile.yml` fixes this by running the app under its own
scope:

```
systemd-run --user --scope --unit="app-winresizer-<run id>" --collect <binary>
```

The portal also resolves the app id to a desktop entry, so the app needs
a `.desktop` file whose basename matches the app id. Packages install it
system-wide; a dev checkout usually has none, so the `run` task installs
the generated one into `~/.local/share/applications`.

Two details worth keeping in mind:

- The user-local copy shadows a system-wide one in the XDG lookup order.
  The `install:desktop` task refreshes it on every run and removes it
  once a package is installed system-wide.
- `task run INSTALL_DESKTOP=false` skips the user-local copy for anyone
  who does not want the task writing to their home directory.

The scope is only used on Wayland sessions, and only when a user systemd
manager is reachable — `run` probes both before committing to it.

## The GTK3 build tag

The Linux build is pinned to `-tags gtk3`. Wails' GTK4 backend does not
implement window transparency (`setTransparent` is a no-op), so the
console's translucent background only works on the GTK3 + WebKit2GTK 4.1
stack. `wails3 dev` builds through the same task with `DEV=true`, so the
tag has to apply in both branches.

## Window events Wails does not deliver

The GTK3 backend connects only `focus-out-event` on the window, never
`focus-in-event`. So:

- `common:WindowFocus` never reaches the frontend on Linux.
- `common:WindowShow` is mapped from `linux:WindowLoadFinished`, so it
  fires when the page loads — not when the window is shown from the
  tray.

Anything that relies on "focus returned to us" as a refresh signal has
to be pushed from the backend instead. See
[the target tracking notes](../architecture/target-tracking.md).

## The frosted backdrop

Windows gets the console's frosted panel from DWM (`BackdropType: Acrylic`)
and macOS from `NSVisualEffectView` (`Mac.Backdrop: MacBackdropTranslucent`).
Both are compositor features the OS applies behind a translucent window, and
the frontend's translucent tint is painted on top of them
(`frontend/public/style.css`).

Linux has no such option in Wails: `BackgroundTypeTranslucent` only gives the
GTK3 window an RGBA visual, which makes the window *transparent* — not
blurred. The desktop shows through sharply.

Compositors do offer blur, though: the `ext-background-effect-v1` protocol
lets a client hand the compositor a region of its surface and ask for that
region to be blurred. It is implemented by KWin 6.7+, Hyprland 0.56+, niri,
Mutter 51+ and Cosmic 1.3+, so the blur is compositor-native and follows
the desktop live, exactly like Acrylic.

`blur_linux_gtk3.go` implements it:

- `prepareBackgroundBlur` binds `ext_background_effect_manager_v1` at startup,
  so the compositor's `capabilities` event has arrived by the time the
  console is first shown.
- `enableBackgroundBlur` attaches a background effect object to the console's
  `wl_surface` and sets the blur region. The window must be realized first,
  which is why it runs right after `prepClientDecorations`.
- The blur region is one oversized rectangle: the protocol clips it to the
  surface size, so a resize never needs a new region.
- The region is double-buffered surface state, published on the next surface
  commit — hence the extra `gtk_widget_queue_draw` when the capabilities
  arrive after the window was first drawn.

The protocol bindings are generated from
`protocol/ext-background-effect-v1.xml` by `wayland-scanner` and vendored, so
building does not require `wayland-scanner`; the regeneration commands are in
the comment above the includes.

The overlay (the window picker) deliberately stays transparent — it has to
show the windows it is picking — so only the console gets a blur.

On a compositor without the protocol nothing happens and the console keeps
its plain translucent look.

> The `wails-cross` image used for cross-compiling needs `libwayland-dev`;
> the native build picks it up from the GTK3 development package.
