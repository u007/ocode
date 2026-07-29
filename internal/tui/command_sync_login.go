package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/sync"
)

// runSyncLoginCmd starts the device-code login flow and displays the auth
// code immediately in the transcript. Polling for approval runs in the
// background via tea.Tick messages driven by the model's Update loop.
func runSyncLoginCmd(m *model, args []string) tea.Cmd {
	return func() tea.Msg {
		client := sync.NewClient(sync.DefaultBaseURL())
		ctx := context.Background()

		// Start device flow (fast HTTP call — no blocking).
		result, err := client.StartDevice(ctx)
		if err != nil {
			return statusMsg{
				text: fmt.Sprintf("Sync login failed to start: %v", err),
			}
		}

		// Open the browser to the verification page.
		if err := openBrowser(result.VerifyURL); err != nil {
			// Non-fatal — the user can manually visit the URL.
		}

		return syncLoginStartedMsg{
			client:     client,
			deviceCode: result.DeviceCode,
			expiresAt:  time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
			userCode:   result.UserCode,
			verifyURL:  result.VerifyURL,
		}
	}
}

func runSyncLogoutCmd(m *model, args []string) tea.Cmd {
	return func() tea.Msg {
		hadSyncState := m.syncStop != nil || m.syncClient != nil || m.syncDeviceCode != ""
		// Stop the background watcher to unhook config/auth save hooks.
		if m.syncStop != nil {
			m.syncStop()
			m.syncStop = nil
		}
		m.clearSyncLoginState()

		token, ok, err := sync.LoadToken()
		if err != nil {
			if hadSyncState {
				return statusMsg{text: "Logged out of sync — sync stopped."}
			}
			return statusMsg{text: fmt.Sprintf("Sync logout failed: %v", err)}
		}
		if !ok {
			if hadSyncState {
				return statusMsg{text: "Logged out of sync — sync stopped."}
			}
			return statusMsg{text: "Not logged in to sync."}
		}
		client := sync.NewClient(sync.DefaultBaseURL())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Revoke(ctx, token); err != nil {
			// Non-fatal; server may be unreachable. Token is cleared locally either way.
		}
		if err := sync.ClearToken(); err != nil {
			return statusMsg{text: fmt.Sprintf("Sync logout failed: %v", err)}
		}
		return statusMsg{text: "Logged out of sync — sync stopped."}
	}
}
