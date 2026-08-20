package agent

import (
	"fmt"
	"strings"
)

// discoveryOptThresholds defines the "significant guide" gate for post-task
// discovery optimization. The optimizer only runs when discovery is ON and the
// turn was non-trivial AND exploration was costly. This avoids noise on trivial
// Q&A turns and avoids rewriting docs when the sticky ranking already worked.
//
// Gate = discoveryConfigEnabled() + disco.enabled + non-trivial turn
//        + ANY exploration inefficiency signal.
// See internal/agent/advisor_checkpoint.go:doneCheckpointMinToolCalls for the
// non-trivial baseline (reused here so "significant" matches "worth reviewing").

const (
	// discoveryOptMinExploreCalls is the grep/read/glob count that marks a turn
	// as having paid high exploration cost that discovery could have avoided.
	discoveryOptMinExploreCalls = 6

	// discoveryOptMaxAttachedUsedRatio below which attached docs are considered
	// low-precision (over-attached but unused). Only evaluated when attached>0.
	discoveryOptUnusedRatioThreshold float64 = 0.5
)

// discoveryOptSignal summarizes per-Step telemetry for the gate.
type discoveryOptSignal struct {
	toolCalls       int
	writeCalls      int
	grepReadCalls   int
	discoverMore    int
	attachedTotal   int
	attachedUsed    int
	queryHadLowRank bool // top rank < SelectMin (fail-open) or no attach despite non-trivial work
}

// shouldOptimizeDiscovery reports whether a post-task discovery optimization
// pass would give significant guide. Caller must have already checked
// discoveryConfigEnabled() && disco.enabled. Trivial turns (toolCalls<5 && writeCalls==0)
// never optimize. Otherwise requires at least one inefficiency signal.
func shouldOptimizeDiscovery(sig discoveryOptSignal) bool {
	// Non-trivial gate — mirrors advisor "done" checkpoint.
	if sig.toolCalls < doneCheckpointMinToolCalls && sig.writeCalls == 0 {
		return false
	}
	// Inefficiency signals — any one is enough to make the pass worthwhile.
	if sig.discoverMore > 0 {
		return true
	}
	if sig.grepReadCalls >= discoveryOptMinExploreCalls {
		return true
	}
	if sig.attachedTotal > 0 && float64(sig.attachedUsed)/float64(sig.attachedTotal) < discoveryOptUnusedRatioThreshold {
		// Attached but largely unused — WhenToUse/description tuning could help.
		return true
	}
	if sig.queryHadLowRank && sig.attachedTotal == 0 {
		// Non-trivial work but nothing attached — ranking missed, needs attention.
		return true
	}
	return false
}

// maybePostTaskDiscoveryOptimization is the post-Step hook. It is a no-op when
// discovery is off or the turn was not significant. When significant, it emits
// a low-noise debug line and, for future extension, records a suggestion
// (pin promotion or WhenToUse hint). It never auto-rewrites SKILL.md/plugin.json:
// those are upstream-owned and would bust the prompt cache + drift from source.
// Suggestions are surfaced via emitDebug / OnDiscovery-style notice so a human
// can run /learn or /discover pin explicitly.
//
// Why discovery-native, not context plugin/agent: discovery owns the cosine
// rank over Doc.Text (skill Name+Description+WhenToUse, MCP name+description,
// MD summaries) and the grow-only sticky Session. The context agent (sole writer
// of docs/ knowledge bundle under WithBundleLock) indexes conceptual docs
// semantically; conflating them would violate the sole-writer invariant and
// require contested locking. Plugins have no access to Session/Engine internals.
// See internal/agent/discovery_glue.go, internal/knowledge/store.go.
func (a *Agent) maybePostTaskDiscoveryOptimization(sig discoveryOptSignal, query string) {
	if !a.discoveryConfigEnabled() || a.disco == nil || !a.disco.enabled {
		return
	}
	if !shouldOptimizeDiscovery(sig) {
		return
	}
	// Build a concise guide for next-best discovery.
	var parts []string
	if sig.discoverMore > 0 {
		parts = append(parts, fmt.Sprintf("discover_more ×%d", sig.discoverMore))
	}
	if sig.grepReadCalls >= discoveryOptMinExploreCalls {
		parts = append(parts, fmt.Sprintf("explore cost %d reads/greps", sig.grepReadCalls))
	}
	if sig.attachedTotal > 0 {
		parts = append(parts, fmt.Sprintf("attached %d used %d", sig.attachedTotal, sig.attachedUsed))
	}
	if sig.queryHadLowRank && sig.attachedTotal == 0 {
		parts = append(parts, "low rank → no attach")
	}
	detail := strings.Join(parts, ", ")
	if detail == "" {
		detail = "inefficient ranking signal"
	}
	q := strings.TrimSpace(query)
	if len(q) > 60 {
		q = q[:60] + "…"
	}
	a.emitDebug("DISCOVERY-OPT", fmt.Sprintf("significant guide: %s (q=%.60q) — consider pinning used skills or tightening WhenToUse via /learn", detail, q))
	// Future: record to .ocode/discovery-opt.jsonl for offline tuning,
	// or enqueue an async small-model WhenToUse suggestion (1/model call, 20s
	// budget, fail-open). Not auto-applying keeps changes human-reviewed.
}

// collectDiscoveryOptSignal scans the Step's newMsgs (tool results + assistant
// tool calls) to build the signal. attachedTotal/attachedUsed are derived from
// the current discovery session; grep/read counts from tool call names.
func (a *Agent) collectDiscoveryOptSignal(ckpt *advisorCheckpointState, newMsgs []Message) discoveryOptSignal {
	sig := discoveryOptSignal{}
	if ckpt != nil {
		sig.toolCalls = ckpt.toolCalls
		sig.writeCalls = ckpt.writeCalls
	}
	if a.disco != nil && a.disco.session != nil {
		attached := a.disco.session.Attached()
		sig.attachedTotal = len(attached)
		// Count attached skills actually referenced via load_skill/skill tool calls
		// or via explicit discover_more results already counted. Approximate by
		// scanning tool call names for load_skill/skill usage and read paths that
		// overlap attached skill sources.
		used := make(map[string]struct{})
		for _, m := range newMsgs {
			for _, tc := range m.ToolCalls {
				switch tc.Function.Name {
				case "load_skill", "skill":
					// Argument contains skill name; count as used if attached.
					args := string(tc.Function.Arguments)
					for _, id := range attached {
						if strings.HasPrefix(id, "skill:") {
							name := strings.TrimPrefix(id, "skill:")
							if strings.Contains(args, name) {
								used[id] = struct{}{}
							}
						}
					}
				case "discover_more":
					sig.discoverMore++
				case "grep", "read", "glob", "list":
					sig.grepReadCalls++
				default:
					// MCP tools: if a tool call name matches an attached MCP doc, count it as used.
					for _, id := range attached {
						if strings.HasPrefix(id, "mcp:") {
							name := strings.TrimPrefix(id, "mcp:")
							if tc.Function.Name == name {
								used[id] = struct{}{}
							}
						}
					}
				}
			}
			// Tool results for grep/read are not counted here; only calls.
			// Count tool result messages that are expensive exploration steps.
			if m.Role == "tool" {
				// Already counted via call; keep shape stable.
			}
		}
		// Also attribute standalone tool result messages whose originating
		// assistant ToolCalls entry was dropped from newMsgs (defensive: handle
		// truncated history) by cross-referencing tool_call_id against every
		// ToolCalls entry seen anywhere in newMsgs.
		if sig.grepReadCalls == 0 {
			toolNameByID := make(map[string]string)
			for _, m := range newMsgs {
				for _, tc := range m.ToolCalls {
					toolNameByID[tc.ID] = tc.Function.Name
				}
			}
			for _, m := range newMsgs {
				if m.Role != "tool" {
					continue
				}
				switch toolNameByID[m.ToolID] {
				case "discover_more":
					sig.discoverMore++
				case "grep", "read", "glob", "list":
					sig.grepReadCalls++
				default:
					if strings.Contains(m.Content, "discover_more") {
						sig.discoverMore++
					}
				}
			}
		}
		sig.attachedUsed = len(used)
	} else {
		// Fallback: scan newMsgs directly when session not yet available.
		for _, m := range newMsgs {
			for _, tc := range m.ToolCalls {
				if tc.Function.Name == "discover_more" {
					sig.discoverMore++
				}
				if tc.Function.Name == "grep" || tc.Function.Name == "read" || tc.Function.Name == "glob" {
					sig.grepReadCalls++
				}
			}
		}
	}
	// Low-rank heuristic: if discovery is active but session stayed empty after
	// a non-trivial turn, the rank was likely below SelectMin.
	if sig.attachedTotal == 0 && sig.toolCalls >= doneCheckpointMinToolCalls {
		sig.queryHadLowRank = true
	}
	return sig
}
