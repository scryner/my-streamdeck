package widgets

import (
	"context"
	"fmt"
	"os/exec"
)

type ActivityMonitorOpenFunc func(context.Context) error

const (
	activityMonitorBundleID   = "com.apple.ActivityMonitor"
	activityMonitorCPUTab     = 1
	activityMonitorNetworkTab = 5
)

func openActivityMonitorCPU(ctx context.Context) error {
	return openActivityMonitorTab(ctx, activityMonitorCPUTab)
}

func openActivityMonitorNetwork(ctx context.Context) error {
	return openActivityMonitorTab(ctx, activityMonitorNetworkTab)
}

func openActivityMonitorTab(ctx context.Context, tabIndex int) error {
	openCmd := exec.CommandContext(ctx, "/usr/bin/open", "-b", activityMonitorBundleID)
	if err := openCmd.Run(); err != nil {
		return fmt.Errorf("open activity monitor: %w", err)
	}

	// Activity Monitor does not expose a direct public API for tab selection.
	// If Accessibility is allowed, GUI scripting can switch to the requested tab.
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
`, activityMonitorBundleID, activityMonitorBundleID, tabIndex)
	guiCmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script)
	if err := guiCmd.Run(); err != nil {
		return nil
	}
	return nil
}
