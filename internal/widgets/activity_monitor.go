package widgets

import (
	"context"
	"fmt"
	"os/exec"
)

type ActivityMonitorOpenFunc func(context.Context) error

type activityMonitorTab struct {
	defaultsIndex int
	uiIndex       int
}

const (
	activityMonitorBundleID = "com.apple.ActivityMonitor"
)

var (
	activityMonitorCPUTab = activityMonitorTab{
		defaultsIndex: 0,
		uiIndex:       1,
	}
	activityMonitorNetworkTab = activityMonitorTab{
		defaultsIndex: 4,
		uiIndex:       5,
	}
)

func openActivityMonitorCPU(ctx context.Context) error {
	return openActivityMonitorTab(ctx, activityMonitorCPUTab)
}

func openActivityMonitorNetwork(ctx context.Context) error {
	return openActivityMonitorTab(ctx, activityMonitorNetworkTab)
}

func openActivityMonitorTab(ctx context.Context, tab activityMonitorTab) error {
	defaultsCmd := exec.CommandContext(
		ctx,
		"/usr/bin/defaults",
		"write",
		activityMonitorBundleID,
		"SelectedTab",
		"-int",
		fmt.Sprintf("%d", tab.defaultsIndex),
	)
	if err := defaultsCmd.Run(); err != nil {
		return fmt.Errorf("set activity monitor tab: %w", err)
	}

	openCmd := exec.CommandContext(ctx, "/usr/bin/open", "-b", activityMonitorBundleID)
	if err := openCmd.Run(); err != nil {
		return fmt.Errorf("open activity monitor: %w", err)
	}

	// If Activity Monitor is already running, best-effort GUI scripting can still
	// switch the visible tab after activation. This requires Accessibility access.
	script := fmt.Sprintf(`
tell application id "%s" to activate
delay 0.3
tell application "System Events"
	tell first application process whose bundle identifier is "%s"
		tell toolbar 1 of window 1
			if exists radio group 1 then
				click radio button %d of radio group 1
			end if
		end tell
	end tell
end tell
`, activityMonitorBundleID, activityMonitorBundleID, tab.uiIndex)
	guiCmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script)
	if err := guiCmd.Run(); err != nil {
		return nil
	}
	return nil
}
