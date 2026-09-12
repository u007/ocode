// ocode-desktop is the cross-platform desktop shell for ocode. It wraps the
// existing ocode HTTP/SSE API server in a native Wails v3 webview window and
// provides tray, dock badge, and notification features.
//
// Build: go build -o bin/ocode-desktop ./cmd/ocode-desktop
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/bundled"
	"github.com/u007/ocode/internal/desktop"
	"github.com/u007/ocode/internal/lsp"
	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/skill"
	"github.com/u007/ocode/web"
	// Register provider plugins in the desktop binary as well as the CLI. Without
	// these side-effect imports, OAuth-backed OpenAI requests fall through to the
	// API-key endpoint and ChatGPT tokens fail with missing_scope.
	_ "github.com/u007/ocode/internal/plugin/codex"
	_ "github.com/u007/ocode/internal/plugin/grok"
)

//go:embed all:embedded-assets
var embeddedAssets embed.FS

//go:embed appicon.png
var appIcon []byte

// showQuittingIndicator updates the native window title and injects a visible
// DOM overlay so the user sees an immediate, non-blocking indicator that the
// app is quitting — before the synchronous OnShutdown drains agents.
func showQuittingIndicator(win *application.WebviewWindow) {
	if win == nil {
		return
	}
	win.SetTitle("ocode — Quitting…")
	// The overlay runs async (InvokeAsync inside ExecJS) so it paints even
	// when the main thread is about to block in OnShutdown.
	win.ExecJS(`(function(){if(window.__ocodeQuittingOverlay)return;var d=document.createElement('div');d.id='ocode-quitting';d.style.cssText='position:fixed;top:0;left:0;right:0;bottom:0;background:#0a0a0aff;color:#e8e8e8;font-family:system-ui,sans-serif;z-index:2147483646;display:flex;align-items:center;justify-content:center;flex-direction:column;font-size:20px;letter-spacing:0.5px;pointer-events:none;';d.innerHTML='<div style="font-size:28px;margin-bottom:12px;">ocode</div><div>Quitting — finishing active tasks</div><div style="margin-top:18px;font-size:13px;color:#999;">Please wait a moment</div>';document.body.appendChild(d);window.__ocodeQuittingOverlay=true;})();`)
}

func main() {
	// Hidden subcommand: internal/lsp/manager.go's spawnDaemonProcess re-execs
	// os.Executable() with "lsp-daemon" to start a detached broker. In this
	// binary os.Executable() resolves to ocode-desktop itself, so without this
	// dispatch the child falls straight through to the Wails bootstrap below
	// and opens a second full desktop window instead of running the daemon.
	// Mirrors the "lsp-daemon" case in the root cmd/ocode main().
	if len(os.Args) > 1 && os.Args[1] == "lsp-daemon" {
		if err := lsp.RunDaemon(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	// The desktop shell hosts the web UI, so resume a requested session by
	// navigating to the same session route used by the web application.
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-session", "--session":
			if i+1 >= len(os.Args) || os.Args[i+1] == "" {
				log.Printf("ocode-desktop: %s requires a session ID", os.Args[i])
				os.Exit(2)
			}
			i++
		}
	}
	sessionID := sessionIDFromArgs(os.Args)

	// Only one desktop instance may run: it owns the in-process API server,
	// the terminal ptys, and the per-window profile state, so a second copy
	// would silently fork all of that. application.New acquires the instance
	// lock and, for a second launch, forwards its argv to the first instance
	// and exits — which is why the app is created *before* the server boots
	// below, so the loser never starts a second server. The first instance
	// reacts by raising its window (and jumping to a requested --session).
	// The callback runs on Wails' listener goroutine and the window does not
	// exist yet, so it reads the window through an atomic set after creation;
	// nil means a second launch raced our own startup and is simply dropped.
	var mainWin atomic.Pointer[desktopWindow]
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
		ShouldQuit: func() bool {
			// Show the quitting indicator synchronously (before any block in OnShutdown)
			// and return true so Wails proceeds with Quit(). The indicator uses the
			// atomic pointer set after window creation; it may be nil on very early
			// signals before the window exists, so it is a no-op in that case.
			dw := mainWin.Load()
			if dw != nil && dw.window != nil {
				showQuittingIndicator(dw.window)
			}
			return true
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.ocode.desktop",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				log.Printf("ocode-desktop: second instance launch blocked (args %q), focusing existing window", data.Args)
				dw := mainWin.Load()
				if dw == nil {
					return
				}
				if id := sessionIDFromArgs(data.Args); id != "" {
					dw.window.SetURL(dw.sessionURL(id))
				}
				dw.window.UnMinimise()
				dw.window.Show()
				dw.window.Focus()
			},
		},
	})

	// Resolve the working directory the server anchors relative paths to.
	// A Finder/Dock-launched .app starts with cwd "/"; the old fallback used
	// $HOME which is a huge tree that triggers macOS TCC prompts (Documents,
	// Desktop, Downloads) when the file tree or LSP walker scans it. Instead
	// reuse the most-recent saved project, or a small safe dir
	// (~/.local/share/ocode) on fresh installs.
	workDir, err := os.Getwd()
	if err != nil || desktop.IsUnsafeDesktopRoot(workDir) {
		fb := desktop.ResolveFallbackWorkDir()
		if err != nil {
			log.Printf("ocode-desktop: cwd error %v, using fallback workDir %q", err, fb)
		} else {
			log.Printf("ocode-desktop: unsafe cwd %q, using fallback workDir %q", workDir, fb)
		}
		workDir = fb
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

	// Boot the ocode API server. If a remote SSH workspace is saved
	// on disk, connect to it; otherwise start locally.
	var (
		handle  *desktop.Handle
		bootErr error
	)
	if cfg, err := desktop.LoadWorkspaceConfig(); err == nil && cfg.Mode == desktop.WorkspaceRemoteSSH && cfg.TargetHost != "" {
		log.Printf("ocode-desktop: booting remote workspace %s:%d %s", cfg.TargetHost, cfg.TargetPort, cfg.RemotePath)
		target := remote.Target{Kind: remote.KindSSH, Host: cfg.TargetHost, User: ""}
		ws, err := desktop.OpenRemoteWorkspace(target, cfg.RemotePath)
		if err != nil {
			bootErr = fmt.Errorf("open remote workspace: %w", err)
		} else {
			handle, bootErr = desktop.StartServer(web.FS(), workDir, ws.Remote)
			if bootErr == nil {
				// Persist the target for reconnect on next launch.
				_ = desktop.SaveWorkspaceConfig(desktop.WorkspaceConfig{
					Mode:       desktop.WorkspaceRemoteSSH,
					TargetHost: cfg.TargetHost,
					RemotePath: cfg.RemotePath,
				})
			}
		}
	} else {
		handle, bootErr = desktop.StartServer(web.FS(), workDir, nil)
	}
	if bootErr != nil {
		log.Printf("ocode-desktop: server boot failed: %v", bootErr)
	} else {
		log.Printf("ocode-desktop: server running at %s", handle.URL)
		// Attach a scheduler.Service so the `cron` tool is available in
		// agent sessions created in the desktop-hosted server, and /api/cron
		// routes are live. This is the desktop counterpart of the
		// schedulerSetup() hook in cmd/ocode's serve/web paths.
		// Skipped in remote mode (handle.Srv == nil) — scheduler runs on the remote server.
		if handle.Srv != nil {
			desktop.AttachScheduler(handle.Srv, workDir)
		}
	}

	// The Wails app (badge + notification services) was created above, ahead
	// of the server boot, so the single-instance check runs first. The
	// notifier is only created when supported: on macOS, touching
	// UNUserNotificationCenter from a non-.app binary aborts the process.
	if bootErr != nil {
		// Native dialog so a double-clicked .app surfaces the failure. Dialog
		// dispatch requires the Wails main-thread run loop, which only starts
		// inside app.Run() below — calling Show() any earlier dereferences a
		// nil dispatcher and crashes the whole process instead of reporting
		// the error. events.Common.ApplicationStarted fires once that loop is
		// live, so the dialog is deferred to there and the app quits after.
		app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			app.Dialog.Error().
				SetTitle("ocode failed to start").
				SetMessage(bootErr.Error()).
				Show()
			app.Quit()
		})
		if err := app.Run(); err != nil {
			log.Printf("ocode-desktop: app run error: %v", err)
		}
		os.Exit(1)
	}

	// Stable per-window id threaded into the webview so the frontend binds
	// per-window profile state (active profile, chat config) to this desktop
	// window and survives webview reloads (the random per-tab id the web
	// falls back to would otherwise reset on every reload).
	const desktopWindowName = "main"

	// Build the webview URL with the auth token (same ?token= param the TUI /rc
	// command and EventSource use) and the stable windowId.
	appURL := fmt.Sprintf("%s/?token=%s&windowId=%s", handle.URL, handle.Token, desktopWindowName)
	sessionURL := func(id string) string {
		return fmt.Sprintf("%s/session/%s?token=%s&windowId=%s", handle.URL, url.PathEscape(id), handle.Token, desktopWindowName)
	}
	if sessionID != "" {
		appURL = sessionURL(sessionID)
	}

	// Determine desktop URL via env override (for dev hot-reload).
	desktopURL := appURL
	if devURL := os.Getenv("OCODE_DESKTOP_DEV_URL"); devURL != "" {
		log.Printf("ocode-desktop: using dev URL %s", devURL)
		parsed, err := url.Parse(devURL)
		if err == nil {
			q := parsed.Query()
			q.Set("windowId", desktopWindowName)
			// The desktop server boots with a random auth token that gates every
			// /api/* route. The webview SPA reads it from the ?token= query param
			// at load time and replays it as a Bearer header on every fetch. The
			// production URL (appURL) already carries it; the dev override must
			// too, or every API call (incl. /api/chat) returns 401 Unauthorized.
			if handle != nil {
				q.Set("token", handle.Token)
			}
			parsed.RawQuery = q.Encode()
			desktopURL = parsed.String()
		} else {
			desktopURL = devURL
		}
		if sessionID != "" {
			parsed, err := url.Parse(desktopURL)
			if err == nil {
				parsed.Path = strings.TrimRight(parsed.Path, "/") + "/session/" + url.PathEscape(sessionID)
				desktopURL = parsed.String()
			}
		}
	}

	// Create the main webview window.
	// Derive the window title from the resolved workDir: prefer the saved
	// project name, fall back to directory basename, last resort "ocode".
	windowTitle := desktop.ResolveWindowTitle(workDir)
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      desktopWindowName,
		Title:     windowTitle,
		Width:     1280,
		Height:    800,
		MinWidth:  800,
		MinHeight: 600,
		URL:       desktopURL,
		Linux: application.LinuxWindow{
			Icon: appIcon,
		},
	})

	mainWin.Store(&desktopWindow{window: window, sessionURL: sessionURL})

	// Set up the application menu. The Wails default binds CmdOrCtrl+W to
	// "Close Window" (which on this single-window shell quits the whole app)
	// and CmdOrCtrl+Q to an unconfirmed quit. ocode sessions are web tabs, so
	// Cmd/Ctrl+W is deliberately left unbound — the key reaches the webview,
	// which closes the active session tab — and Cmd/Ctrl+Q asks for
	// confirmation before quitting. The menu needs the window reference for
	// the confirmation dialog, so it is built after window creation.
	app.Menu.SetApplicationMenu(buildAppMenu(app, window, handle))

	// Closing the window quits the app. Without this, the system tray below
	// keeps the process (and its in-process server) alive after the window
	// closes — surprising on macOS where closing the last window normally
	// terminates a non-tray app.
	window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		showQuittingIndicator(window)
		app.Quit()
	})

	// System tray for show/hide and quit.
	tray := app.SystemTray.New()
	tray.SetLabel("ocode")
	tray.SetIcon(appIcon)
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
		// The desktop shell runs on WKWebView (macOS)/WebKitGTK (Linux), neither
		// of which exposes Chrome DevTools Protocol — CDP-based tools (Playwright,
		// claude-in-chrome, htrcli) cannot attach to this window directly. It
		// does load a plain HTTP page served by ocode's own server though, so
		// pointing a real Chromium-based browser at the same URL (token included,
		// since every /api/* route requires it) gets full CDP support against an
		// otherwise-identical UI.
		application.NewMenuItem("Copy Debug URL").OnClick(func(ctx *application.Context) {
			app.Clipboard.SetText(desktopURL)
		}),
		application.NewMenuItemSeparator(),
		application.NewMenuItem("Share Session…").OnClick(func(ctx *application.Context) {
			window.ExecJS(`window.dispatchEvent(new CustomEvent("ocode:share-session"))`)
		}),
		application.NewMenuItem("Share Entire Desktop…").OnClick(func(ctx *application.Context) {
			window.ExecJS(`window.dispatchEvent(new CustomEvent("ocode:share-desktop"))`)
		}),
		application.NewMenuItemSeparator(),
		application.NewMenuItem("Quit").OnClick(func(ctx *application.Context) {
			confirmQuit(app, window, handle)
		}),
	))

	// Dock badge, notifications, and focus tracking driven by run state.
	wireNative(app.Context(), window, handle, notifier, dockSvc)

	// Graceful shutdown on quit: when the app quits (Cmd+Q, tray Quit, window
	// close), drain agent sessions and terminate any running terminal ptys
	// within a TTL. Wails runs OnShutdown synchronously on the main thread
	// before the process exits, so this blocks the quit only up to the timeout
	// and guarantees shells aren't slaughtered mid-command.
	app.OnShutdown(func() {
		if handle == nil || handle.Srv == nil {
			return
		}
		timeout := desktopShutdownTimeout()
		window.SetTitle("ocode — Waiting for processes to gracefully shut down…")
		log.Printf("ocode-desktop: graceful shutdown (timeout %s)", timeout)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		handle.Srv.Shutdown(ctx)
	})

	// Run the application (blocks until the window closes).
	if err := app.Run(); err != nil {
		log.Printf("ocode-desktop: app run error: %v", err)
	}
}

// desktopWindow is what the single-instance callback needs from the first
// instance once its window exists: the window to raise and how to build a
// session URL for a forwarded --session argument.
type desktopWindow struct {
	window     *application.WebviewWindow
	sessionURL func(id string) string
}

// sessionIDFromArgs extracts the -session/--session value from an argv (the
// last one wins). Shared by the first launch and the second-instance forward,
// so both resolve the requested session identically. Missing/empty values
// yield ""; the first launch separately rejects those with a usage error.
func sessionIDFromArgs(args []string) string {
	var id string
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-session", "--session":
			if i+1 < len(args) && args[i+1] != "" {
				id = args[i+1]
			}
			i++
		}
	}
	return id
}

// desktopShutdownTimeout returns the maximum time the desktop app waits for a
// graceful shutdown before the process exits. It is the TTL that bounds the
// whole teardown: agent sessions drain and running terminals are SIGTERM'd then
// SIGKILL'd within this window. Override with OCODE_DESKTOP_SHUTDOWN_TIMEOUT
// (seconds). The default of 5s is long enough for terminals to flush and short
// enough that a quit never feels frozen; the sane range is 3–10s.
func desktopShutdownTimeout() time.Duration {
	const defaultTimeout = 5 * time.Second
	if v := os.Getenv("OCODE_DESKTOP_SHUTDOWN_TIMEOUT"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			d := time.Duration(secs) * time.Second
			if d > 60*time.Second {
				d = 60 * time.Second
			}
			return d
		}
	}
	return defaultTimeout
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
func buildAppMenu(app *application.App, window *application.WebviewWindow, handle *desktop.Handle) *application.Menu {
	menu := application.NewMenu()
	quitHandler := rapidQuitHandler(app, window, handle)

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

	// Share menu — native entry points for remote-control / desktop sharing.
	// Mirrors TUI's /rc (share session) and surfaces the already-running
	// desktop server URL (share entire desktop). The handlers dispatch DOM
	// CustomEvents to the React frontend (same mechanism as Settings…) so the
	// SPA can show the share dialog / copy the token URL without duplicating
	// dialog logic in Go.
	shareMenu := menu.AddSubmenu("Share")
	shareMenu.Add("Share Session…").
		SetAccelerator("CmdOrCtrl+Shift+S").
		OnClick(func(*application.Context) {
			window.ExecJS(`window.dispatchEvent(new CustomEvent("ocode:share-session"))`)
		})
	shareMenu.Add("Share Entire Desktop…").
		OnClick(func(*application.Context) {
			window.ExecJS(`window.dispatchEvent(new CustomEvent("ocode:share-desktop"))`)
		})
	shareMenu.AddSeparator()
	shareMenu.Add("Copy Desktop URL").
		OnClick(func(*application.Context) {
			window.ExecJS(`window.dispatchEvent(new CustomEvent("ocode:copy-desktop-url"))`)
		})

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
func rapidQuitHandler(app *application.App, window *application.WebviewWindow, handle *desktop.Handle) func() {
	var mu sync.Mutex
	var lastPress time.Time
	return func() {
		mu.Lock()
		now := time.Now()
		rapid := !lastPress.IsZero() && now.Sub(lastPress) < rapidQuitThreshold
		lastPress = now
		mu.Unlock()

		if rapid {
			showQuittingIndicator(window)
			app.Quit()
			return
		}
		confirmQuit(app, window, handle)
	}
}

// confirmQuit asks for explicit confirmation before quitting. Quit is
// cancelled by default (Enter/Escape dismisses safely); the app only exits
// when the user clicks the "Quit" button.
func confirmQuit(app *application.App, window *application.WebviewWindow, handle *desktop.Handle) {
	message := "Are you sure you want to quit ocode?"
	title := "Quit ocode?"
	buttonLabel := "Quit"

	// Build graceful-shutdown status from the live server state.
	if handle != nil && handle.Srv != nil {
		runs := handle.Srv.RunStates()
		pendingAsks := handle.Srv.PendingPermissionAsks()
		activeRuns := 0
		runNames := []string{}
		for _, r := range runs {
			if !r.Ended && !r.Failed {
				activeRuns++
				runNames = append(runNames, r.Name)
			}
		}
		if activeRuns > 0 || pendingAsks > 0 {
			title = "Graceful shutdown — still working"
			buttonLabel = "Force Close"
			parts := []string{}
			if activeRuns > 0 {
				parts = append(parts, fmt.Sprintf("%d active agent run(s)", activeRuns))
				for _, n := range runNames {
					if n != "" {
						parts = append(parts, fmt.Sprintf("  • %s", n))
					}
				}
			}
			if pendingAsks > 0 {
				parts = append(parts, fmt.Sprintf("%d session(s) waiting for permission approval", pendingAsks))
			}
			message = strings.Join(parts, "\n")
		}
	}

	dlg := app.Dialog.Question().
		SetTitle(title).
		SetMessage(message).
		AttachToWindow(window)
	cancel := dlg.AddButton("Cancel")
	cancel.SetAsCancel()
	cancel.SetAsDefault()
	dlg.AddButton(buttonLabel).OnClick(func() {
		showQuittingIndicator(window)
		app.Quit()
	})
	dlg.Show()
}
