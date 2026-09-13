package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/u007/ocode/internal/lsp"
	"github.com/u007/ocode/internal/tool"
)

// LSP diagnostics reach the model on the MESSAGE level only — never as a
// system-role message. Every provider builder hoists system-role messages
// into the cached system block (collectAndRemoveSystemMessages and friends
// in client.go), so a system-role diagnostics block that changes with every
// edit rewrote and busted the cached system prompt on every turn.
//
// Two message-level channels replace it:
//
//  1. appendEditDiagnostics: after a tool that writes a single file, the
//     fresh diagnostics for that file are appended to the tool result. The
//     result is transcript history, so it is byte-stable once persisted.
//  2. injectLSPDelta: before each model call, files whose diagnostic set
//     changed since the agent last reported them (out-of-band edits,
//     package-level errors in files the edit did not touch) are rendered as
//     one user-role block in the uncached tail. No change → no block.

// lspMarker prefixes the user-role delta block so it can be grepped and so
// the model reads it as a system-side signal rather than user prose.
const lspMarker = "[ocode:lsp]"

// lspEditTools maps single-file write tools to the JSON arg key holding
// the path they wrote. Multi-file tools (multi_file_edit, apply_patch) are
// not covered here; their cross-file fallout surfaces via injectLSPDelta.
var lspEditTools = map[string]string{
	"write": "path", "edit": "path", "replace_lines": "path", "format": "path",
	"multiedit": "file_path",
}

// lspEditWait bounds how long a write tool waits for the language server
// to republish after the edit. Servers typically answer within a few
// hundred milliseconds; a slow or silent server must not stall the loop.
const lspEditWait = 2 * time.Second

// lspDeltaLineLimit caps the rendered delta so a project-wide breakage
// cannot flood the tail. The lsp_diagnostics tool paginates the rest.
const lspDeltaLineLimit = 50

// lspDiagnosticWaiter is the seam appendEditDiagnostics uses to reach the
// language server; tests substitute a stub. Production wires it to
// lsp.Manager.WaitDiagnosticsForPath in NewAgent.
type lspDiagnosticWaiter func(ctx context.Context, path string, since time.Time) ([]lsp.Diagnostic, bool, error)

// lspWaiterFor adapts an optional manager to the waiter seam.
func lspWaiterFor(mgr *lsp.Manager) lspDiagnosticWaiter {
	if mgr == nil {
		return nil
	}
	return mgr.WaitDiagnosticsForPath
}

// appendEditDiagnostics returns result with the fresh diagnostics for the
// file written by tool name appended, when there are any. It also records
// the reported set so injectLSPDelta does not repeat it on the next call.
func (a *Agent) appendEditDiagnostics(name string, args json.RawMessage, result string, since time.Time) string {
	if a == nil || a.lspWait == nil {
		return result
	}
	pathKey, ok := lspEditTools[name]
	if !ok {
		return result
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(args, &params); err != nil {
		return result
	}
	var path string
	if err := json.Unmarshal(params[pathKey], &path); err != nil || strings.TrimSpace(path) == "" {
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspEditWait)
	defer cancel()
	diags, fresh, err := a.lspWait(ctx, path, since)
	if err != nil {
		// No server for this file type or the binary is missing: expected
		// for most non-code files, so debug level only.
		a.emitDebug("LSP", fmt.Sprintf("post-edit diagnostics for %s skipped: %v", path, err))
		return result
	}
	if !fresh {
		a.emitDebug("LSP", fmt.Sprintf("post-edit diagnostics for %s: no publish within %s", path, lspEditWait))
		return result
	}
	a.markLSPReported(diags, path)
	if len(diags) == 0 {
		return result
	}
	return result + "\n\n" + tool.RenderDiagnosticsPage(diags, 0, lspDeltaLineLimit)
}

// injectLSPDelta appends one user-role block listing files whose
// diagnostics changed since the agent last reported them (through an edit
// attachment or a prior delta). It returns messages unchanged when nothing
// changed, which keeps the tail byte-identical across quiet iterations.
func (a *Agent) injectLSPDelta(messages []Message) []Message {
	if a == nil || a.lspMgr == nil {
		return messages
	}
	store := a.lspMgr.Diagnostics()
	if store == nil {
		return messages
	}
	current := groupDiagnosticsByURI(store.All())

	a.lspSeenMu.Lock()
	defer a.lspSeenMu.Unlock()
	if a.lspSeen == nil {
		a.lspSeen = make(map[string]string)
	}
	var changed, resolved []string
	for uri := range current {
		if a.lspSeen[uri] != fingerprintDiagnostics(current[uri]) {
			changed = append(changed, uri)
		}
	}
	for uri := range a.lspSeen {
		if _, still := current[uri]; !still {
			resolved = append(resolved, uri)
		}
	}
	if len(changed) == 0 && len(resolved) == 0 {
		return messages
	}
	sort.Strings(changed)
	sort.Strings(resolved)

	var b strings.Builder
	b.WriteString(lspMarker)
	b.WriteString(" LSP diagnostics changed since last reported:\n")
	lines := 0
	for _, uri := range changed {
		diags := current[uri]
		a.lspSeen[uri] = fingerprintDiagnostics(diags)
		if lines >= lspDeltaLineLimit {
			continue
		}
		remaining := lspDeltaLineLimit - lines
		b.WriteString(tool.RenderDiagnosticsPage(diags, 0, remaining))
		b.WriteByte('\n')
		if len(diags) < remaining {
			lines += len(diags)
		} else {
			lines = lspDeltaLineLimit
		}
	}
	for _, uri := range resolved {
		delete(a.lspSeen, uri)
		fmt.Fprintf(&b, "%s: clean (no diagnostics)\n", displayURIPath(uri))
	}
	if lines >= lspDeltaLineLimit {
		b.WriteString("(truncated; call lsp_diagnostics to page through the rest)\n")
	}
	out := make([]Message, 0, len(messages)+1)
	out = append(out, messages...)
	out = append(out, Message{Role: "user", Content: strings.TrimRight(b.String(), "\n")})
	return out
}

// markLSPReported records diags for path as already shown to the model.
// An empty diags marks the file clean so a later delta does not re-report
// a stale set, and so a clean→dirty flip is reported.
func (a *Agent) markLSPReported(diags []lsp.Diagnostic, path string) {
	a.lspSeenMu.Lock()
	defer a.lspSeenMu.Unlock()
	if a.lspSeen == nil {
		a.lspSeen = make(map[string]string)
	}
	if len(diags) == 0 {
		// The file is clean; forget any prior fingerprint. The delta only
		// reports "resolved" for files it saw dirty itself, so silently
		// dropping here is correct — the edit result already implied clean.
		if uri, err := lsp.AbsURI(path); err == nil {
			delete(a.lspSeen, uri)
		}
		return
	}
	a.lspSeen[diags[0].URI] = fingerprintDiagnostics(diags)
}

func groupDiagnosticsByURI(all []lsp.Diagnostic) map[string][]lsp.Diagnostic {
	out := make(map[string][]lsp.Diagnostic)
	for _, d := range all {
		out[d.URI] = append(out[d.URI], d)
	}
	return out
}

// fingerprintDiagnostics renders the set the same way the model sees it,
// so "changed" means "the text the model would read changed".
func fingerprintDiagnostics(diags []lsp.Diagnostic) string {
	return tool.RenderDiagnosticsPage(diags, 0, len(diags))
}

func displayURIPath(uri string) string {
	return strings.TrimPrefix(uri, "file://")
}
