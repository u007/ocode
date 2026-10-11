package agent

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// clefLegalID is Cloudflare's published constraint on a clef question id: only
// letters, digits, underscore, dot and hyphen, up to 100 characters. `~` is
// deliberately absent, which is what makes a tilde-based collision suffix a bug
// rather than a style choice.
var clefLegalID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

func assertLegalClefIDs(t *testing.T, p clefQuestionPlan) {
	t.Helper()
	for orig, wire := range p.wire {
		if !clefLegalID.MatchString(wire) {
			t.Errorf("wire id %q (from %q) is not a legal clef id", wire, orig)
		}
		if p.reverse[wire] != orig {
			t.Errorf("reverse[%q] = %q, want %q", wire, p.reverse[wire], orig)
		}
		if _, ok := p.questions[wire]; !ok {
			t.Errorf("wire id %q has no question in the wire map", wire)
		}
	}
}

func TestClefQuestionPlan_SanitisesIllegalRunes(t *testing.T) {
	p := planClefQuestions(map[string]TypesafeQuestion{
		"mcp:github/create_issue": {Type: "choice", Instructions: "keep?"},
		"internal/agent/clef.go":  {Type: "choice", Instructions: "keep?"},
		"skill:web-search":        {Type: "noul", Instructions: "relevant?"},
	})
	assertLegalClefIDs(t, p)
	if p.dropped != 0 {
		t.Errorf("dropped = %d, want 0", p.dropped)
	}
	// The three originals must survive intact as the reverse-map targets.
	got := map[string]bool{}
	for _, orig := range p.reverse {
		got[orig] = true
	}
	for _, want := range []string{"mcp:github/create_issue", "internal/agent/clef.go", "skill:web-search"} {
		if !got[want] {
			t.Errorf("original %q lost from the reverse map; got %v", want, got)
		}
	}
}

func TestClefQuestionPlan_IllegalRuneCollisionStaysDistinct(t *testing.T) {
	// Both sanitise to "a_b.go", which is exactly the collision the guard exists
	// for. A distinctness-only implementation would pass; only checking that the
	// two wire ids differ catches the bug where the suffix is illegal or the
	// second silently overwrites the first.
	p := planClefQuestions(map[string]TypesafeQuestion{
		"a/b.go": {Type: "choice", Instructions: "keep?"},
		"a_b.go": {Type: "choice", Instructions: "keep?"},
	})
	assertLegalClefIDs(t, p)
	if len(p.wire) != 2 || len(p.reverse) != 2 {
		t.Fatalf("got %d wire ids and %d reverse entries, want 2 and 2: wire=%v reverse=%v", len(p.wire), len(p.reverse), p.wire, p.reverse)
	}
	if p.wire["a/b.go"] == p.wire["a_b.go"] {
		t.Fatalf("both originals mapped to the same wire id %q", p.wire["a/b.go"])
	}
}

func TestClefQuestionPlan_LongPrefixCollisionStaysDistinctAndLegal(t *testing.T) {
	// The differing part sits past position 100, so both ids truncate to the same
	// 100-character prefix and only the collision suffix can separate them. The
	// suffix therefore has to be built by SHORTENING the id to make room — naively
	// appending to an already-100-char id produces an illegal 102-char id, which
	// is the trap this test exists to catch.
	prefix := strings.Repeat("a", 100)
	p := planClefQuestions(map[string]TypesafeQuestion{
		prefix + "/x": {Type: "choice", Instructions: "keep?"},
		prefix + "/y": {Type: "choice", Instructions: "keep?"},
	})
	assertLegalClefIDs(t, p)
	if p.wire[prefix+"/x"] == p.wire[prefix+"/y"] {
		t.Fatalf("long-prefix collision collapsed to %q", p.wire[prefix+"/x"])
	}
}

func TestClefQuestionPlan_SuffixLeavesRoomWithinTheLimit(t *testing.T) {
	// Directly pins the length arithmetic: a full-length colliding id must yield a
	// legal id, i.e. the suffix is paid for out of the truncated prefix.
	prefix := strings.Repeat("b", 100)
	p := planClefQuestions(map[string]TypesafeQuestion{
		prefix + "/x": {Type: "noul"},
		prefix + "/y": {Type: "noul"},
	})
	for orig, wire := range p.wire {
		if len(wire) > 100 {
			t.Errorf("wire id for %q is %d chars, over the 100 limit: %q", orig, len(wire), wire)
		}
	}
}

func TestClefQuestionPlan_IsDeterministic(t *testing.T) {
	// Go randomises map iteration order, so building the mapping by ranging over
	// the caller's map would let iteration order decide which of several
	// COLLIDING originals wins the bare sanitised id and which gets a suffix.
	// Determinism is what lets a judge result be cached, replayed or compared.
	//
	// The ids below are chosen so they genuinely collide once sanitised:
	// "a/0", "a_0" and "a.0" all become "a_0". With a collision-free fixture this
	// test would still pass with the sort removed, because the mapping would be
	// unique either way and order could not possibly matter.
	in := map[string]TypesafeQuestion{}
	for i := 0; i < 9; i++ {
		in["a/"+strconv.Itoa(i%3)] = TypesafeQuestion{Type: "noul"}
		in["a_"+strconv.Itoa(i%3)] = TypesafeQuestion{Type: "noul"}
		in["a."+strconv.Itoa(i%3)] = TypesafeQuestion{Type: "noul"}
	}
	first := planClefQuestions(in)

	// Guard the fixture itself. If these ids ever stop colliding the whole
	// determinism assertion below becomes vacuous, so fail loudly instead.
	if first.wire["a/0"] == first.wire["a_0"] || first.wire["a_0"] == first.wire["a.0"] {
		t.Fatalf("fixture does not collide: a/0=%q a_0=%q a.0=%q", first.wire["a/0"], first.wire["a_0"], first.wire["a.0"])
	}

	for i := 0; i < 40; i++ {
		again := planClefQuestions(in)
		if len(again.wire) != len(first.wire) {
			t.Fatalf("iteration %d produced %d wire ids, want %d", i, len(again.wire), len(first.wire))
		}
		for orig, wire := range first.wire {
			if again.wire[orig] != wire {
				t.Fatalf("iteration %d: original %q mapped to %q, want %q (map iteration order leaked in)", i, orig, again.wire[orig], wire)
			}
		}
	}
}

func TestClefQuestionPlan_CapsAtSixtyFourAndReportsDropped(t *testing.T) {
	in := map[string]TypesafeQuestion{}
	for i := 0; i < clefMaxQuestions+17; i++ {
		in["cand/"+strconv.Itoa(i)] = TypesafeQuestion{Type: "noul"}
	}
	p := planClefQuestions(in)
	if len(p.questions) != clefMaxQuestions {
		t.Errorf("sent %d questions, want the ceiling of %d", len(p.questions), clefMaxQuestions)
	}
	if p.dropped != 17 {
		t.Errorf("dropped = %d, want 17", p.dropped)
	}
	assertLegalClefIDs(t, p)
}

func TestClefQuestionPlan_CeilingTrimsEveryMapTogether(t *testing.T) {
	// The three structures must agree. A reverse entry surviving for a dropped
	// question is the dangerous half: a late answer could then be attributed to a
	// candidate that was never sent, which is a verdict nobody asked for.
	in := map[string]TypesafeQuestion{}
	for i := 0; i < clefMaxQuestions+5; i++ {
		in["cand/"+strconv.Itoa(i)] = TypesafeQuestion{Type: "noul"}
	}
	p := planClefQuestions(in)
	if len(p.wire) != clefMaxQuestions || len(p.reverse) != clefMaxQuestions || len(p.questions) != clefMaxQuestions {
		t.Fatalf("trimmed inconsistently: wire=%d reverse=%d questions=%d", len(p.wire), len(p.reverse), len(p.questions))
	}
	for wire := range p.questions {
		if _, ok := p.reverse[wire]; !ok {
			t.Errorf("sent wire id %q has no reverse entry", wire)
		}
		if _, ok := p.wire[p.reverse[wire]]; !ok {
			t.Errorf("reverse entry %q has no forward wire id", wire)
		}
	}
	// Determinism of WHICH candidates survive: the same input must trim to the
	// same 64 every time, otherwise two runs disagree about what was judged.
	for i := 0; i < 10; i++ {
		again := planClefQuestions(in)
		for wire := range p.questions {
			if _, ok := again.questions[wire]; !ok {
				t.Fatalf("iteration %d dropped %q that the first run kept", i, wire)
			}
		}
	}
}

func TestClefDemux_ReKeysAnswersOntoCallerIDs(t *testing.T) {
	p := planClefQuestions(map[string]TypesafeQuestion{
		"a/b.go": {Type: "noul"},
		"a_b.go": {Type: "noul"},
	})
	wireA := p.wire["a/b.go"]
	wireB := p.wire["a_b.go"]
	resp := &TypesafeResponse{Answers: map[string]TypesafeAnswer{
		wireA: {Type: "noul", Noul: 0.9, Confidence: 0.8},
		wireB: {Type: "noul", Noul: 0.1, Confidence: 0.8},
	}}
	out := p.demux(resp)
	if out == nil {
		t.Fatal("demux returned nil")
	}
	// Each original must receive ITS OWN answer, not the other's. Swapping them is
	// the exact silent wrong-veto this whole part exists to prevent.
	if got := out.Answers["a/b.go"].Noul; got != 0.9 {
		t.Errorf(`Answers["a/b.go"].Noul = %v, want 0.9 (answers swapped or mis-attributed)`, got)
	}
	if got := out.Answers["a_b.go"].Noul; got != 0.1 {
		t.Errorf(`Answers["a_b.go"].Noul = %v, want 0.1 (answers swapped or mis-attributed)`, got)
	}
	if len(out.Answers) != 2 {
		t.Errorf("got %d answers, want 2", len(out.Answers))
	}
}

func TestClefDemux_UnansweredQuestionStaysAbsent(t *testing.T) {
	// Absence must stay absence. A zero-valued TypesafeAnswer has Noul == 0, which
	// every relevance judge reads as "definitely not relevant" — so materialising a
	// zero for a missing answer would veto the candidate silently.
	p := planClefQuestions(map[string]TypesafeQuestion{
		"a/b.go": {Type: "noul"},
		"a_b.go": {Type: "noul"},
	})
	out := p.demux(&TypesafeResponse{Answers: map[string]TypesafeAnswer{
		p.wire["a/b.go"]: {Type: "noul", Noul: 0.9},
	}})
	if _, ok := out.Answers["a_b.go"]; ok {
		t.Errorf(`Answers["a_b.go"] was materialised despite no answer: %+v`, out.Answers["a_b.go"])
	}
	if _, ok := out.Answers["a/b.go"]; !ok {
		t.Error(`the answered question lost its answer`)
	}
}

func TestClefDemux_DiscardsAnswerForAnUnsentQuestion(t *testing.T) {
	// A backend answer for a wire id we never sent must not become a verdict for
	// any caller id. Otherwise a hallucinated or tampered response could inject a
	// decision about a candidate that was never put to the judge.
	p := planClefQuestions(map[string]TypesafeQuestion{
		"a/b.go": {Type: "noul"},
	})
	out := p.demux(&TypesafeResponse{Answers: map[string]TypesafeAnswer{
		p.wire["a/b.go"]:    {Type: "noul", Noul: 0.9},
		"never-sent-at-all": {Type: "noul", Noul: 0.0},
	}})
	if len(out.Answers) != 1 {
		t.Errorf("got %d answers, want 1: %v", len(out.Answers), out.Answers)
	}
	for id := range out.Answers {
		if id == "never-sent-at-all" || p.reverse[id] == "never-sent-at-all" {
			t.Errorf("an answer for an unsent question leaked through as %q", id)
		}
	}
}

func TestClefDemux_ZeroAnswersIsNotAnError(t *testing.T) {
	p := planClefQuestions(map[string]TypesafeQuestion{"a/b.go": {Type: "noul"}})
	out := p.demux(&TypesafeResponse{Answers: map[string]TypesafeAnswer{}})
	if out == nil {
		t.Fatal("demux returned nil for an empty answer set")
	}
	if len(out.Answers) != 0 {
		t.Errorf("got %d answers, want 0", len(out.Answers))
	}
}
