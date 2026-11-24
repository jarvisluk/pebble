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
			Name:            "屏幕录制",
			Description:     "用于采集本机桌面画面并在远程桌面中实时预览。",
			Status:          "unsupported",
			CanPrompt:       false,
			CanOpenSettings: false,
		},
		{
			ID:              "accessibility",
			Name:            "辅助功能",
			Description:     "用于远程鼠标点击、滚轮、键盘输入以及桌面窗口识别与控制。",
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
