# Linux

The Linux build is not just "the same app on another OS". Two areas need
their own treatment: the global shortcut on Wayland, and the window
events Wails' GTK3 backend does not deliver.

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
