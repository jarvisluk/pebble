//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices -framework CoreFoundation -framework IOKit
#import <CoreGraphics/CoreGraphics.h>
#import <ApplicationServices/ApplicationServices.h>
#import <CoreFoundation/CoreFoundation.h>
#import <IOKit/hidsystem/IOHIDLib.h>

static int bhScreenRecordingGranted(void) {
	return CGPreflightScreenCaptureAccess() ? 1 : 0;
}

static int bhRequestScreenRecording(void) {
	return CGRequestScreenCaptureAccess() ? 1 : 0;
}

static int bhAccessibilityGranted(void) {
	return AXIsProcessTrusted() ? 1 : 0;
}

static int bhRequestAccessibility(void) {
	const void *keys[] = { kAXTrustedCheckOptionPrompt };
	const void *vals[] = { kCFBooleanTrue };
	CFDictionaryRef opts = CFDictionaryCreate(
		kCFAllocatorDefault,
		keys,
		vals,
		1,
		&kCFTypeDictionaryKeyCallBacks,
		&kCFTypeDictionaryValueCallBacks
	);
	Boolean ok = AXIsProcessTrustedWithOptions(opts);
	if (opts != NULL) {
		CFRelease(opts);
	}
	return ok ? 1 : 0;
}

// Returns: 1 = granted, 0 = denied, -1 = not_determined/unknown
static int bhInputMonitoringStatus(void) {
	IOHIDAccessType t = IOHIDCheckAccess(kIOHIDRequestTypeListenEvent);
	if (t == kIOHIDAccessTypeGranted) return 1;
	if (t == kIOHIDAccessTypeDenied) return 0;
	return -1;
}

static int bhRequestInputMonitoring(void) {
	return IOHIDRequestAccess(kIOHIDRequestTypeListenEvent) ? 1 : 0;
}

// Returns: 0 = noErr (granted), -1743 = errAEEventNotPermitted (denied), -1744 = errAEEventWouldRequireUserConsent (not_determined), other = error
static int bhAutomationSystemEventsStatus(int askIfNeeded) {
	const char *bundleId = "com.apple.systemevents";
	AEAddressDesc target;
	OSStatus err = AECreateDesc(typeApplicationBundleID, bundleId, strlen(bundleId), &target);
	if (err != noErr) {
		return (int)err;
	}
	OSStatus st = AEDeterminePermissionToAutomateTarget(&target, typeWildCard, typeWildCard, askIfNeeded ? true : false);
	AEDisposeDesc(&target);
	return (int)st;
}
*/
import "C"

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	macPrivacyRoot          = "x-apple.systempreferences:com.apple.preference.security?Privacy"
	macPrivacyScreenCapture = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
	macPrivacyAccessibility = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
	macPrivacyListenEvent   = "x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent"
	macPrivacyAutomation    = "x-apple.systempreferences:com.apple.preference.security?Privacy_Automation"
	macPrivacyAllFiles      = "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"
)

func checkFullDiskAccessStatus() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "not_determined"
	}
	for _, rel := range []string{
		filepath.Join("Library", "Application Support", "com.apple.TCC", "TCC.db"),
		filepath.Join("Library", "Mail"),
	} {
		p := filepath.Join(home, rel)
		info, statErr := os.Stat(p)
		if statErr != nil {
			continue
		}
		if info.IsDir() {
			if _, rerr := os.ReadDir(p); rerr == nil {
				return "granted"
			}
			return "denied"
		}
		f, oerr := os.Open(p)
		if oerr == nil {
			_ = f.Close()
			return "granted"
		}
		return "denied"
	}
	return "not_determined"
}

func mapAutomationStatus(code int) string {
	switch code {
	case 0:
		return "granted"
	case -1743:
		return "denied"
	case -1744:
		return "not_determined"
	default:
		return "not_determined"
	}
}

func platformPermissions(_ context.Context) []PermissionItem {
	sr := "denied"
	if C.bhScreenRecordingGranted() == 1 {
		sr = "granted"
	}
	ax := "denied"
	if C.bhAccessibilityGranted() == 1 {
		ax = "granted"
	}
	im := "not_determined"
	switch C.bhInputMonitoringStatus() {
	case 1:
		im = "granted"
	case 0:
		im = "denied"
	}
	auto := mapAutomationStatus(int(C.bhAutomationSystemEventsStatus(0)))
	fda := checkFullDiskAccessStatus()

	return []PermissionItem{
		{
			ID:              "screen_recording",
			Name:            "Screen Recording",
			Description:     "Capture the host desktop screen for live remote desktop preview.",
			Status:          sr,
			CanPrompt:       true,
			CanOpenSettings: true,
		},
		{
			ID:              "accessibility",
			Name:            "Accessibility",
			Description:     "Inject remote mouse, scroll, and keyboard input and inspect desktop windows.",
			Status:          ax,
			CanPrompt:       true,
			CanOpenSettings: true,
		},
		{
			ID:              "input_monitoring",
			Name:            "Input Monitoring",
			Description:     "Inject remote keyboard events and pass through keyboard shortcuts.",
			Status:          im,
			CanPrompt:       true,
			CanOpenSettings: true,
		},
		{
			ID:              "automation",
			Name:            "Automation (System Events)",
			Description:     "Focus applications, list desktop windows, and run AppleScript automation.",
			Status:          auto,
			CanPrompt:       true,
			CanOpenSettings: true,
		},
		{
			ID:              "full_disk_access",
			Name:            "Full Disk Access",
			Description:     "Optional; access protected system directories or sandboxed app data.",
			Status:          fda,
			CanPrompt:       false,
			CanOpenSettings: true,
		},
	}
}

func openMacSystemSettings(uri string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/open", uri).Run()
}

func triggerAutomationSystemEventsPrompt(ctx context.Context) error {
	runCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "/usr/bin/osascript", "-e", `tell application "System Events" to get name of first application process`)
	return cmd.Run()
}

func triggerAutomationTimed(wait time.Duration) (timedOut bool, err error) {
	errCh := make(chan error, 1)
	go func() {
		errCh <- triggerAutomationSystemEventsPrompt(context.Background())
	}()
	select {
	case err := <-errCh:
		return false, err
	case <-time.After(wait):
		return true, nil
	}
}

func requestPlatformPermission(ctx context.Context, id string, openSettings bool) (*PermissionRequestResult, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	var uri string
	switch id {
	case "all":
		uri = macPrivacyRoot
	case "screen_recording":
		uri = macPrivacyScreenCapture
	case "accessibility":
		uri = macPrivacyAccessibility
	case "input_monitoring":
		uri = macPrivacyListenEvent
	case "automation":
		uri = macPrivacyAutomation
	case "full_disk_access":
		uri = macPrivacyAllFiles
	default:
		return nil, fmt.Errorf("unknown permission id %q", id)
	}

	prompted := false
	opened := false
	if !openSettings {
		switch id {
		case "all":
			// Trigger every ungranted promptable permission once without auto-opening System Settings.
			if C.bhScreenRecordingGranted() == 0 {
				_ = C.bhRequestScreenRecording()
				prompted = true
			}
			if C.bhAccessibilityGranted() == 0 {
				_ = C.bhRequestAccessibility()
				prompted = true
			}
			if C.bhInputMonitoringStatus() == -1 {
				_ = C.bhRequestInputMonitoring()
				prompted = true
			}
			if int(C.bhAutomationSystemEventsStatus(0)) == -1744 {
				_, _ = triggerAutomationTimed(600 * time.Millisecond)
				prompted = true
			}
		case "screen_recording":
			granted := C.bhRequestScreenRecording()
			prompted = true
			if granted == 0 {
				if err := openMacSystemSettings(uri); err != nil {
					return nil, err
				}
				opened = true
			}
		case "accessibility":
			C.bhRequestAccessibility()
			prompted = true
		case "input_monitoring":
			if C.bhInputMonitoringStatus() == 0 {
				if err := openMacSystemSettings(uri); err != nil {
					return nil, err
				}
				opened = true
			} else {
				granted := C.bhRequestInputMonitoring()
				prompted = true
				if granted == 0 && C.bhInputMonitoringStatus() == 0 {
					if err := openMacSystemSettings(uri); err != nil {
						return nil, err
					}
					opened = true
				}
			}
		case "automation":
			st := int(C.bhAutomationSystemEventsStatus(0))
			if st == -1743 {
				if err := openMacSystemSettings(uri); err != nil {
					return nil, err
				}
				opened = true
			} else {
				timedOut, err := triggerAutomationTimed(600 * time.Millisecond)
				if !timedOut && err != nil {
					if openErr := openMacSystemSettings(uri); openErr != nil {
						return nil, openErr
					}
					opened = true
				} else {
					prompted = true
				}
			}
		case "full_disk_access":
			if err := openMacSystemSettings(uri); err != nil {
				return nil, err
			}
			opened = true
		}
	} else {
		if err := openMacSystemSettings(uri); err != nil {
			return nil, err
		}
		opened = true
	}

	items := platformPermissions(ctx)
	var cur PermissionItem
	for _, it := range items {
		if strings.EqualFold(it.ID, id) {
			cur = it
			break
		}
	}
	return &PermissionRequestResult{
		OK:             true,
		Prompted:       prompted,
		OpenedSettings: opened,
		Item:           cur,
		Items:          items,
	}, nil
}
