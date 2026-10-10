// Pins an NSWindow in place by disabling user-driven movement.
//
// Wails v3 (beta.28) sets movableByWindowBackground = YES for every
// window in its WebviewWindow init (webview_window_darwin.m) and exposes
// no Go API or option to turn it off, so the picker overlay would be
// draggable by clicking its background — which also breaks outline
// alignment (rects are relative to the window's top-left). This shim
// finds our window by title and freezes it. Programmatic positioning
// (SetPosition) is unaffected by setMovable:NO.

#import <Cocoa/Cocoa.h>
#include <dispatch/dispatch.h>
#include <stdlib.h>
#include <string.h>

static void winresizerPinOnMain(void *ctx) {
    char *title = (char *)ctx;
    @autoreleasepool {
        NSString *nsTitle = [NSString stringWithUTF8String:title];
        for (NSWindow *win in [NSApp windows]) {
            if (win.title != nil && [win.title isEqualToString:nsTitle]) {
                [win setMovable:NO];
                [win setMovableByWindowBackground:NO];
                break;
            }
        }
    }
    free(title);
}

// winresizerPinWindow disables user dragging of the NSWindow whose title
// matches `title`. Safe to call from any thread: the work is deferred to
// the main queue, where AppKit requires it. The title is copied so the
// caller's buffer need not outlive the call.
void winresizerPinWindow(const char *title) {
    if (title == NULL) {
        return;
    }
    char *copy = strdup(title);
    if (copy == NULL) {
        return;
    }
    dispatch_async_f(dispatch_get_main_queue(), copy, winresizerPinOnMain);
}
