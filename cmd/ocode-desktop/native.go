package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/u007/ocode/internal/desktop"
)

// notificationsSupported reports whether native notifications can be used at
// all. On macOS every UNUserNotificationCenter call (including authorization)
// throws NSInternalInconsistencyException and aborts the process when the
// executable is not inside a .app bundle — so a bare `bin/ocode-desktop`
// binary must never touch the notifier. Other platforms have no such
// restriction.
func notificationsSupported() bool {
	if runtime.GOOS != "darwin" {
		return true
	}
	exe, err := os.Executable()
	if err != nil {
		log.Printf("ocode-desktop: resolve executable for bundle check: %v", err)
		return false
	}
	return strings.Contains(exe, ".app/Contents/MacOS/")
}

// wireNative connects OS integration (dock badge, notifications, focus
// tracking) to the in-process server's run state. All state flows one way:
// Server.RunStates() → desktop.Watch poll-and-diff → badge/notification calls.
// notifier is nil when notifications are unsupported (unbundled macOS binary);
// badge and tray still work.
func wireNative(ctx context.Context, win *application.WebviewWindow, handle *desktop.Handle, notifier *notifications.NotificationService, dockSvc *dock.DockService) {
	// Notifications fire only when the window is unfocused.
	var focused atomic.Bool
	focused.Store(true) // window opens focused
	win.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) { focused.Store(true) })
	win.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) { focused.Store(false) })

	if notifier != nil {
		// Clicking a notification focuses the window.
		notifier.OnNotificationResponse(func(notifications.NotificationResult) {
			win.Show()
			win.Focus()
		})

		// macOS requires explicit authorization; an unsigned bundle may be
		// refused — badge and tray still work, so log and continue.
		if runtime.GOOS == "darwin" {
			if ok, err := notifier.RequestNotificationAuthorization(); err != nil || !ok {
				log.Printf("ocode-desktop: notification authorization (ok=%v): %v", ok, err)
			}
		}
	} else {
		log.Printf("ocode-desktop: native notifications disabled (not running from a .app bundle)")
	}

	go desktop.Watch(ctx, 750*time.Millisecond, handle.Srv.RunStates, func(sum desktop.Summary) {
		// Dock badge: running-agent count. macOS/Windows only; errors on
		// unsupported platforms are logged, never fatal.
		if sum.RunningCount > 0 {
			if err := dockSvc.SetBadge(fmt.Sprintf("%d", sum.RunningCount)); err != nil {
				log.Printf("ocode-desktop: set badge %d: %v", sum.RunningCount, err)
			}
		} else {
			if err := dockSvc.RemoveBadge(); err != nil {
				log.Printf("ocode-desktop: remove badge: %v", err)
			}
		}

		if notifier == nil || focused.Load() {
			return // unsupported, or user is looking at the window
		}
		for _, run := range sum.Finished {
			title := "Agent finished"
			if run.Failed {
				title = "Agent failed"
			}
			err := notifier.SendNotification(notifications.NotificationOptions{
				ID:    run.SessionID + "/" + run.ID,
				Title: title,
				Body:  run.Name,
			})
			if err != nil {
				log.Printf("ocode-desktop: send notification %q: %v", run.Name, err)
			}
		}
	})
}
