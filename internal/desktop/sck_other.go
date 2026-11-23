//go:build !darwin || !cgo

package desktop

import (
	"context"
	"errors"
)

func nativeCaptureAvailable() bool {
	return false
}

func startNativeVideoStream(
	_ context.Context,
	_, _, _, _ int,
	_ string,
	_ func(StreamControl),
	_ func(H264Chunk) error,
) error {
	return errors.New("native ScreenCaptureKit unavailable on this platform")
}
