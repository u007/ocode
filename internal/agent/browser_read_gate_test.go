package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// browserUserTabReadArgs is one call per read action that asks on a tab the agent
// did not open. wait names text, so the call is valid without a target.
var browserUserTabReadArgs = map[string]string{
	"text":       `{"action":"text","tab":"b1"}`,
	"snapshot":   `{"action":"snapshot","tab":"b1"}`,
	"find":       `{"action":"find","tab":"b1","target":"button"}`,
	"console":    `{"action":"console","tab":"b1"}`,
	"network":    `{"action":"network","tab":"b1"}`,
	"screenshot": `{"action":"screenshot","tab":"b1"}`,
	"wait":       `{"action":"wait","tab":"b1","text":"Done"}`,
}

// TestBrowserReadOfOwnTabIsAllowed pins that the read actions run without a
// prompt on a tab this agent opened.
func TestBrowserReadOfOwnTabIsAllowed(t *testing.T) {
	for action, args := range browserUserTabReadArgs {
		t.Run(action, func(t *testing.T) {
			bt := &browserStubTool{ok: true, page: ownTab}
			ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
			out, err := ag.handleToolCall("browser", json.RawMessage(args), nil, "")
			if err != nil {
				t.Fatalf("%s on own tab: %v", action, err)
			}
			if strings.HasPrefix(out, tool.SentinelPermissionAsk) {
				t.Fatalf("%s on own tab asked for permission: %q", action, out)
			}
			if bt.executed != 1 {
				t.Fatalf("%s on own tab executed %d times, want 1", action, bt.executed)
			}
		})
	}
}

// TestBrowserReadOfUserTabAsksInNormalMode pins that every read action asks before
// it touches a tab the agent did not open, and that the dialog names the page.
func TestBrowserReadOfUserTabAsksInNormalMode(t *testing.T) {
	for action, args := range browserUserTabReadArgs {
		t.Run(action, func(t *testing.T) {
			bt := &browserStubTool{ok: true, page: stripTab}
			ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
			out, err := ag.handleToolCall("browser", json.RawMessage(args), nil, "")
			if err != nil {
				t.Fatalf("%s on user tab: %v", action, err)
			}
			if !strings.HasPrefix(out, tool.SentinelPermissionAsk) {
				t.Fatalf("%s on user tab did not ask: %q", action, out)
			}
			if payload := strings.TrimPrefix(out, tool.SentinelPermissionAsk); !strings.Contains(payload, "bank.example") {
				t.Fatalf("%s ask lacks the page host: %s", action, payload)
			}
			if bt.executed != 0 {
				t.Fatalf("%s on user tab ran before an answer: %d executions", action, bt.executed)
			}
		})
	}
}

// TestBrowserTabsAndScrollOnUserTabAreAllowed pins the reads that do not ask:
// tabs lists the strip and reads no page, and scroll only moves the viewport.
func TestBrowserTabsAndScrollOnUserTabAreAllowed(t *testing.T) {
	bt := &browserStubTool{ok: true, page: map[string]any{"targets_page": false}}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	out, err := ag.handleToolCall("browser", json.RawMessage(`{"action":"tabs"}`), nil, "")
	if err != nil || strings.HasPrefix(out, tool.SentinelPermissionAsk) {
		t.Fatalf("tabs = (%q, %v), want it allowed", out, err)
	}

	scroll := &browserStubTool{ok: true, page: stripTab}
	ag = newTestAgent(nil, []tool.Tool{scroll}, &config.Config{}, nil)
	out, err = ag.handleToolCall("browser", json.RawMessage(`{"action":"scroll","tab":"b1","direction":"down"}`), nil, "")
	if err != nil || strings.HasPrefix(out, tool.SentinelPermissionAsk) {
		t.Fatalf("scroll on user tab = (%q, %v), want it allowed", out, err)
	}
}

// TestBrowserReadOfUserTabAlwaysAllowStopsAsking pins that an "always allow" answer
// to the browser tool's ask holds for later user-tab reads: the second read runs
// without a second prompt.
func TestBrowserReadOfUserTabAlwaysAllowStopsAsking(t *testing.T) {
	bt := &browserStubTool{ok: true, page: stripTab}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	asks := 0
	ag.OnPermissionAsk = func(PermissionRequest) PermissionResponse {
		asks++
		return PermissionResponse{Level: PermissionAllow, PersistRule: true}
	}
	args := json.RawMessage(`{"action":"text","tab":"b1"}`)
	if _, err := ag.handleToolCall("browser", args, nil, ""); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if asks != 1 {
		t.Fatalf("first read asked %d times, want 1", asks)
	}
	if _, err := ag.handleToolCall("browser", args, nil, ""); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if asks != 1 {
		t.Fatalf("second read asked again after always-allow (asks=%d)", asks)
	}
	if bt.executed != 2 {
		t.Fatalf("executions = %d, want 2", bt.executed)
	}
}

// TestBrowserReadOfUserTabHonoursADenyRule pins that a deny on the browser tool
// still stops a read of a user tab, with no prompt.
func TestBrowserReadOfUserTabHonoursADenyRule(t *testing.T) {
	bt := &browserStubTool{ok: true, page: stripTab}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	ag.permissions.SetRule("browser", PermissionDeny)
	out, err := ag.handleToolCall("browser", json.RawMessage(`{"action":"text","tab":"b1"}`), nil, "")
	if err != nil {
		t.Fatalf("read under deny: %v", err)
	}
	if strings.HasPrefix(out, tool.SentinelPermissionAsk) || bt.executed != 0 {
		t.Fatalf("read under deny asked or ran: out=%q executed=%d", out, bt.executed)
	}
}

// TestBrowserReadOfUserTabNotAskedInYOLO pins that YOLO, which allows every
// browser action, does not add a read ask.
func TestBrowserReadOfUserTabNotAskedInYOLO(t *testing.T) {
	bt := &browserStubTool{ok: true, page: stripTab}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	ag.permissions.SetMode(PermissionModeYOLO)
	out, err := ag.handleToolCall("browser", json.RawMessage(`{"action":"text","tab":"b1"}`), nil, "")
	if err != nil || strings.HasPrefix(out, tool.SentinelPermissionAsk) || bt.executed != 1 {
		t.Fatalf("yolo read = (%q, %v) executed %d; want it run without an ask", out, err, bt.executed)
	}
}

// TestBrowserReadOfUnresolvedTabAsks pins fail-closed: a read whose page cannot be
// resolved is asked about, with the summary saying so.
func TestBrowserReadOfUnresolvedTabAsks(t *testing.T) {
	bt := &browserStubTool{ok: false}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	out, err := ag.handleToolCall("browser", json.RawMessage(`{"action":"text","tab":"nope"}`), nil, "")
	if err != nil {
		t.Fatalf("read of unresolved tab: %v", err)
	}
	if !strings.HasPrefix(out, tool.SentinelPermissionAsk) || !strings.Contains(out, "could not be resolved") {
		t.Fatalf("read of unresolved tab = %q, want an ask that says the page is unresolved", out)
	}
	if bt.executed != 0 {
		t.Fatalf("read of unresolved tab ran before an answer")
	}
}
