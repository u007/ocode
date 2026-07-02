# Part 03 — Wails v3 shell: app, window, menus, tray, badge, notifications

Global constraints from `INDEX.md` apply. Only this package (`cmd/ocode-desktop`) may import Wails. Wails v3 is alpha: every step marked **API-verify** gives the exact `go doc` command — run it first and adjust symbol names only (never the design) if the pinned version renamed something.

**Interfaces consumed (produced by Parts 01–02):**

```go
// package github.com/u007/ocode/web
func FS() fs.FS

// package github.com/u007/ocode/internal/server
type RunState struct {
	SessionID string
	ID        string
	Name      string
	Ended     bool
	Failed    bool
}
func (s *Server) RunStates() []RunState

// package github.com/u007/ocode/internal/desktop
type Handle struct {
	URL   string // "http://127.0.0.1:<port>", no trailing slash
	Token string
	Srv   *server.Server
}
func StartServer(webFS fs.FS, workDir string) (*Handle, error)
type Summary struct {
	RunningCount int
	Finished     []server.RunState
}
func Watch(ctx context.Context, interval time.Duration, source func() []server.RunState, onChange func(Summary))
```

---

### Task 5: Wails app + window + menus

**Files:**
- Create: `cmd/ocode-desktop/main.go`
- Modify: `go.mod` / `go.sum` (via `go get`, pinned)

**Interfaces:**
- Produces: `newShell()` split point consumed by Task 6 — Task 5 writes `main.go` with a `// Task 6 wires tray/badge/notifications here` marker comment where Task 6 inserts its call.

- [ ] **Step 1: Pin the Wails v3 dependency (never `latest`)**

```bash
go list -m -versions github.com/wailsapp/wails/v3 | tr ' ' '\n' | grep alpha | tail -3
```

Pick the newest version printed and pin it explicitly, e.g.:

```bash
go get github.com/wailsapp/wails/v3@v3.0.0-alpha.88   # replace with newest printed
go mod tidy
```

Record the chosen version — Part 04's docs step mentions it. Expected: `go.mod` gains the pinned `require`.

- [ ] **Step 2: API-verify the symbols this task uses**

```bash
go doc github.com/wailsapp/wails/v3/pkg/application Options | head -40
go doc github.com/wailsapp/wails/v3/pkg/application WebviewWindowOptions | head -60
go doc github.com/wailsapp/wails/v3/pkg/application App | grep -iE 'menu|quit|window'
go doc github.com/wailsapp/wails/v3/pkg/application NewMenu
go doc github.com/wailsapp/wails/v3/pkg/application Menu | grep -iE 'AddRole|role'
go doc github.com/wailsapp/wails/v3/pkg/application ErrorDialog
go doc github.com/wailsapp/wails/v3/pkg/application MacOptions | grep -i terminate
go doc github.com/wailsapp/wails/v3/pkg/application WebviewWindowOptions | grep -iE 'link|browser'
```

Confirm: options struct fields (`Title`, `Width`, `Height`, `URL`), menu roles (`application.AppMenu`, `application.EditMenu`, `application.WindowMenu`), `app.SetMenu` (or `app.Menu.Set...`), `application.ErrorDialog()`, and Mac option `ApplicationShouldTerminateAfterLastWindowClosed`. Also note from the last command whether an "open external links in browser" window option exists — record yes/no for Part 04's TODO step.

- [ ] **Step 3: Write `cmd/ocode-desktop/main.go`**

```go
// ocode-desktop is the native desktop shell: a Wails v3 window over the
// in-process ocode web server. All app/session logic stays behind the same
// HTTP/SSE API the browser uses; this binary only owns window chrome and
// OS integration. See docs/superpowers/specs/2026-07-02-desktop-shell-design.md.
package main

import (
	"context"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/u007/ocode/internal/desktop"
	"github.com/u007/ocode/web"
)

func main() {
	workDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("ocode-desktop: resolve working directory: %v", err)
	}

	handle, bootErr := desktop.StartServer(web.FS(), workDir)

	app := application.New(application.Options{
		Name: "ocode",
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	if bootErr != nil {
		// Native dialog so a double-clicked .app surfaces the failure.
		log.Printf("ocode-desktop: server start failed: %v", bootErr)
		application.ErrorDialog().
			SetTitle("ocode failed to start").
			SetMessage(bootErr.Error()).
			Show()
		os.Exit(1)
	}

	url := handle.URL + "/?token=" + handle.Token
	if dev := os.Getenv("OCODE_DESKTOP_DEV_URL"); dev != "" {
		// Frontend served by Vite; API still runs in-process. The dev
		// server must proxy /api to handle.URL — printed here for that.
		log.Printf("ocode-desktop: dev mode; API at %s (token %s)", handle.URL, handle.Token)
		url = dev + "/?token=" + handle.Token
	}

	// Native menu incl. Edit role: without it Cmd+C/V/X do not work in the
	// macOS webview.
	menu := application.NewMenu()
	menu.AddRole(application.AppMenu)
	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)
	app.SetMenu(menu)

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "ocode",
		Width:  1280,
		Height: 840,
		URL:    url,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Task 6 wires tray/badge/notifications here (uses ctx, win, handle).
	_ = ctx
	_ = win

	if err := app.Run(); err != nil {
		log.Fatalf("ocode-desktop: %v", err)
	}
}
```

Adjust symbol names per Step 2 findings (e.g. if `app.SetMenu` is `app.Menu.SetApplicationMenu` in the pinned alpha).

- [ ] **Step 4: Build and smoke-run**

```bash
go build -o bin/ocode-desktop ./cmd/ocode-desktop && go build ./...
./bin/ocode-desktop
```

Expected: window opens showing the ocode web UI (SPA loads, no auth error banner — token flowed through `?token=`). Verify Cmd+C/V works in the chat input. Quit via Cmd+Q; process exits.
Also verify the main binary stayed pure-Go: `go list -deps ./... | grep -v ocode-desktop | grep wailsapp` under the root build — simplest check: `grep -rn "wailsapp" --include="*.go" . | grep -v cmd/ocode-desktop | grep -v _test` must output nothing (excluding go.mod/go.sum).

- [ ] **Step 5: Commit**

```bash
git add cmd/ocode-desktop/main.go go.mod go.sum
git commit -m "feat: ocode-desktop Wails v3 shell (window over in-process server)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Tray, dock badge, notifications

**Files:**
- Modify: `cmd/ocode-desktop/main.go` (replace the `// Task 6 wires...` marker)
- Create: `cmd/ocode-desktop/native.go`

**Interfaces:**
- Consumes: `desktop.Watch`, `desktop.Summary`, `handle.Srv.RunStates` (see header block), `win`, `app`, `ctx` from Task 5.
- Produces: `wireNative(ctx context.Context, app *application.App, win *application.WebviewWindow, handle *desktop.Handle, notifier *notifications.Service, badgeSvc *badge.Service)` — called once from `main` (exact service types come from the pinned alpha's `New()` return types; verify in Step 1).

- [ ] **Step 1: API-verify service symbols**

```bash
go doc github.com/wailsapp/wails/v3/pkg/services/notifications | head -30
go doc github.com/wailsapp/wails/v3/pkg/services/notifications NotificationOptions
go doc github.com/wailsapp/wails/v3/pkg/services/badge | head -20
go doc github.com/wailsapp/wails/v3/pkg/application SystemTray | grep -iE 'New|SetMenu|SetLabel|SetTemplateIcon|SetIcon'
go doc github.com/wailsapp/wails/v3/pkg/events Common | grep -iE 'Focus'
go doc github.com/wailsapp/wails/v3/pkg/icons | head
```

Confirm: `notifications.New()`, `SendNotification(...)` + macOS `RequestNotificationAuthorization`, `badge.New()` + `SetBadge(string)`/`RemoveBadge()`, tray API, `events.Common.WindowFocus` / `WindowLostFocus`. If the badge service is named `dock` in the pinned alpha (it moved once), use that package with the same call shape.

- [ ] **Step 2: Register the services in `application.Options` (edit Task 5's `main.go`)**

```go
notifier := notifications.New()
badgeSvc := badge.New()

app := application.New(application.Options{
	Name: "ocode",
	Services: []application.Service{
		application.NewService(notifier),
		application.NewService(badgeSvc),
	},
	Mac: application.MacOptions{
		ApplicationShouldTerminateAfterLastWindowClosed: true,
	},
})
```

(add imports `.../pkg/services/notifications`, `.../pkg/services/badge`), then replace the Task 6 marker lines with:

```go
wireNative(ctx, app, win, handle, notifier, badgeSvc)
```

- [ ] **Step 3: Write `cmd/ocode-desktop/native.go`**

```go
package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"
	"github.com/wailsapp/wails/v3/pkg/services/badge"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/u007/ocode/internal/desktop"
)

// wireNative connects OS integration (tray, dock badge, notifications) to
// the in-process server's run state. All state flows one way:
// RunStates() → desktop.Watch poll-and-diff → badge/notification calls.
func wireNative(ctx context.Context, app *application.App, win *application.WebviewWindow, handle *desktop.Handle, notifier *notifications.Service, badgeSvc *badge.Service) {
	// --- window focus tracking (notifications fire only when unfocused) ---
	var focused atomic.Bool
	focused.Store(true) // window opens focused
	win.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) { focused.Store(true) })
	win.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) { focused.Store(false) })

	// --- system tray ---
	tray := app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(icons.SystrayMacTemplate)
	} else {
		tray.SetIcon(icons.SystrayLight)
		tray.SetDarkModeIcon(icons.SystrayDark)
	}
	trayMenu := application.NewMenu()
	trayMenu.Add("Show ocode").OnClick(func(*application.Context) {
		win.Show()
		win.Focus()
	})
	trayMenu.Add("Quit").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(trayMenu)

	// --- macOS notification permission (no-op elsewhere) ---
	if runtime.GOOS == "darwin" {
		if _, err := notifier.RequestNotificationAuthorization(); err != nil {
			// Expected when the user previously denied; badge/tray still work.
			log.Printf("ocode-desktop: notification authorization: %v", err)
		}
	}

	// --- run-state → badge + notifications ---
	go desktop.Watch(ctx, 750*time.Millisecond, handle.Srv.RunStates, func(sum desktop.Summary) {
		// Dock badge: running-agent count. macOS/Windows only; the badge
		// service is a no-op/error on Linux — log once-per-change, not fatal.
		if sum.RunningCount > 0 {
			if err := badgeSvc.SetBadge(fmt.Sprintf("%d", sum.RunningCount)); err != nil {
				log.Printf("ocode-desktop: set badge: %v", err)
			}
		} else {
			if err := badgeSvc.RemoveBadge(); err != nil {
				log.Printf("ocode-desktop: remove badge: %v", err)
			}
		}

		if focused.Load() {
			return // user is looking at the window; no notifications
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
```

Adjust exact method receivers/signatures per Step 1's `go doc` output (e.g. `SendNotification` may take a context in newer alphas; `RequestNotificationAuthorization` return shape varies). Clicking a notification focusing the window: check `go doc .../notifications | grep -i 'OnNotification\|Response'` — if a response callback exists, wire `win.Show(); win.Focus()` in it; if the pinned alpha has none, record that as a Part 04 TODO entry.

- [ ] **Step 4: Build and smoke-test**

```bash
go build -o bin/ocode-desktop ./cmd/ocode-desktop && ./bin/ocode-desktop
```

Manual checks (macOS dev machine):
1. Tray icon appears; "Show ocode" reveals the window after closing-to-tray/hide; "Quit" exits.
2. Start an agent task in the UI that spawns a subagent → dock badge shows a count while running, clears when done.
3. Unfocus the window before the run finishes → native notification appears. Focused → no notification.

- [ ] **Step 5: Run full test suite**

Run: `go build ./... && go test ./...`
Expected: PASS (native.go has no unit tests — all logic with branches lives in `internal/desktop`, already covered).

- [ ] **Step 6: Commit**

```bash
git add cmd/ocode-desktop/main.go cmd/ocode-desktop/native.go go.mod go.sum
git commit -m "feat(desktop): tray, dock badge, and run notifications

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```
