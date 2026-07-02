// ocode-desktop is the cross-platform desktop shell for ocode. It wraps the
// existing ocode HTTP/SSE API server in a native Wails v3 webview window and
// provides tray, dock badge, and notification features.
//
// Build: go build -o bin/ocode-desktop ./cmd/ocode-desktop
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/u007/ocode/internal/desktop"
	"github.com/u007/ocode/web"
)

func main() {
	// Resolve the working directory the server anchors relative paths to.
	// A Finder/Dock-launched .app starts with cwd "/" — fall back to the
	// user's home directory so session/upload paths never target the root.
	workDir, err := os.Getwd()
	if err != nil || workDir == "/" {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			workDir = home
		} else {
			log.Printf("ocode-desktop: resolve home dir (cwd err %v): %v", err, homeErr)
			workDir = "."
		}
	}

	// Boot the ocode API server on a random loopback port with a fresh token.
	handle, err := desktop.StartServer(web.FS(), workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ocode-desktop: server boot failed: %v\n", err)
		os.Exit(1)
	}

	log.Printf("ocode-desktop: server running at %s", handle.URL)

	// Build the webview URL with the auth token (same ?token= param the TUI /rc
	// command and EventSource use).
	appURL := fmt.Sprintf("%s/?token=%s", handle.URL, handle.Token)

	// Determine desktop URL via env override (for dev hot-reload).
	desktopURL := appURL
	if devURL := os.Getenv("OCODE_DESKTOP_DEV_URL"); devURL != "" {
		log.Printf("ocode-desktop: using dev URL %s", devURL)
		desktopURL = devURL
	}

	// Create the Wails application.
	app := application.New(application.Options{
		Name:        "ocode",
		Description: "AI coding agent",
	})

	// Set up the application menu (native Edit menu for Cmd+C/V etc.).
	app.Menu.SetApplicationMenu(application.DefaultApplicationMenu())

	// Create the main webview window.
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:    "main",
		Title:   "ocode",
		Width:   1280,
		Height:  800,
		MinWidth:  800,
		MinHeight: 600,
		URL:    desktopURL,
	})

	// System tray for show/hide and quit.
	tray := app.SystemTray.New()
	tray.SetLabel("ocode")
	tray.SetMenu(application.NewMenuFromItems(
		application.NewMenuItem("Show ocode").OnClick(func(ctx *application.Context) {
			window.Show()
			window.Focus()
		}),
		application.NewMenuItemSeparator(),
		application.NewMenuItem("Quit").OnClick(func(ctx *application.Context) {
			app.Quit()
		}),
	))

	// Run-state watcher for dock badge count and notifications.
	// TODO(desktop Task 6): wire Summary.RunningCount to the dock badge and
	// Finished to native notifications; log-only until then (tracked in TODO.md).
	go desktop.Watch(app.Context(), 750*time.Millisecond, handle.Srv.RunStates, func(summary desktop.Summary) {
		for _, r := range summary.Finished {
			if r.Failed {
				log.Printf("Run %q failed", r.Name)
			} else {
				log.Printf("Run %q completed", r.Name)
			}
		}
	})

	// Run the application (blocks until the window closes).
	if err := app.Run(); err != nil {
		log.Printf("ocode-desktop: app run error: %v", err)
	}
}
