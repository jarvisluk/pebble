//go:build !darwin || !cgo

package desktop

import "context"

func mainDisplayMetrics(_ context.Context) (nativeW, nativeH int, bounds displayBounds, ok bool) {
	return 0, 0, displayBounds{}, false
}
