//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static void configureMacWindowNative(void *ptr) {
	if (ptr == NULL) {
		return;
	}
	void (^apply)(void) = ^{
		NSWindow *win = (__bridge NSWindow *)ptr;
		[win setMovableByWindowBackground:NO];
		[win setOpaque:YES];
	};
	if ([NSThread isMainThread]) {
		apply();
	} else {
		dispatch_async(dispatch_get_main_queue(), apply);
	}
}
*/
import "C"
import "unsafe"

// configureMacWindow disables background drag interception so native window edge resizing stays smooth.
func configureMacWindow(ptr unsafe.Pointer) {
	if ptr != nil {
		C.configureMacWindowNative(ptr)
	}
}
