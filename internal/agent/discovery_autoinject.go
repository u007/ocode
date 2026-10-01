package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/discovery"
	"github.com/u007/ocode/internal/skill"
)

// Auto-injection of a single top-ranked skill body.
//
// Discovery normally advertises skills by NAME only (a cached, system-role name
// index) and lets the model decide to call the skill tool for the body. That
// costs a round trip and depends on the model choosing to load it. When the
// TypeSafe judge (Jev) is confident that ONE skill is the right one for the
// request, this path spends that round trip up front and inlines the body.
//
// The rules that keep this cheap and safe:
//
//   - Top ONE skill, chosen by the judge's noul score, not by embedder rank. The
//     embedder proposes; Jev decides.
//   - Its own confidence floor (discoveryAutoInjectFloor), NOT the lenient
//     relevance floor and NOT the permission floor. See the const's comment.
//   - Fail-CLOSED. No judge (TypeSafe disconnected), a judge transport error, or
//     a missing/non-noul answer all mean no score, and no score means no
//     injection. This is the opposite of the veto judges' fail-open rule, and
//     deliberately so: a veto can only hide a retrieval result, while a spurious
//     injection spends real prompt budget on the wrong instructions.
//   - Once per session, per skill. Session.Select only returns docs that are not
//     already attached, so a skill is a candidate exactly once — there is no
//     re-judging loop to get wrong.
//   - Never re-inject what the chat already has (chatAlreadyHasSkill).

const (
	// discoveryAutoInjectFloor is the noul (yes-probability) a skill must reach
	// before its full body is inlined.
	//
	// It is deliberately its own constant, and it sits between the two floors
	// that already exist:
	//
	//   - relevanceJudgeMinConfidenceDefault (0.5) is far too lenient here. It
	//     answers "should this name be shown?", and the product rule for that is
	//     "even slight relevancy should be presented". Inlining a body is a much
	//     bigger commitment than printing a name.
	//   - permissions.auto.min_confidence (0.85) governs whether a tool call is
	//     ALLOWED. Reusing it here would couple a prompt-spend decision to a
	//     permission tuning, so a stricter auto-permission setting would silently
	//     disable auto-injection.
	//
	// 0.8 is a deliberately conservative bar: the failure mode of a low bar is
	// confidently injecting the wrong playbook, which is worse than injecting
	// nothing. The top skill's real score is logged every judged turn (see
	// runDiscovery) precisely so this constant can be tuned against observed
	// numbers rather than guessed at.
	discoveryAutoInjectFloor = 0.8

	// discoveryAutoInjectMaxBytes caps the inlined body. Skills in this repo run
	// from a few hundred bytes to ~20KB (SKILL.md is documentation, not a
	// prompt), so an uncapped inline is a per-request cost that can swamp the
	// actual task. The head is kept: frontmatter plus the workflow/checklist
	// sections, which is what makes the model follow the procedure. The rest
	// stays one skill-tool call away, and the block says so.
	discoveryAutoInjectMaxBytes = 8 << 10
)

// autoInjectMarker labels the block so the model reads it as system-origin (the
// same convention as promptDiscoveryMarker) and so chatAlreadyHasSkill can
// recognise a previous injection by name.
const autoInjectMarker = "auto-loaded skill"

// autoInjectSkill is one selected skill, ready to render.
type autoInjectSkill struct {
	Name    string
	Source  string
	Content string
	// Noul is the judge score that selected it, surfaced in the block so the
	// model can weigh the instruction it is being handed.
	Noul float64
	// Truncated records that Content is a capped head, not the whole SKILL.md.
	Truncated bool
}

// pickAutoInjectSkill returns the highest-scoring SKILL candidate whose noul
// meets discoveryAutoInjectFloor, and the score that chose it.
//
// Only Kind == "skill" qualifies: an MCP tool or a project markdown summary has
// no body to inline (the MCP gate is a separate mechanism, and md docs are
// already emitted as summaries by renderAttachedMarkdown).
//
// keep arrives in embedder-rank order (Session.Select preserves it), so equal
// scores resolve to the higher-ranked candidate by taking the first — a
// deterministic tie-break rather than map iteration order.
//
// Absent from scores means "unknown" (judge unavailable, errored, or the answer
// was missing/not a noul) and never means 0.0, so a nil map selects nothing.
func pickAutoInjectSkill(keep []discovery.Doc, scores map[string]float64) (discovery.Doc, float64, bool) {
	var best discovery.Doc
	bestNoul := 0.0
	found := false
	for _, d := range keep {
		if d.Kind != "skill" {
			continue
		}
		noul, ok := scores[d.ID]
		if !ok || noul < discoveryAutoInjectFloor {
			continue
		}
		if !found || noul > bestNoul {
			best, bestNoul, found = d, noul, true
		}
	}
	return best, bestNoul, found
}

// truncateSkillBody caps content at discoveryAutoInjectMaxBytes, keeping the
// head and cutting on a line boundary so the block never ends mid-sentence or
// with a dangling partial markdown construct. Content at or under the cap is
// returned verbatim.
func truncateSkillBody(content string) (string, bool) {
	if len(content) <= discoveryAutoInjectMaxBytes {
		return content, false
	}
	head := content[:discoveryAutoInjectMaxBytes]
	// Back up to the last newline so the kept head is whole lines. If the cap
	// lands before any newline at all, keep the raw head rather than returning
	// nothing.
	if i := strings.LastIndexByte(head, '\n'); i > 0 {
		head = head[:i+1]
	}
	return head, true
}

// loadAutoInjectBody reads the skill named name from the SESSION's project root
// and returns the capped body, or nil when the skill cannot be read.
//
// It deliberately does not go through tool.SkillTool: that resolves the project
// root from os.Getwd(), which is the server process's cwd and therefore the wrong
// root for any session not rooted at the process cwd.
func (a *Agent) loadAutoInjectBody(name string) *autoInjectSkill {
	s, err := skill.LoadSkillForRoot(name, a.workDir)
	if err != nil || s == nil {
		return nil
	}
	content, truncated := truncateSkillBody(s.Content)
	return &autoInjectSkill{
		Name:      s.Name,
		Source:    s.Source,
		Content:   content,
		Truncated: truncated,
	}
}

// chatAlreadyHasSkill reports whether the transcript already carries this
// skill's content, so it is never injected a second time. Three independent
// signals, because each covers a case the others miss:
//
//  1. a prior skill/load_skill tool call naming it (the model loaded it);
//  2. the skill's Source path appearing anywhere in the transcript (the model
//     read the SKILL.md directly, e.g. via grep or read);
//  3. a previous auto-inject block naming it (see autoInjectMarker).
//
// This scan walks the WHOLE transcript, not just the turn, because the point is
// "does the model already have this anywhere in context".
func chatAlreadyHasSkill(messages []Message, name, source string) bool {
	if name == "" {
		return false
	}
	for _, m := range messages {
		for _, tc := range m.ToolCalls {
			if tc.Function.Name != "skill" && tc.Function.Name != "load_skill" {
				continue
			}
			args := tc.Function.Arguments
			// Cheap pre-filter before parsing: the name must appear in the raw
			// JSON at all. Substring-matching the name directly would make
			// "pdf" match a load_skill("pdf-forms") call.
			if !strings.Contains(args, name) {
				continue
			}
			var p struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal([]byte(args), &p); err != nil {
				continue
			}
			if p.Name == name {
				return true
			}
		}
		if source != "" && strings.Contains(m.Content, source) {
			return true
		}
		if strings.Contains(m.Content, autoInjectMarker) && strings.Contains(m.Content, name) {
			return true
		}
	}
	return false
}

// renderAutoInjectBlock renders the user-role block. It is user-role, never
// system-role: collectAndRemoveSystemMessages hoists EVERY system message into
// the cached `system` field, so a per-turn-varying injection there would
// invalidate the whole cached prefix (see injectDiscoveryContext).
func renderAutoInjectBlock(s *autoInjectSkill) string {
	if s == nil || s.Content == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %q (Jev relevance %.2f) — this skill is almost certainly the one you need for this request. Follow it; you do not need to load it again.\n",
		promptDiscoveryMarker, autoInjectMarker, s.Name, s.Noul)
	if s.Truncated {
		b.WriteString("(body truncated — call the skill tool for the rest)\n")
	}
	b.WriteString("\n")
	b.WriteString(s.Content)
	if s.Source != "" {
		b.WriteString("\nFile: ")
		b.WriteString(s.Source)
	}
	b.WriteString("\n")
	return b.String()
}

// maybeAutoInjectSkill runs the auto-inject decision for one judged turn. It
// logs the top skill's real score EVERY judged turn — including below the floor
// — so discoveryAutoInjectFloor can be tuned against observed numbers instead of
// guessed at, and so "the feature is dead" is distinguishable from "the judge
// never scored anything high this session".
func (a *Agent) maybeAutoInjectSkill(keep []discovery.Doc, scores map[string]float64, messages []Message) {
	if len(scores) == 0 {
		return // no judge this turn: fail-closed, nothing to log or inject
	}
	doc, noul, ok := pickAutoInjectSkill(keep, scores)
	if !ok {
		a.logAutoInjectMiss(keep, scores)
		return
	}
	sel := a.loadAutoInjectBody(doc.Name)
	if sel == nil {
		a.emitDebug("DISCOVERY", fmt.Sprintf("auto-inject: skill %q scored %.2f but its SKILL.md could not be read from the session root; not injecting", doc.Name, noul))
		return
	}
	sel.Noul = noul
	if !a.recordAutoInject(sel, messages) {
		a.emitDebug("DISCOVERY", fmt.Sprintf("auto-inject: skill %q scored %.2f but skipped (already injected this session, or the chat already has it)", doc.Name, noul))
		return
	}
	a.emitDebug("DISCOVERY", fmt.Sprintf("auto-inject: skill %q scored %.2f (floor %.2f) — body inlined (%d bytes, truncated=%v)",
		sel.Name, noul, discoveryAutoInjectFloor, len(sel.Content), sel.Truncated))
}

// logAutoInjectMiss reports the best SKILL score seen this turn and why it did
// not fire, so a session where nothing is ever injected is diagnosable.
func (a *Agent) logAutoInjectMiss(keep []discovery.Doc, scores map[string]float64) {
	bestName, best := "", -1.0
	for _, d := range keep {
		if d.Kind != "skill" {
			continue
		}
		if noul, ok := scores[d.ID]; ok && noul > best {
			bestName, best = d.Name, noul
		}
	}
	if bestName == "" {
		return // no scored skill candidate at all
	}
	a.emitDebug("DISCOVERY", fmt.Sprintf("auto-inject: no skill qualified (best %q noul=%.3f < floor %.2f)", bestName, best, discoveryAutoInjectFloor))
}

// autoInjectBlock renders every staged auto-inject block, oldest first, or ""
// when there is none. One message carries all of them so the tail stays a single
// volatile append (the cached prefix above it is untouched either way).
//
// Read under the mutex because a status read can run on another goroutine.
func (a *Agent) autoInjectBlock() string {
	if a.disco == nil {
		return ""
	}
	a.disco.autoInjectMu.Lock()
	sel := a.disco.autoInject
	a.disco.autoInjectMu.Unlock()
	var b strings.Builder
	for _, s := range sel {
		if block := renderAutoInjectBlock(s); block != "" {
			b.WriteString(block)
		}
	}
	return b.String()
}

// recordAutoInject stages sel for emission unless the chat already has it or this
// session already auto-injected that skill. It reports whether the skill was
// staged.
//
// messages is the live transcript; it is the same slice the model sees, so the
// "already in the chat" check is against real content rather than a guess.
func (a *Agent) recordAutoInject(sel *autoInjectSkill, messages []Message) bool {
	if sel == nil || a.disco == nil || sel.Name == "" {
		return false
	}
	if chatAlreadyHasSkill(messages, sel.Name, sel.Source) {
		return false
	}
	a.disco.autoInjectMu.Lock()
	defer a.disco.autoInjectMu.Unlock()
	if a.disco.autoInjected == nil {
		a.disco.autoInjected = make(map[string]bool)
	}
	if a.disco.autoInjected[sel.Name] {
		return false
	}
	a.disco.autoInjected[sel.Name] = true
	// Appended, never assigned: an earlier staged skill keeps its block (see the
	// autoInject field comment). The sticky check above is what keeps a name out
	// of this slice twice, so no name-based dedupe is needed here.
	a.disco.autoInject = append(a.disco.autoInject, sel)
	return true
}

// resetAutoInjected forgets every auto-injected skill. Called at the compaction
// splice for the same reason resetDirMDSeen is: the injected blocks were never
// persisted, so after the splice the model genuinely no longer has them, and the
// next judged turn must be able to re-select.
func (a *Agent) resetAutoInjected() {
	if a.disco == nil {
		return
	}
	a.disco.autoInjectMu.Lock()
	defer a.disco.autoInjectMu.Unlock()
	a.disco.autoInjected = nil
	a.disco.autoInject = nil
}
