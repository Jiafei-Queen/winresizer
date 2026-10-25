# Target tracking

WinResizer acts on the window the user was in *before* it clicked our own
UI. Clicking the console steals focus, so "the currently focused window"
is always us by the time the user presses Record or Cycle. The target
tracker solves that by remembering the last focused window that belongs
to another process.

## The tracker

`targetTracker` (`window.go`) polls the window list once a second and
keeps the focused window whose PID is not our own. Our own windows are
skipped, so clicking the console or opening the picker does not move the
target.

The snapshot it keeps is the last focused external window. Two snapshots
are the same target when the window ID, app, title, monitor and size
match (`sameTarget`). The position is deliberately not compared: the
console shows the size, not the coordinates, so a window that only moved
must not cost a round-trip.

## Who may announce a change

The tracker has two entry points, and the difference matters:

| Path | Announces? | Why |
| --- | --- | --- |
| Background ticker (`poll`) | yes | Nothing else tells the console that the user moved on to another window. |
| Read path (`GetTarget` → `pollQuiet`) | no | The response already carries the fresh target. Announcing it would make the console read the very same target again — and again, for as long as the target keeps changing. |

`GetTarget` still polls synchronously: the ticker can miss a fast
A → B → console switch that lands inside one interval, and the read is
exactly the moment when the answer matters.

## The `target-changed` event

The tracker announces a change through `setOnChange`, which the service
wires to a `target-changed` event on the console window. The event
carries no payload: the console re-reads `GetTarget` instead, because
the `isRecorded` flag on the answer depends on the saved presets and can
only be computed in the backend.

The callback runs outside the tracker's lock, but it may run while the
service lock is held (`RecordWindow` pins the picked window that way).
Callbacks must therefore never acquire the service lock themselves.

## Why the console needs a push at all

On Linux, Wails' GTK3 backend connects only `focus-out-event` — never
`focus-in-event` — so `common:WindowFocus` never reaches the frontend.
`common:WindowShow` is not a rescue either: on Linux it is mapped from
`linux:WindowLoadFinished`, so it fires when the page loads, not when
the window is shown. Without the push, the target card would only ever
be correct on the first paint.

## The frontend side

Every push the backend sends shares one debounce (`queueSync`), because
a record changes the presets and the tracked target at the same time and
two timers would answer that with two round-trips. The `full` flag marks
the pushes that also need the config re-read:

| Event | Refresh |
| --- | --- |
| `common:WindowFocus`, `common:WindowShow` | config + presets + target |
| `presets-changed` | config + presets + target |
| `target-changed` | target only |
