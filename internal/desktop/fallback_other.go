//go:build !linux

package desktop

import (
	"context"
	"errors"
	"fmt"
	"runtime"
)

func hasAgentDesktop() bool {
	return false
}

func captureLinuxFallbackFrame(context.Context, []string, string, int, int) (*FrameResult, error) {
	return nil, fmt.Errorf("desktop capture fallback unsupported on %s without bot-host", runtime.GOOS)
}

func listLinuxFallbackWindows(context.Context, []string) ([]WindowInfo, error) {
	return nil, nil
}

func focusLinuxFallbackWindow(context.Context, []string, string, string) error {
	return nil
}

func injectLinuxFallbackInput(context.Context, []string, UnifiedInput) error {
	return errors.New("desktop input requires bot-host on this platform")
}
