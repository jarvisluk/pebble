//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -fobjc-arc -x objective-c
#cgo LDFLAGS: -framework ScreenCaptureKit -framework VideoToolbox -framework CoreMedia -framework CoreVideo -framework CoreGraphics -framework Foundation
#include <stdlib.h>
#include "sck_darwin.h"
*/
import "C"

import (
	"context"
	"errors"
	"runtime/cgo"
	"sync"
	"sync/atomic"
	"unsafe"
)

type sckSession struct {
	mu      sync.Mutex
	ptr     unsafe.Pointer
	closed  atomic.Bool
	onChunk func(H264Chunk) error
	errCh   chan error
}

//export goSCKOnFrame
func goSCKOnFrame(handle C.uintptr_t, data unsafe.Pointer, length C.int, isKey C.int) {
	if data == nil || length <= 0 {
		return
	}
	h := cgo.Handle(handle)
	sess, ok := h.Value().(*sckSession)
	if !ok || sess == nil || sess.closed.Load() {
		return
	}
	buf := C.GoBytes(data, length)
	if err := sess.onChunk(H264Chunk{
		Data: buf,
		Key:  isKey != 0,
	}); err != nil {
		select {
		case sess.errCh <- err:
		default:
		}
	}
}

func (s *sckSession) SetBitrate(kbps int) {
	if kbps <= 0 || s.closed.Load() {
		return
	}
	s.mu.Lock()
	ptr := s.ptr
	s.mu.Unlock()
	if ptr != nil {
		C.bh_sck_set_bitrate(ptr, C.int(kbps))
	}
}

func (s *sckSession) ForceKeyframe() {
	if s.closed.Load() {
		return
	}
	s.mu.Lock()
	ptr := s.ptr
	s.mu.Unlock()
	if ptr != nil {
		C.bh_sck_force_keyframe(ptr)
	}
}

func nativeCaptureAvailable() bool {
	return true
}

func startNativeVideoStream(
	ctx context.Context,
	w, h, fps, bitrateKbps int,
	codec string,
	onReady func(StreamControl),
	onChunk func(H264Chunk) error,
) error {
	sess := &sckSession{
		onChunk: onChunk,
		errCh:   make(chan error, 1),
	}
	handle := cgo.NewHandle(sess)
	defer handle.Delete()

	isHEVC := 0
	if codec == "hevc" {
		isHEVC = 1
	}

	var errPtr *C.char
	ptr := C.bh_sck_start(
		C.uintptr_t(handle),
		C.int(w),
		C.int(h),
		C.int(fps),
		C.int(bitrateKbps),
		C.int(isHEVC),
		&errPtr,
	)
	if ptr == nil {
		msg := "ScreenCaptureKit start failed"
		if errPtr != nil {
			msg = C.GoString(errPtr)
			C.free(unsafe.Pointer(errPtr))
		}
		return errors.New(msg)
	}

	sess.mu.Lock()
	sess.ptr = ptr
	sess.mu.Unlock()

	if onReady != nil {
		onReady(sess)
	}

	var retErr error
	select {
	case <-ctx.Done():
		retErr = ctx.Err()
	case err := <-sess.errCh:
		retErr = err
	}

	sess.closed.Store(true)
	sess.mu.Lock()
	p := sess.ptr
	sess.ptr = nil
	sess.mu.Unlock()
	if p != nil {
		C.bh_sck_stop(p)
	}
	return retErr
}
