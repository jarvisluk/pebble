//go:build !linux

package desktop

import "context"

type stubVirtualBackend struct{}

func newPlatformVirtualBackend() virtualBackend {
	return stubVirtualBackend{}
}

func (stubVirtualBackend) Probe() ProbeInfo {
	return ProbeInfo{VirtualSupported: false}
}

func (stubVirtualBackend) Start(context.Context, string) error { return nil }
func (stubVirtualBackend) Freeze() error                       { return nil }
func (stubVirtualBackend) Resume() error                       { return nil }
func (stubVirtualBackend) Stop() error                         { return nil }
func (stubVirtualBackend) Env() []string                       { return nil }
func (stubVirtualBackend) HasWindows(context.Context) (bool, error) {
	return false, nil
}
func (stubVirtualBackend) Install(context.Context) (*SetupResult, error) {
	return &SetupResult{OK: false, Error: "virtual desktop is not required on this platform"}, nil
}

func wakeLinuxNativeDisplay() {}
