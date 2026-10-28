package server

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// browserMutatingActions are the actions that can change a page, send a request
// or read a local file. The read-only agent types are refused every one of them.
var browserMutatingActions = []string{
	"back", "check", "click", "close", "fill", "fill_form", "forward", "key",
	"navigate", "open", "pick", "reload", "select", "type", "uncheck", "upload",
}

// browserReadActionNames are the actions the read-only agent types keep.
var browserReadActionNames = []string{
	"console", "find", "network", "screenshot", "scroll", "snapshot", "tabs", "text", "wait",
}

// readOnlyAgentTypes are the subagent types the browser tool refuses mutations for.
var readOnlyAgentTypes = []string{"explore", "context"}

// fullControlAgentTypes keep every browser action, whatever their name.
var fullControlAgentTypes = []string{"general", "debug", "scout", "docs", "build"}

// policyBrowserTool returns one agent's instance of the browser tool over a seeded
// strip, with the fake surfaces it drives and the fake tab it attaches.
func policyBrowserTool(t *testing.T, agentName string) (*browserTool, *fakeSurface, *fakeSurfaces) {
	t.Helper()
	surf := &fakeSurface{png: solidPNG(t, 200, 100), cssW: 200, cssH: 100}
	parent, _ := newTestBrowserTool(t, surf, 1568)
	child, ok := parent.ForAgent(agentName).(*browserTool)
	if !ok {
		t.Fatalf("ForAgent(%q) did not return a browser tool", agentName)
	}
	return child, surf, parent.surfaces.(*fakeSurfaces)
}

// TestBrowserRefusal_ReadOnlyAgentsRefuseEveryMutatingAction pins that explore and
// context get a refusal, as a tool error, for every mutating action, and that the
// refused call reaches no Chrome target and no tab.
func TestBrowserRefusal_ReadOnlyAgentsRefuseEveryMutatingAction(t *testing.T) {
	for _, agentName := range readOnlyAgentTypes {
		for _, action := range browserMutatingActions {
			t.Run(agentName+"/"+action, func(t *testing.T) {
				b, surf, fs := policyBrowserTool(t, agentName)
				args := `{"action":"` + action + `","tab":"b1"}`
				refusal := b.Refusal(json.RawMessage(args))
				if refusal == "" {
					t.Fatalf("Refusal(%s) for %s = empty; want a refusal", action, agentName)
				}
				_, err := browserCall(t, b, args)
				if err == nil || err.Error() != refusal {
					t.Fatalf("ExecuteCtx(%s) error = %v, want the refusal %q", action, err, refusal)
				}
				if got := surf.got(); len(got) != 0 {
					t.Fatalf("refused %s reached the tab: %v", action, got)
				}
				if len(fs.surfaceCalls) != 0 || len(fs.closeKeys) != 0 {
					t.Fatalf("refused %s attached or closed a target: attach %v, close %v", action, fs.surfaceCalls, fs.closeKeys)
				}
				if got := b.openedTabs(); len(got) != 0 {
					t.Fatalf("refused open left a tab in the agent's list: %v", got)
				}
			})
		}
	}
}

// TestBrowserRefusal_ReadOnlyAgentsKeepEveryReadAction pins that a read action
// still runs for explore and context.
func TestBrowserRefusal_ReadOnlyAgentsKeepEveryReadAction(t *testing.T) {
	args := map[string]string{
		"console":    `{"action":"console"}`,
		"find":       `{"action":"find","target":"button"}`,
		"network":    `{"action":"network"}`,
		"screenshot": `{"action":"screenshot"}`,
		"scroll":     `{"action":"scroll","direction":"down"}`,
		"snapshot":   `{"action":"snapshot"}`,
		"tabs":       `{"action":"tabs"}`,
		"text":       `{"action":"text"}`,
		"wait":       `{"action":"wait","text":"Done"}`,
	}
	for _, agentName := range readOnlyAgentTypes {
		for _, action := range browserReadActionNames {
			t.Run(agentName+"/"+action, func(t *testing.T) {
				b, _, _ := policyBrowserTool(t, agentName)
				if got := b.Refusal(json.RawMessage(args[action])); got != "" {
					t.Fatalf("Refusal(%s) for %s = %q; want none", action, agentName, got)
				}
				if _, err := browserCall(t, b, args[action]); err != nil {
					t.Fatalf("read action %s for %s: %v", action, agentName, err)
				}
			})
		}
	}
}

// TestBrowserRefusal_FullControlAgentTypesKeepMutations pins that general, debug,
// scout, docs and the main agent (no type name) are refused nothing.
func TestBrowserRefusal_FullControlAgentTypesKeepMutations(t *testing.T) {
	for _, agentName := range append([]string{""}, fullControlAgentTypes...) {
		b, _, _ := policyBrowserTool(t, agentName)
		for _, action := range browserMutatingActions {
			if got := b.Refusal(json.RawMessage(`{"action":"` + action + `"}`)); got != "" {
				t.Errorf("agent %q: Refusal(%s) = %q; want none", agentName, action, got)
			}
		}
	}
}

// TestBrowserRefusal_ReadOnlyPartitionMatchesEveryAction pins that every action in
// the static enum is either read-only or refused for explore, so a new action is
// never allowed to the read-only types by accident.
func TestBrowserRefusal_ReadOnlyPartitionMatchesEveryAction(t *testing.T) {
	if len(browserMutatingActions)+len(browserReadActionNames) != len(browserActions) {
		t.Fatalf("policy lists cover %d actions, the enum has %d",
			len(browserMutatingActions)+len(browserReadActionNames), len(browserActions))
	}
	b, _, _ := policyBrowserTool(t, "explore")
	for _, action := range browserActions {
		refused := b.Refusal(json.RawMessage(`{"action":"`+action+`"}`)) != ""
		if want := !slices.Contains(browserReadActionNames, action); refused != want {
			t.Errorf("explore: action %s refused = %v, want %v", action, refused, want)
		}
	}
}

// TestBrowserRefusal_UnparseableArgumentsAreNotRefusedHere pins that Refusal leaves
// unparseable arguments to ExecuteCtx, which reports them as invalid arguments.
func TestBrowserRefusal_UnparseableArgumentsAreNotRefusedHere(t *testing.T) {
	b, _, _ := policyBrowserTool(t, "explore")
	if got := b.Refusal(json.RawMessage(`{"action":`)); got != "" {
		t.Fatalf("Refusal of unparseable arguments = %q; want none", got)
	}
	_, err := browserCall(t, b, `{"action":`)
	if err == nil || !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("unparseable arguments error = %v, want invalid arguments", err)
	}
}

// TestBrowserTool_FillFormEmptyFileListNamesTheField pins the message for an empty
// file list in fill_form: it names the field and the fix, and does not talk about
// an upload. The refused call reaches no tab.
func TestBrowserTool_FillFormEmptyFileListNamesTheField(t *testing.T) {
	surf := &fakeSurface{}
	b, _ := newTestBrowserTool(t, surf, 1568)
	fs := b.surfaces.(*fakeSurfaces)
	_, err := browserCall(t, b, `{"action":"fill_form","fields":[{"selector":"#files","value":[]}]}`)
	if err == nil {
		t.Fatal("fill_form with an empty file list was accepted")
	}
	want := "browser: fill_form field 0 (#files): the file list is empty; give at least one path, or null to skip the field"
	if err.Error() != want {
		t.Fatalf("fill_form empty file list error = %q, want %q", err.Error(), want)
	}
	if len(fs.surfaceCalls) != 0 || len(surf.got()) != 0 {
		t.Fatalf("refused fill_form reached Chrome: attach %v, calls %v", fs.surfaceCalls, surf.got())
	}
}
