# Part 09 — KindDiscovery Debug Log + Log Tab

Adds a `DISCOVERY` log kind, bridges the `internal/discovery` package's logger to
the global debug log (replacing the temp stub from Part 04), and surfaces it on the
Log tab with a color + filter entry.

## Task 12: KindDiscovery + Log tab + discovery logger bridge

**Files:**
- Modify: `internal/debuglog/debuglog.go` (add `KindDiscovery`)
- Modify: `internal/tui/debuglog.go` (add `DebugKindDiscovery` alias)
- Modify: `internal/tui/model.go` (add to `logKindFilter` default map ~4309; add to `kindColor` map ~13892)
- Create: `internal/discovery/debug.go` (real `emitDiscoveryDebug`)
- Modify: `internal/discovery/cache.go` (remove the temp stub)
- Test: `internal/debuglog/debuglog_test.go` (extend or create)

**Interfaces:**
- Produces: `debuglog.KindDiscovery EntryKind = "DISCOVERY"`; `tui.DebugKindDiscovery`.

- [ ] **Step 1: Write the failing test**

Create/append `internal/debuglog/debuglog_test.go`:

```go
package debuglog

import "testing"

func TestKindDiscoveryExists(t *testing.T) {
	Log.Append(Entry{Kind: KindDiscovery, Message: "rank: 3/12 attached"})
	snap := Log.Snapshot()
	found := false
	for _, e := range snap {
		if e.Kind == KindDiscovery && e.Message == "rank: 3/12 attached" {
			found = true
		}
	}
	if !found {
		t.Fatal("KindDiscovery entry must round-trip through the log")
	}
	if KindDiscovery != "DISCOVERY" {
		t.Fatalf("KindDiscovery value = %q", KindDiscovery)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/debuglog/ -run TestKindDiscovery -v`
Expected: FAIL — `KindDiscovery` undefined.

- [ ] **Step 3: Add the kind**

In `internal/debuglog/debuglog.go`, add to the `const` block:

```go
	KindDiscovery EntryKind = "DISCOVERY"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/debuglog/ -run TestKindDiscovery -v`
Expected: PASS.

- [ ] **Step 5: Add the TUI alias**

In `internal/tui/debuglog.go`, add to the `const` block:

```go
	DebugKindDiscovery = debuglog.KindDiscovery
```

- [ ] **Step 6: Show DISCOVERY on the Log tab**

In `internal/tui/model.go`, in the `logKindFilter` default-map initializer (~line 4309), add:

```go
		DebugKindDiscovery: true,
```

In the `kindColor` map in the log renderer (~line 13892), add a distinct color:

```go
	DebugKindDiscovery: lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7")).Bold(true),
```

(The renderer already falls back to `hintStyle` for unmapped kinds, so an explicit
color is the only change needed; the kind appears as soon as entries arrive.)

- [ ] **Step 7: Bridge the discovery package logger**

Create `internal/discovery/debug.go`:

```go
package discovery

import "github.com/u007/ocode/internal/debuglog"

// emitDiscoveryDebug forwards discovery-package log lines to the global debug log
// so they appear on the Log tab. kind is a debuglog EntryKind string
// ("DISCOVERY" or "WARN").
var emitDiscoveryDebug = func(kind, msg string) {
	debuglog.Log.Append(debuglog.Entry{Kind: debuglog.EntryKind(kind), Message: msg})
}
```

Remove the temporary stub line added in Part 04 from `internal/discovery/cache.go`:

```go
// DELETE this line:
var emitDiscoveryDebug = func(kind, msg string) {}
```

- [ ] **Step 8: Run tests + build**

Run: `go test ./internal/debuglog/ ./internal/discovery/ -v`
Expected: PASS (discovery warm now emits real log lines; no duplicate-symbol error).
Run: `go build ./...`
Expected: success.

- [ ] **Step 9: Commit**

```bash
git add internal/debuglog/debuglog.go internal/debuglog/debuglog_test.go internal/tui/debuglog.go internal/tui/model.go internal/discovery/debug.go internal/discovery/cache.go
git commit -m "feat(discovery): DISCOVERY debug-log kind + Log tab wiring"
```
