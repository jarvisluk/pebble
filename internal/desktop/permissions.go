package desktop

import (
	"context"
	"runtime"
	"strings"
	"time"
)

// PermissionItem describes a single host OS permission used by remote desktop & computer-use automation.
type PermissionItem struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Status          string `json:"status"` // granted | denied | not_determined | unsupported
	CanPrompt       bool   `json:"canPrompt"`
	CanOpenSettings bool   `json:"canOpenSettings"`
}

// PermissionsResponse describes all desktop-related system permissions on a target host.
type PermissionsResponse struct {
	Target   string           `json:"target,omitempty"`
	Platform string           `json:"platform"`
	Items    []PermissionItem `json:"items"`
}

// PermissionRequestResult describes the outcome of requesting a permission or opening system settings.
type PermissionRequestResult struct {
	OK             bool             `json:"ok"`
	Prompted       bool             `json:"prompted,omitempty"`
	OpenedSettings bool             `json:"openedSettings,omitempty"`
	Item           PermissionItem   `json:"item"`
	Items          []PermissionItem `json:"items"`
}

func (e *Engine) resetPermissionCache() {
	if e == nil {
		return
	}
	e.permMu.Lock()
	e.srDenied = false
	e.srChecked = time.Time{}
	e.permMu.Unlock()
}

// Permissions inspects host OS permissions relevant to remote desktop and automation.
func (e *Engine) Permissions(ctx context.Context) (*PermissionsResponse, error) {
	if e != nil {
		e.resetPermissionCache()
	}
	items := platformPermissions(ctx)
	return &PermissionsResponse{
		Platform: runtime.GOOS,
		Items:    items,
	}, nil
}

// RequestPermission triggers a native system permission prompt and/or opens System Settings for the given permission ID.
func (e *Engine) RequestPermission(ctx context.Context, id string, openSettings bool) (*PermissionRequestResult, error) {
	if e != nil {
		e.resetPermissionCache()
	}
	id = strings.TrimSpace(id)
	res, err := requestPlatformPermission(ctx, id, openSettings)
	if err != nil {
		return nil, err
	}
	if e != nil {
		e.resetPermissionCache()
	}
	return res, nil
}
