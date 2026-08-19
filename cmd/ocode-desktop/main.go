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
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/bundled"
	"github.com/u007/ocode/internal/desktop"
	"github.com/u007/ocode/internal/skill"
	"github.com/u007/ocode/internal/version"
	"github.com/u007/ocode/web"
)

//go:embed all:embedded-assets
var embeddedAssets embed.FS

//go:embed appicon.png
var appIcon []byte

func main() {
	// The desktop shell hosts the web UI, so resume a requested session by
	// navigating to the same session route used by the web application.
	var sessionID string
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-session", "--session":
			if i+1 >= len(os.Args) || os.Args[i+1] == "" {
				log.Printf("ocode-desktop: %s requires a session ID", os.Args[i])
				os.Exit(2)
			}
			sessionID = os.Args[i+1]
			i++
		}
	}

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
		// Attach a scheduler.Service so the `cron` tool is available in
		// agent sessions created in the desktop-hosted server, and /api/cron
		// routes are live. This is the desktop counterpart of the
		// schedulerSetup() hook in cmd/ocode's serve/web paths.
		desktop.AttachScheduler(handle.Srv, workDir)
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
		Icon:        appIcon,
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
	if sessionID != "" {
		appURL = fmt.Sprintf("%s/session/%s?token=%s", handle.URL, url.PathEscape(sessionID), handle.Token)
	}

	// Determine desktop URL via env override (for dev hot-reload).
	desktopURL := appURL
	if devURL := os.Getenv("OCODE_DESKTOP_DEV_URL"); devURL != "" {
		log.Printf("ocode-desktop: using dev URL %s", devURL)
		desktopURL = devURL
		if sessionID != "" {
			parsed, err := url.Parse(devURL)
			if err == nil {
				parsed.Path = strings.TrimRight(parsed.Path, "/") + "/session/" + url.PathEscape(sessionID)
				desktopURL = parsed.String()
			}
		}
	}

	// Create the main webview window.
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     "ocode " + version.Version,
		Width:     1280,
		Height:    800,
		MinWidth:  800,
		MinHeight: 600,
		URL:       desktopURL,
		Linux: application.LinuxWindow{
			Icon: appIcon,
		},
	})

	// Set up the application menu. The Wails default binds CmdOrCtrl+W to
	// "Close Window" (which on this single-window shell quits the whole app)
	// and CmdOrCtrl+Q to an unconfirmed quit. ocode sessions are web tabs, so
	// Cmd/Ctrl+W is deliberately left unbound — the key reaches the webview,
	// which closes the active session tab — and Cmd/Ctrl+Q asks for
	// confirmation before quitting. The menu needs the window reference for
	// the confirmation dialog, so it is built after window creation.
	app.Menu.SetApplicationMenu(buildAppMenu(app, window))

	// Closing the window quits the app. Without this, the system tray below
	// keeps the process (and its in-process server) alive after the window
	// closes — surprising on macOS where closing the last window normally
	// terminates a non-tray app.
	window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		app.Quit()
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

// buildAppMenu builds the native application menu. It differs from the Wails
// default in two deliberate ways:
//
//   - CmdOrCtrl+W is NOT bound to any menu item. The default binds it to
//     "Close Window", which on this single-window shell quits the whole app
//     (WindowClosing → app.Quit). Desktop sessions are web tabs, so the key
//     is left to fall through to the webview, where the frontend closes the
//     active session tab instead. The frontend handles it in useKeyboard.
//
//   - CmdOrCtrl+Q (Quit) shows a native confirmation dialog first; the app
//     only quits when the user confirms, so a stray ⌘Q cannot kill the app
//     (and its in-process agent server) by accident.
//
// The standard Edit/View/Window menus are kept for text editing (Cmd+C/V),
// reload/devtools, and window management.
func buildAppMenu(app *application.App, window *application.WebviewWindow) *application.Menu {
	menu := application.NewMenu()
	quitHandler := rapidQuitHandler(app, window)

	// macOS application menu (first menu, named after the app).
	if runtime.GOOS == "darwin" {
		appMenu := menu.AddSubmenu("ocode")
		appMenu.AddRole(application.About)
		appMenu.AddSeparator()
		appMenu.Add("Settings…").
			SetAccelerator("CmdOrCtrl+,").
			OnClick(func(*application.Context) {
				// window.EmitEvent depends on Wails' own wails:// scheme handler
				// to inject window._wails into the page; this webview loads a
				// plain http://127.0.0.1:PORT URL served by ocode's own
				// embed.FS-backed HTTP server, which never goes through that
				// handler, so window._wails is never injected there — the event
				// is a structural no-op, not a timing issue. ExecJS instead runs
				// arbitrary JS directly in the loaded page regardless of how it
				// was loaded, so we dispatch a plain DOM CustomEvent that the
				// React app listens for with a normal addEventListener.
				window.ExecJS(`window.dispatchEvent(new CustomEvent("ocode:open-settings"))`)
			})
		appMenu.AddSeparator()
		appMenu.AddRole(application.ServicesMenu)
		appMenu.AddSeparator()
		appMenu.AddRole(application.Hide)
		appMenu.AddRole(application.HideOthers)
		appMenu.AddRole(application.UnHide)
		appMenu.AddSeparator()
		appMenu.Add("Quit ocode").
			SetAccelerator("CmdOrCtrl+q").
			OnClick(func(*application.Context) {
				quitHandler()
			})
	} else {
		// Windows/Linux: Settings + Quit live in the File menu.
		fileMenu := menu.AddSubmenu("File")
		fileMenu.Add("Settings…").
			SetAccelerator("CmdOrCtrl+,").
			OnClick(func(*application.Context) {
				// See the darwin branch comment: window.EmitEvent is a structural
				// no-op for a plain http:// webview, so use ExecJS directly.
				window.ExecJS(`window.dispatchEvent(new CustomEvent("ocode:open-settings"))`)
			})
		fileMenu.AddSeparator()
		fileMenu.Add("Quit ocode").
			SetAccelerator("CmdOrCtrl+q").
			OnClick(func(*application.Context) {
				quitHandler()
			})
	}

	// Standard Edit menu (Cmd+C/V/X/A/Z for webview text fields).
	menu.AddRole(application.EditMenu)

	// View menu (reload, devtools, zoom, fullscreen).
	menu.AddRole(application.ViewMenu)

	// Window menu. The macOS role is fine (Minimise/Zoom/Front, no Cmd+W).
	// On Windows/Linux the default role adds "Close Window" bound to Cmd+W,
	// which we must not bind — build it manually without that item.
	if runtime.GOOS == "darwin" {
		menu.AddRole(application.WindowMenu)
	} else {
		winMenu := menu.AddSubmenu("Window")
		winMenu.AddRole(application.Minimise)
		winMenu.AddRole(application.Zoom)
	}

	return menu
}

// rapidQuitThreshold is the window within which a second CmdOrCtrl+Q is
// treated as deliberate double-tap-to-quit, bypassing the confirmation
// dialog. A single press still confirms, so an accidental shortcut remains
// protected by the confirmation dialog.
const rapidQuitThreshold = 1500 * time.Millisecond

// rapidQuitHandler returns a quit handler that quits immediately if invoked
// twice within rapidQuitThreshold, and otherwise shows the confirmation
// dialog (see confirmQuit).
func rapidQuitHandler(app *application.App, window *application.WebviewWindow) func() {
	var mu sync.Mutex
	var lastPress time.Time
	return func() {
		mu.Lock()
		now := time.Now()
		rapid := !lastPress.IsZero() && now.Sub(lastPress) < rapidQuitThreshold
		lastPress = now
		mu.Unlock()

		if rapid {
			app.Quit()
			return
		}
		confirmQuit(app, window)
	}
}

// confirmQuit asks for explicit confirmation before quitting. Quit is
// cancelled by default (Enter/Escape dismisses safely); the app only exits
// when the user clicks the "Quit" button.
func confirmQuit(app *application.App, window *application.WebviewWindow) {
	dlg := app.Dialog.Question().
		SetTitle("Quit ocode?").
		SetMessage("Are you sure you want to quit ocode?").
		AttachToWindow(window)
	cancel := dlg.AddButton("Cancel")
	cancel.SetAsCancel()
	cancel.SetAsDefault()
	dlg.AddButton("Quit").OnClick(func() {
		app.Quit()
	})
	dlg.Show()
}
