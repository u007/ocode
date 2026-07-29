package tui

import (
	"context"
	"fmt"
	"log"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/sync"
)

func runSyncLoginCmd(m *model, args []string) tea.Cmd {
	return func() tea.Msg {
		client := sync.NewClient(sync.DefaultBaseURL())
		err := client.Login(context.Background(), openBrowser, func(format string, a ...interface{}) {
			log.Printf(format, a...)
		})
		if err != nil {
			return statusMsg{
				text: fmt.Sprintf("Sync login failed: %v", err),
			}
		}
		// Pull both blobs on first login to bootstrap local state.
		if err := client.Pull(context.Background(), sync.BlobTypeConfig); err != nil {
			// Non-fatal; will retry on next cycle.
		}
		if err := client.Pull(context.Background(), sync.BlobTypeAuth); err != nil {
			// Non-fatal; will retry on next cycle.
		}
		// Stop any previous watcher before starting a new one.
		if m.syncStop != nil {
			m.syncStop()
		}
		m.syncStop = sync.StartWatcher(client)
		return statusMsg{text: "Logged in to sync — encrypted config sync active."}
	}
}

func runSyncLogoutCmd(m *model, args []string) tea.Cmd {
	return func() tea.Msg {
		token, ok, err := sync.LoadToken()
		if err != nil || !ok {
			return statusMsg{text: "Not logged in to sync."}
		}
		// Stop the background watcher to unhook config/auth save hooks.
		if m.syncStop != nil {
			m.syncStop()
			m.syncStop = nil
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
