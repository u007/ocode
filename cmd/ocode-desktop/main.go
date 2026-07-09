// ocode-desktop is the cross-platform desktop shell for ocode. It wraps the
// existing ocode HTTP/SSE API server in a native Wails v3 webview window and
// provides tray, dock badge, and notification features.
//
// Build: go build -o bin/ocode-desktop ./cmd/ocode-desktop
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/bundled"
	"github.com/u007/ocode/internal/desktop"
	"github.com/u007/ocode/internal/skill"
	"github.com/u007/ocode/web"
)

//go:embed all:embedded-assets
var embeddedAssets embed.FS

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

	// Register the embedded skills/plugins so the server can serve them even
	// when no disk copy exists. The assets are copied into embedded-assets/ by
	// the build (make desktop); a bare `go build` embeds only the placeholder
	// and the fallback becomes a no-op.
	assetsSub, _ := fs.Sub(embeddedAssets, "embedded-assets")
	skill.SetBundledFS(assetsSub)
	bundled.SetEmbeddedSkills(embeddedAssets)
	bundled.SetEmbeddedPlugins(embeddedAssets)
	if err := bundled.EnsureExtracted(); err != nil {
		log.Printf("ocode-desktop: bundled asset extraction failed: %v", err)
	}
	if assetsSub != nil {
		agent.SetBundledModelConfigFS(assetsSub)
	}

	// Boot the ocode API server on a random loopback port with a fresh token.
	// On failure a native dialog is shown after the app is created below —
	// stderr alone is invisible from a Finder-launched .app.
	handle, bootErr := desktop.StartServer(web.FS(), workDir)
	if bootErr != nil {
		log.Printf("ocode-desktop: server boot failed: %v", bootErr)
	} else {
		log.Printf("ocode-desktop: server running at %s", handle.URL)
	}

	// Create the Wails application with the badge + notification services.
	// The notifier is only created when supported: on macOS, touching
	// UNUserNotificationCenter from a non-.app binary aborts the process.
	dockSvc := dock.New()
	services := []application.Service{application.NewService(dockSvc)}
	var notifier *notifications.NotificationService
	if notificationsSupported() {
		notifier = notifications.New()
		services = append(services, application.NewService(notifier))
	}
	app := application.New(application.Options{
		Name:        "ocode",
		Description: "AI coding agent",
		Services:    services,
	})

	if bootErr != nil {
		// Native dialog so a double-clicked .app surfaces the failure.
		app.Dialog.Error().
			SetTitle("ocode failed to start").
			SetMessage(bootErr.Error()).
			Show()
		os.Exit(1)
	}

	// Build the webview URL with the auth token (same ?token= param the TUI /rc
	// command and EventSource use).
	appURL := fmt.Sprintf("%s/?token=%s", handle.URL, handle.Token)

	// Determine desktop URL via env override (for dev hot-reload).
	desktopURL := appURL
	if devURL := os.Getenv("OCODE_DESKTOP_DEV_URL"); devURL != "" {
		log.Printf("ocode-desktop: using dev URL %s", devURL)
		desktopURL = devURL
	}

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
		// Web inspector for debugging the frontend. Also available from the
		// native menu bar (View → Open Developer Tools, ⌥⌘I) in non-production
		// builds; the tray entry makes it discoverable.
		application.NewMenuItem("Open DevTools").OnClick(func(ctx *application.Context) {
			window.OpenDevTools()
		}),
		application.NewMenuItemSeparator(),
		application.NewMenuItem("Quit").OnClick(func(ctx *application.Context) {
			app.Quit()
		}),
	))

	// Dock badge, notifications, and focus tracking driven by run state.
	wireNative(app.Context(), window, handle, notifier, dockSvc)

	// Run the application (blocks until the window closes).
	if err := app.Run(); err != nil {
		log.Printf("ocode-desktop: app run error: %v", err)
	}
}
