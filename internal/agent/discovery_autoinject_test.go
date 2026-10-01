package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
	"github.com/u007/ocode/internal/skill"
)

// autoInjectDocs mirrors discoveryJudgeCandidates' shape: two skills plus a
// non-skill, in embedder-rank order (the order Select returns them in).
func autoInjectDocs() []discovery.Doc {
	return []discovery.Doc{
		{ID: "skill:low", Kind: "skill", Name: "low", Text: "low skill"},
		{ID: "skill:best", Kind: "skill", Name: "best", Text: "best skill"},
		{ID: "mcp:send", Kind: "mcp", Name: "send", Text: "send mail"},
	}
}

// loadSkillCall builds a `load_skill` tool call naming argName. ToolCall.Function
// is an anonymous struct, so it cannot be written as a composite literal
// inline; this helper keeps the call sites readable.
func loadSkillCall(argName string) ToolCall {
	var tc ToolCall
	tc.Function.Name = "load_skill"
	tc.Function.Arguments = `{"name":"` + argName + `"}`
	return tc
}

func newAutoInjectAgent(t *testing.T) *Agent {
	t.Helper()
	a := newGateAgent()
	a.config = &config.Config{}
	a.config.Ocode.Discovery.Enabled = true
	a.disco = &discoveryState{enabled: true,
		session: discovery.NewSession(discovery.NewEngine(discovery.FakeEmbedder{Dimension: 8}, t.TempDir()))}
	return a
}

func TestPickAutoInjectSkillTakesHighestScoringSkill(t *testing.T) {
	doc, noul, ok := pickAutoInjectSkill(autoInjectDocs(), map[string]float64{
		"skill:low":  0.82,
		"skill:best": 0.93,
		"mcp:send":   0.99, // highest overall, but not a skill
	})
	if !ok {
		t.Fatal("expected a skill to be selected")
	}
	if doc.ID != "skill:best" {
		t.Fatalf("selected %s, want skill:best (highest-scoring SKILL, not highest overall)", doc.ID)
	}
	if noul != 0.93 {
		t.Fatalf("noul = %v, want 0.93", noul)
	}
}

func TestPickAutoInjectSkillSkipsBelowFloor(t *testing.T) {
	// LITERALS, never discoveryAutoInjectFloor +/- epsilon. Deriving the
	// expectation from the constant under test makes the test move WITH any
	// mutation of it, which is exactly how a floor change from 0.8 down to the
	// lenient relevance 0.5 slipped past the first version of this test. The
	// constant's own value and its decoupling are pinned separately, below.
	if _, _, ok := pickAutoInjectSkill(autoInjectDocs(), map[string]float64{"skill:low": 0.79}); ok {
		t.Fatal("0.79 must not auto-inject")
	}
	if _, _, ok := pickAutoInjectSkill(autoInjectDocs(), map[string]float64{"skill:low": 0.8}); !ok {
		t.Fatal("0.80 (exactly the floor) must auto-inject")
	}
	// Merely-somewhat-relevant is the value discovery deliberately KEEPS for
	// name display. It must never be enough to justify inlining a body.
	if _, _, ok := pickAutoInjectSkill(autoInjectDocs(), map[string]float64{"skill:low": 0.5}); ok {
		t.Fatal("a merely-somewhat-relevant 0.5 must not auto-inject a full body")
	}
}

// The floor is a deliberate policy value, and its position relative to the two
// floors that already exist is the invariant that stops the three judges from
// drifting into one another.
func TestAutoInjectFloorIsDeliberatelyDecoupled(t *testing.T) {
	if discoveryAutoInjectFloor != 0.8 {
		t.Fatalf("auto-inject floor = %v, want the agreed 0.8", discoveryAutoInjectFloor)
	}
	if discoveryAutoInjectFloor <= relevanceJudgeMinConfidenceDefault {
		t.Fatalf("floor %v must stay ABOVE the lenient relevance floor %v, or name-display leniency silently starts inlining bodies",
			discoveryAutoInjectFloor, relevanceJudgeMinConfidenceDefault)
	}
	if discoveryAutoInjectFloor == autoJudgeMinConfidenceDefault {
		t.Fatal("the floor must be its own constant, not the permission floor")
	}
}

func TestPickAutoInjectSkillIgnoresNonSkillKinds(t *testing.T) {
	if _, _, ok := pickAutoInjectSkill(autoInjectDocs(), map[string]float64{"mcp:send": 0.99}); ok {
		t.Fatal("an mcp doc must never be auto-injected as a skill body")
	}
	if _, _, ok := pickAutoInjectSkill([]discovery.Doc{{ID: "md:c", Kind: "md", Name: "c"}}, map[string]float64{"md:c": 0.99}); ok {
		t.Fatal("a project-doc md candidate must never be auto-injected")
	}
}

func TestPickAutoInjectSkillTieBreaksByRankOrder(t *testing.T) {
	// Equal scores: the earlier embedder rank wins (candidates arrive in rank
	// order from Select, so first-wins is the deterministic tie-break).
	doc, _, ok := pickAutoInjectSkill(autoInjectDocs(), map[string]float64{
		"skill:low":  0.9,
		"skill:best": 0.9,
	})
	if !ok || doc.ID != "skill:low" {
		t.Fatalf("tie must resolve to the higher-ranked candidate, got %s ok=%v", doc.ID, ok)
	}
}

func TestPickAutoInjectSkillNoScoresMeansNoInjection(t *testing.T) {
	// A judge error, a disconnected TypeSafe, or a missing/non-noul answer all
	// leave no score. Auto-injecting is fail-CLOSED: a spent 8KB prompt budget
	// must never be triggered by the judge being unavailable.
	if _, _, ok := pickAutoInjectSkill(autoInjectDocs(), nil); ok {
		t.Fatal("no scores must mean no auto-inject")
	}
	if _, _, ok := pickAutoInjectSkill(autoInjectDocs(), map[string]float64{}); ok {
		t.Fatal("empty scores must mean no auto-inject")
	}
}

func TestPickAutoInjectSkillEmptyCandidates(t *testing.T) {
	if _, _, ok := pickAutoInjectSkill(nil, map[string]float64{"skill:x": 0.99}); ok {
		t.Fatal("no candidates must mean no auto-inject")
	}
}

func TestTruncateSkillBodyKeepsHeadOnLineBoundary(t *testing.T) {
	var b strings.Builder
	for b.Len() < discoveryAutoInjectMaxBytes*2 {
		b.WriteString("line of the skill body\n")
	}
	body := b.String()

	got, truncated := truncateSkillBody(body)
	if !truncated {
		t.Fatal("an oversized body must report truncated")
	}
	if len(got) > discoveryAutoInjectMaxBytes {
		t.Fatalf("kept %d bytes, over the %d cap", len(got), discoveryAutoInjectMaxBytes)
	}
	if !strings.HasPrefix(body, got) {
		t.Fatal("truncation must keep the HEAD of the body, not the tail")
	}
	// Cut on a line boundary: never leave a half-written line dangling.
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("truncated body must end on a line boundary: %q", got[len(got)-40:])
	}
}

func TestTruncateSkillBodyShortContentUntouched(t *testing.T) {
	body := "# Short skill\n\nJust a few lines.\n"
	got, truncated := truncateSkillBody(body)
	if truncated || got != body {
		t.Fatalf("short body must pass through verbatim: truncated=%v got %q", truncated, got)
	}
}

func TestChatAlreadyHasSkillDetectsLoadSkillCall(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "do the thing"},
		{Role: "assistant", ToolCalls: []ToolCall{loadSkillCall("pdf")}},
	}
	if !chatAlreadyHasSkill(msgs, "pdf", "/somewhere/pdf/SKILL.md") {
		t.Fatal("a prior load_skill call for the name means the chat already has it")
	}
	if chatAlreadyHasSkill(msgs, "docx", "/somewhere/docx/SKILL.md") {
		t.Fatal("a different skill name must not count as present")
	}
}

func TestChatAlreadyHasSkillDetectsSourcePathInTranscript(t *testing.T) {
	// The model read the SKILL.md itself (a read tool result), so the content is
	// already in the chat even though no load_skill call happened.
	msgs := []Message{
		{Role: "user", Content: "read it"},
		{Role: "tool", Content: "package main // /somewhere/pdf/SKILL.md contents here"},
	}
	if !chatAlreadyHasSkill(msgs, "pdf", "/somewhere/pdf/SKILL.md") {
		t.Fatal("a transcript hit on the skill's Source path means the chat already has it")
	}
}

func TestChatAlreadyHasSkillDetectsPriorAutoInjectMarker(t *testing.T) {
	// Re-injecting the same block would be pure duplicate tokens.
	msgs := []Message{{Role: "user", Content: renderAutoInjectBlock(&autoInjectSkill{
		Name: "pdf", Source: "/other/pdf/SKILL.md", Content: "body", Noul: 0.9,
	})}}
	if !chatAlreadyHasSkill(msgs, "pdf", "/other/pdf/SKILL.md") {
		t.Fatal("a prior auto-inject marker for the skill must count as already present")
	}
}

func TestChatAlreadyHasSkillDoesNotMatchSkillNameAsSubstring(t *testing.T) {
	// load_skill arguments are JSON, so the name is compared as a parsed field:
	// an unrelated skill whose name merely CONTAINS the target is not a hit.
	msgs := []Message{{Role: "assistant", ToolCalls: []ToolCall{loadSkillCall("pdf-forms")}}}
	if chatAlreadyHasSkill(msgs, "pdf", "/x/pdf/SKILL.md") {
		t.Fatal("pdf-forms must not count as pdf")
	}
}

func TestChatAlreadyHasSkillFalseOnUnrelatedChat(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	if chatAlreadyHasSkill(msgs, "pdf", "/x/pdf/SKILL.md") {
		t.Fatal("an unrelated chat must not report the skill as present")
	}
}

func TestInjectDiscoveryContextEmitsAutoInjectBlock(t *testing.T) {
	a := newAutoInjectAgent(t)
	base := []Message{{Role: "user", Content: "hi"}}
	a.disco.autoInject = &autoInjectSkill{
		Name: "pdf", Source: "/x/pdf/SKILL.md", Content: "PDF WORKFLOW BODY", Noul: 0.93,
	}

	got := a.injectDiscoveryContext(base)
	last := got[len(got)-1]
	if last.Role != "user" {
		t.Fatalf("auto-inject block must be user-role so it rides the uncached tail, got %q", last.Role)
	}
	if !strings.HasPrefix(last.Content, promptDiscoveryMarker) {
		t.Fatalf("block must carry the discovery marker: %q", last.Content)
	}
	for _, want := range []string{"pdf", "PDF WORKFLOW BODY", "/x/pdf/SKILL.md", "0.93"} {
		if !strings.Contains(last.Content, want) {
			t.Fatalf("block missing %q: %q", want, last.Content)
		}
	}
}

// The cached system block is the load-bearing prompt-cache invariant: a system
// message is hoisted into the cached `system` field, so a per-turn-varying
// injection there would bust the whole prefix. The auto-inject block must
// therefore not perturb sysContent at all.
func TestInjectDiscoveryContextAutoInjectKeepsSystemBlockByteIdentical(t *testing.T) {
	a := newAutoInjectAgent(t)
	base := []Message{{Role: "user", Content: "hi"}}

	without := a.injectDiscoveryContext(base)
	a.disco.autoInject = &autoInjectSkill{
		Name: "pdf", Source: "/x/pdf/SKILL.md", Content: "PDF WORKFLOW BODY", Noul: 0.93,
	}
	with := a.injectDiscoveryContext(base)

	sysBefore := systemBlockOf(t, without)
	sysAfter := systemBlockOf(t, with)
	if sysBefore != sysAfter {
		t.Fatalf("system block changed when the auto-inject fired — this busts the prompt cache.\nbefore: %q\nafter:  %q", sysBefore, sysAfter)
	}
	if len(with) != len(without)+1 {
		t.Fatalf("auto-inject must append exactly one message, got %d vs %d", len(with), len(without))
	}
}

func systemBlockOf(t *testing.T, msgs []Message) string {
	t.Helper()
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == "system" {
			b.WriteString(m.Content)
			b.WriteString("\x00")
		}
	}
	return b.String()
}

func TestInjectDiscoveryContextNoBlockWhenNothingSelected(t *testing.T) {
	a := newAutoInjectAgent(t)
	base := []Message{{Role: "user", Content: "hi"}}
	got := a.injectDiscoveryContext(base)
	for _, m := range got {
		if strings.Contains(m.Content, "auto-loaded skill") {
			t.Fatalf("no skill selected, so no auto-inject block may be emitted: %q", m.Content)
		}
	}
}

func TestRecordAutoInjectMarksStickyAndDedupeReplays(t *testing.T) {
	a := newAutoInjectAgent(t)
	doc, noul, ok := pickAutoInjectSkill(autoInjectDocs(), map[string]float64{"skill:best": 0.93})
	if !ok {
		t.Fatal("setup: expected a selection")
	}
	sel := &autoInjectSkill{Name: doc.Name, Source: "/x/best/SKILL.md", Content: "body", Noul: noul}

	// First selection is recorded and emitted.
	if a.recordAutoInject(sel, []Message{{Role: "user", Content: "hi"}}) {
		if a.disco.autoInject == nil || a.disco.autoInject.Name != "best" {
			t.Fatalf("selected skill not staged for emission: %+v", a.disco.autoInject)
		}
	} else {
		t.Fatal("first selection should be emitted")
	}

	// A replay must be refused: the sticky set already has it.
	if a.recordAutoInject(sel, []Message{{Role: "user", Content: "hi"}}) {
		t.Fatal("a second selection of the same skill must be refused")
	}
	if !a.disco.autoInjected["best"] {
		t.Fatal("the sticky set must remember the skill")
	}
}

func TestRecordAutoInjectRefusedWhenChatAlreadyHasSkill(t *testing.T) {
	a := newAutoInjectAgent(t)
	sel := &autoInjectSkill{Name: "best", Source: "/x/best/SKILL.md", Content: "body", Noul: 0.93}
	msgs := []Message{{Role: "assistant", ToolCalls: []ToolCall{loadSkillCall("best")}}}
	if a.recordAutoInject(sel, msgs) {
		t.Fatal("a skill the chat already loaded must not be injected again")
	}
	if a.disco.autoInject != nil {
		t.Fatal("nothing may be staged for emission")
	}
}

func TestResetAutoInjectedClearsStickyAndStaged(t *testing.T) {
	a := newAutoInjectAgent(t)
	sel := &autoInjectSkill{Name: "best", Source: "/x/best/SKILL.md", Content: "body", Noul: 0.93}
	if !a.recordAutoInject(sel, []Message{{Role: "user", Content: "hi"}}) {
		t.Fatal("setup: expected the first selection to be emitted")
	}

	a.resetAutoInjected()

	if len(a.disco.autoInjected) != 0 {
		t.Fatalf("sticky set must be cleared by compaction, got %v", a.disco.autoInjected)
	}
	if a.disco.autoInject != nil {
		t.Fatal("a compacted-away skill must not keep being emitted")
	}
	// And it can be selected again, because after the splice the model no
	// longer has the body.
	if !a.recordAutoInject(sel, []Message{{Role: "user", Content: "summary only"}}) {
		t.Fatal("after a reset the skill must be selectable again")
	}
}

func TestLoadAutoInjectBodyUsesWorkDirNotProcessCwd(t *testing.T) {
	// Regression for the root-aware lookup: LoadSkill resolves the project root
	// from os.Getwd(), which is the SERVER process's cwd, not the session's
	// project root. Auto-injection must read the skill from the session's root.
	root := t.TempDir()
	dir := filepath.Join(root, ".opencode", "skills", "sessiononly")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "SESSION ROOT ONLY SKILL BODY"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: sessiononly\ndescription: d\n---\n"+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newGateAgent()
	a.config = &config.Config{}
	a.workDir = root
	skill.InvalidateSkillCache()
	t.Cleanup(skill.InvalidateSkillCache)

	got := a.loadAutoInjectBody("sessiononly")
	if got == nil {
		t.Fatal("expected the skill to load from the session's workDir")
	}
	if !strings.Contains(got.Content, body) {
		t.Fatalf("loaded body does not contain the project-local content: %q", got.Content)
	}
	if !strings.Contains(got.Source, filepath.Join(".opencode", "skills", "sessiononly")) {
		t.Fatalf("Source should point at the project-local SKILL.md, got %q", got.Source)
	}
}

func TestLoadAutoInjectBodyCapsAndFlagsTruncation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".opencode", "skills", "bigskill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", discoveryAutoInjectMaxBytes*2)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: bigskill\ndescription: d\n---\n"+big+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newGateAgent()
	a.config = &config.Config{}
	a.workDir = root
	skill.InvalidateSkillCache()
	t.Cleanup(skill.InvalidateSkillCache)

	got := a.loadAutoInjectBody("bigskill")
	if got == nil {
		t.Fatal("expected the skill to load")
	}
	if !got.Truncated {
		t.Fatal("an oversized skill must be flagged truncated")
	}
	if len(got.Content) > discoveryAutoInjectMaxBytes {
		t.Fatalf("content is %d bytes, over the %d cap", len(got.Content), discoveryAutoInjectMaxBytes)
	}
}

func TestLoadAutoInjectBodyMissingSkillIsNil(t *testing.T) {
	a := newGateAgent()
	a.config = &config.Config{}
	a.workDir = t.TempDir()
	skill.InvalidateSkillCache()
	t.Cleanup(skill.InvalidateSkillCache)

	if got := a.loadAutoInjectBody("definitely-not-a-real-skill-xyz"); got != nil {
		t.Fatalf("a missing skill must yield nil, got %+v", got)
	}
}
