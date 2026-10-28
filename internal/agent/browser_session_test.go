package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// browserStubTool is a fake browser tool. It reports a fixed page, counts the
// calls it executes, and counts the CloseOpenedTabs calls teardown makes.
type browserStubTool struct {
	page     map[string]any
	ok       bool
	executed int
	closes   int
	closeErr error
}

func (b *browserStubTool) Name() string        { return "browser" }
func (b *browserStubTool) Description() string { return "fake" }
func (b *browserStubTool) Parallel() bool      { return false }
func (b *browserStubTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "browser"}
}
func (b *browserStubTool) Execute(json.RawMessage) (string, error) {
	b.executed++
	return "ran", nil
}
func (b *browserStubTool) PageContext(json.RawMessage) (map[string]any, bool) {
	if !b.ok {
		return nil, false
	}
	out := map[string]any{}
	for k, v := range b.page {
		out[k] = v
	}
	return out, true
}
func (b *browserStubTool) CloseOpenedTabs(context.Context) error {
	b.closes++
	return b.closeErr
}

var (
	_ tool.PageContextTool = (*browserStubTool)(nil)
	_ tool.SessionCloser   = (*browserStubTool)(nil)
)

// ownTab is a page the agent opened itself; strip is a page from the user's strip.
var (
	ownTab = map[string]any{
		"targets_page": true, "tab_id": "agent1", "opened_by_agent": true,
		"page_url": "https://scratch.example/", "page_host": "scratch.example",
	}
	stripTab = map[string]any{
		"targets_page": true, "tab_id": "b1", "opened_by_agent": false,
		"page_url": "https://bank.example/account", "page_host": "bank.example",
	}
)

// TestBrowserCloseOfOwnTabNeedsNoAsk pins that a tab this agent opened is closed
// without a prompt.
func TestBrowserCloseOfOwnTabNeedsNoAsk(t *testing.T) {
	bt := &browserStubTool{ok: true, page: ownTab}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	out, err := ag.handleToolCall("browser", json.RawMessage(`{"action":"close","tab":"agent1"}`), nil, "")
	if err != nil {
		t.Fatalf("close of own tab: %v", err)
	}
	if strings.HasPrefix(out, tool.SentinelPermissionAsk) {
		t.Fatalf("close of own tab asked for permission: %q", out)
	}
	if bt.executed != 1 {
		t.Fatalf("close of own tab executed %d times, want 1", bt.executed)
	}
}

// TestBrowserCloseOfStripTabAsksWithURL pins that closing a tab the agent did not
// open asks, and the dialog names the page.
func TestBrowserCloseOfStripTabAsksWithURL(t *testing.T) {
	bt := &browserStubTool{ok: true, page: stripTab}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	out, err := ag.handleToolCall("browser", json.RawMessage(`{"action":"close","tab":"b1"}`), nil, "")
	if err != nil {
		t.Fatalf("close of strip tab: %v", err)
	}
	if !strings.HasPrefix(out, tool.SentinelPermissionAsk) {
		t.Fatalf("close of strip tab did not ask: %q", out)
	}
	payload := strings.TrimPrefix(out, tool.SentinelPermissionAsk)
	for _, want := range []string{"close tab", "https://bank.example/account", "bank.example"} {
		if !strings.Contains(payload, want) {
			t.Errorf("ask lacks %q: %s", want, payload)
		}
	}
	if bt.executed != 0 {
		t.Fatalf("close of strip tab ran before an answer: %d executions", bt.executed)
	}
}

// TestBrowserClickAskShowsPageHost pins that a click's permission summary names
// the page it acts on, not only the tab id and element.
func TestBrowserClickAskShowsPageHost(t *testing.T) {
	bt := &browserStubTool{ok: true, page: stripTab}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	out, err := ag.handleToolCall("browser", json.RawMessage(`{"action":"click","tab":"b1","target":"@e4"}`), nil, "")
	if err != nil {
		t.Fatalf("click: %v", err)
	}
	if !strings.HasPrefix(out, tool.SentinelPermissionAsk) {
		t.Fatalf("click did not ask: %q", out)
	}
	if payload := strings.TrimPrefix(out, tool.SentinelPermissionAsk); !strings.Contains(payload, "bank.example") {
		t.Fatalf("click ask lacks the page host: %s", payload)
	}
}

// TestBrowserCloseOfOwnTabDenyIsNotLifted pins that the own-tab allow only turns
// an Ask into an Allow. A user deny on the tool still stands.
func TestBrowserCloseOfOwnTabDenyIsNotLifted(t *testing.T) {
	bt := &browserStubTool{ok: true, page: ownTab}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	ag.permissions.SetRule("browser", PermissionDeny)
	out, err := ag.handleToolCall("browser", json.RawMessage(`{"action":"close","tab":"agent1"}`), nil, "")
	if err != nil {
		t.Fatalf("close under deny: %v", err)
	}
	if bt.executed != 0 {
		t.Fatalf("close of own tab ran under a tool deny: %q", out)
	}
}

// TestShutdownClosesTheAgentsOwnTabs pins that Shutdown closes what the agent's
// tools opened.
func TestShutdownClosesTheAgentsOwnTabs(t *testing.T) {
	bt := &browserStubTool{ok: true, page: ownTab, closeErr: errors.New("tab already gone")}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	ag.Shutdown()
	if bt.closes != 1 {
		t.Fatalf("Shutdown called CloseOpenedTabs %d times, want 1", bt.closes)
	}
}

// TestSubagentRunEndClosesItsOwnTabs pins that a subagent's tools are closed
// when its run finishes, on the synchronous and the background path.
func TestSubagentRunEndClosesItsOwnTabs(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "look at the page"}}

	t.Run("sync", func(t *testing.T) {
		caller := buildResumeCallerAgent(t, "build")
		child := &browserStubTool{ok: true, page: ownTab}
		sub := newTestAgent(&captureClient{}, []tool.Tool{child}, &config.Config{}, nil)
		task := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}
		if _, err := task.runSyncDispatch("explore", sub, nil, msgs, func() {}); err != nil {
			t.Fatalf("runSyncDispatch: %v", err)
		}
		if child.closes != 1 {
			t.Fatalf("sync run end called CloseOpenedTabs %d times, want 1", child.closes)
		}
	})

	t.Run("background", func(t *testing.T) {
		caller := buildResumeCallerAgent(t, "build")
		child := &browserStubTool{ok: true, page: ownTab}
		sub := newTestAgent(&captureClient{}, []tool.Tool{child}, &config.Config{}, nil)
		task := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}
		run := caller.runs.New("explore")
		task.runBackgroundDispatch("explore", sub, run, msgs, "started", func() {})
		run.awaitTeardown(10 * time.Second)
		if child.closes != 1 {
			t.Fatalf("background run end called CloseOpenedTabs %d times, want 1", child.closes)
		}
	})
}
