//go:build !darwin || !cgo

package desktop

import (
	"context"
	"fmt"
	"strings"
)

func platformPermissions(_ context.Context) []PermissionItem {
	return []PermissionItem{
		{
			ID:              "screen_recording",
			Name:            "Screen Recording",
			Description:     "Capture the host desktop screen for live remote desktop preview.",
			Status:          "unsupported",
			CanPrompt:       false,
			CanOpenSettings: false,
		},
		{
			ID:              "accessibility",
			Name:            "Accessibility",
			Description:     "Inject remote mouse, scroll, and keyboard input and inspect desktop windows.",
			Status:          "unsupported",
			CanPrompt:       false,
			CanOpenSettings: false,
		},
	}
}

func requestPlatformPermission(ctx context.Context, id string, _ bool) (*PermissionRequestResult, error) {
	items := platformPermissions(ctx)
	if strings.EqualFold(strings.TrimSpace(id), "all") {
		return &PermissionRequestResult{
			OK:    false,
			Items: items,
		}, nil
	}
	for _, it := range items {
		if strings.EqualFold(it.ID, id) {
			return &PermissionRequestResult{
				OK:    false,
				Item:  it,
				Items: items,
			}, nil
		}
	}
	return nil, fmt.Errorf("unknown permission id %q", id)
}
