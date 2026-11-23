//go:build darwin && cgo

package desktop

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
*/
import "C"

import "context"

func mainDisplayMetrics(_ context.Context) (nativeW, nativeH int, bounds displayBounds, ok bool) {
	did := C.CGMainDisplayID()
	r := C.CGDisplayBounds(did)
	pw := int(C.CGDisplayPixelsWide(did))
	ph := int(C.CGDisplayPixelsHigh(did))
	if pw <= 0 || ph <= 0 || r.size.width <= 0 || r.size.height <= 0 {
		return 0, 0, displayBounds{}, false
	}
	return pw, ph, displayBounds{
		X:      float64(r.origin.x),
		Y:      float64(r.origin.y),
		Width:  float64(r.size.width),
		Height: float64(r.size.height),
	}, true
}
