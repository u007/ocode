package redact

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/u007/dataprotect/analyzer"
	"github.com/u007/dataprotect/recognizers"
	"github.com/u007/ocode/internal/config"
)

// errRecognizer always fails, so Analyze returns an error.
type errRecognizer struct{}

func (errRecognizer) Name() string { return "err_recognizer" }

func (errRecognizer) Recognize(context.Context, string) ([]recognizers.Entity, error) {
	return nil, errors.New("recognizer boom")
}

// panicRecognizer panics inside Recognize.
type panicRecognizer struct{}

func (panicRecognizer) Name() string { return "panic_recognizer" }

func (panicRecognizer) Recognize(context.Context, string) ([]recognizers.Entity, error) {
	panic("recognizer panicked")
}

// spanRecognizer reports one entity covering [start,end) verbatim.
type spanRecognizer struct {
	start, end int
	typ        recognizers.EntityType
}

func (spanRecognizer) Name() string { return "span_recognizer" }

func (r spanRecognizer) Recognize(context.Context, string) ([]recognizers.Entity, error) {
	return []recognizers.Entity{{
		Type:       r.typ,
		Start:      r.start,
		End:        r.end,
		Confidence: 0.99,
	}}, nil
}

// scoreRecognizer reports one entity covering [start,end) at a fixed
// confidence, so a floor boundary can be pinned exactly.
type scoreRecognizer struct {
	start, end int
	typ        recognizers.EntityType
	confidence float64
}

func (scoreRecognizer) Name() string { return "score_recognizer" }

func (r scoreRecognizer) Recognize(context.Context, string) ([]recognizers.Entity, error) {
	return []recognizers.Entity{{
		Type:       r.typ,
		Start:      r.start,
		End:        r.end,
		Confidence: r.confidence,
	}}, nil
}

func countPIIKinds(spans []Span, kind string) int {
	n := 0
	for _, s := range spans {
		if s.Kind == kind {
			n++
		}
	}
	return n
}

func hasPIISpan(spans []Span) bool {
	for _, s := range spans {
		if strings.HasPrefix(s.Kind, "pii:") {
			return true
		}
	}
	return false
}

func spansEqual(a, b []Span) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// resetPIICache empties the process-wide memo. Tests use it so a key left by an
// earlier test (or by the baseline call in the same test) cannot satisfy a
// later lookup and hide the analyzer or stub under test.
func resetPIICache() {
	piiMemo.mu.Lock()
	piiMemo.current = make(map[string][]Span)
	piiMemo.previous = make(map[string][]Span)
	piiMemo.mu.Unlock()
}

// piiMemoSize reports the number of live memo entries across both generations.
func piiMemoSize() int {
	piiMemo.mu.Lock()
	defer piiMemo.mu.Unlock()
	return len(piiMemo.current) + len(piiMemo.previous)
}

func TestPIICategoryDetection(t *testing.T) {
	cases := []struct {
		name     string
		category PIICategory
		positive string
		wantKind string
		negative string
	}{
		{"contact", PIIContact, "jane@example.com", "pii:EMAIL", "mail jane"},
		{"financial", PIIFinancial, "4111111111111111", "pii:CREDIT_CARD", "4111111111111112"},
		{"identity", PIIIdentity, "123-45-6789", "pii:SSN", "no identifier here"},
		{"legal", PIILegal, "23-CV-0001", "pii:CASE_NUMBER", "23-XX-0001"},
		{"network", PIINetwork, "10.0.0.1", "pii:IP_ADDRESS", "version 1.2.3"},
		{"money", PIIMoney, "$1,234.56", "pii:MONEY", "1234.56"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			opts := DetectOpts{PIICategories: []PIICategory{tt.category}}

			spans, err := Detect(tt.positive, nil, opts)
			if err != nil {
				t.Fatalf("Detect(%q): %v", tt.positive, err)
			}
			if got := countPIIKinds(spans, tt.wantKind); got != 1 {
				t.Errorf("Detect(%q) = %v, want exactly one %s", tt.positive, spans, tt.wantKind)
			}

			spans, err = Detect(tt.negative, nil, opts)
			if err != nil {
				t.Fatalf("Detect(%q): %v", tt.negative, err)
			}
			if hasPIISpan(spans) {
				t.Errorf("Detect(%q) = %v, want no PII span", tt.negative, spans)
			}
		})
	}
}

func TestDetectSecretsIgnoresPIICategories(t *testing.T) {
	text := "mail jane@example.com"
	spans := DetectSecrets(text, nil, DetectOpts{PIICategories: []PIICategory{PIIContact}})
	if hasPIISpan(spans) {
		t.Fatalf("DetectSecrets returned PII spans %v", spans)
	}
}

func TestDetectSkipsTokenOverlaps(t *testing.T) {
	token := "[[OCSEC:abc123:1]]"
	text := token + " and jane@example.com"
	spans, err := Detect(text, nil, DetectOpts{PIICategories: []PIICategory{PIIContact}})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	tokenEnd := len(token)
	for _, s := range spans {
		if s.Start < tokenEnd && 0 < s.End {
			t.Errorf("span %v overlaps token %q", s, token)
		}
	}

	// Force a recognizer result over the token itself: the engine must drop it.
	// Clear the memo first so the stub is actually reached.
	resetPIICache()
	t.Cleanup(resetPIICache)
	old := piiAnalyzerFor
	t.Cleanup(func() { piiAnalyzerFor = old })
	onlyToken := "[[OCSEC:abc123:1]]"
	piiAnalyzerFor = func([]PIICategory) *analyzer.Analyzer {
		a := analyzer.New()
		a.AddRecognizer(spanRecognizer{start: 0, end: len(onlyToken), typ: recognizers.EntityEmail})
		return a
	}
	spans, err = Detect(onlyToken, nil, DetectOpts{PIICategories: []PIICategory{PIIContact}})
	if err != nil {
		t.Fatalf("Detect with stub analyzer: %v", err)
	}
	if hasPIISpan(spans) {
		t.Errorf("span over token was not skipped: %v", spans)
	}
}

func TestDetectIsIdempotentAfterRender(t *testing.T) {
	text := "mail jane@example.com card 4111111111111111"
	cats := []PIICategory{PIIContact, PIIFinancial}

	spans, err := Detect(text, nil, DetectOpts{PIICategories: cats})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasPIISpan(spans) {
		t.Fatalf("Detect found no PII in %q", text)
	}

	reg := NewRegistry("abc123")
	for _, s := range spans {
		reg.GetOrAssign(text[s.Start:s.End], s.Kind, "test")
	}
	masked := reg.Substitute(text)
	if !TokenPattern.MatchString(masked) {
		t.Fatalf("substitute produced no token: %q", masked)
	}

	again, err := Detect(masked, nil, DetectOpts{PIICategories: cats})
	if err != nil {
		t.Fatalf("Detect(masked): %v", err)
	}
	if hasPIISpan(again) {
		t.Errorf("re-detection of rendered text found PII: %v", again)
	}
}

func TestDetectMemoKeyChanges(t *testing.T) {
	resetPIICache()
	t.Cleanup(resetPIICache)
	old := piiAnalyzerFor
	t.Cleanup(func() { piiAnalyzerFor = old })

	calls := 0
	piiAnalyzerFor = func([]PIICategory) *analyzer.Analyzer {
		calls++
		return analyzer.New()
	}

	text := "memo key probe"
	contact := []PIICategory{PIIContact}
	financial := []PIICategory{PIIFinancial}

	detect := func(customWords []string, fileContent bool, cats []PIICategory) {
		t.Helper()
		if _, err := Detect(text, customWords, DetectOpts{PIICategories: cats, FileContent: fileContent}); err != nil {
			t.Fatalf("Detect: %v", err)
		}
	}

	detect(nil, false, contact)
	if calls != 1 {
		t.Fatalf("first call = %d analyzer constructions, want 1", calls)
	}
	detect(nil, false, contact)
	if calls != 1 {
		t.Errorf("memo hit constructed another analyzer: %d", calls)
	}
	detect(nil, false, financial)
	if calls != 2 {
		t.Errorf("different categories = %d, want 2", calls)
	}
	detect([]string{"widget"}, false, contact)
	if calls != 3 {
		t.Errorf("different custom words = %d, want 3", calls)
	}
	detect([]string{"widget", "gadget"}, false, contact)
	if calls != 4 {
		t.Errorf("different custom word set = %d, want 4", calls)
	}
	detect([]string{"gadget", "widget"}, false, contact)
	if calls != 4 {
		t.Errorf("canonical custom word order should hit the memo: %d", calls)
	}
	detect(nil, true, contact)
	if calls != 5 {
		t.Errorf("different FileContent = %d, want 5", calls)
	}
	detect(nil, false, []PIICategory{PIIContact, PIIFinancial})
	if calls != 6 {
		t.Errorf("new category set = %d, want 6", calls)
	}
	detect(nil, false, []PIICategory{PIIFinancial, PIIContact})
	if calls != 6 {
		t.Errorf("canonical category order should hit the memo: %d", calls)
	}
}

func TestParsePIICategories(t *testing.T) {
	cats, err := ParsePIICategories([]string{"contact", "legal"})
	if err != nil {
		t.Fatalf("ParsePIICategories: %v", err)
	}
	if len(cats) != 2 || cats[0] != PIIContact || cats[1] != PIILegal {
		t.Errorf("ParsePIICategories = %v, want [contact legal]", cats)
	}
	if _, err := ParsePIICategories([]string{"bogus"}); err == nil {
		t.Error("unknown category accepted")
	}
	if _, err := ParsePIICategories([]string{"contact", "contact"}); err == nil {
		t.Error("duplicate category accepted")
	}
}

func TestMergeSpansKeepsSecretsResolvesPIIOverlaps(t *testing.T) {
	// A secret beats a PII span that overlaps it: the secret is kept and the
	// PII span is dropped (the secret covers the same bytes).
	secret := Span{Start: 5, End: 8, Kind: "aws_key"}
	pii := Span{Start: 4, End: 12, Kind: "pii:EMAIL"}
	if got := mergeSpans([]Span{secret}, []Span{pii}); !spansEqual(got, []Span{secret}) {
		t.Errorf("secret vs PII = %v, want %v", got, []Span{secret})
	}

	// Secret-vs-secret overlaps are ALL kept: substitution masks by value, so
	// dropping one left its bytes unmasked and the value unregistered.
	short := Span{Start: 0, End: 5, Kind: "custom"}
	long := Span{Start: 0, End: 9, Kind: "env_secret"}
	if got := mergeSpans([]Span{short, long}, nil); !spansEqual(got, []Span{short, long}) {
		t.Errorf("overlapping secrets = %v, want both %v", got, []Span{short, long})
	}

	// Exact duplicates are deduped by (Start, End, Kind).
	dup := Span{Start: 1, End: 3, Kind: "custom"}
	if got := mergeSpans([]Span{dup, dup}, nil); !spansEqual(got, []Span{dup}) {
		t.Errorf("duplicate secrets = %v, want %v", got, []Span{dup})
	}

	// PII-vs-PII keeps the priority winner: longer, then earlier start.
	pShort := Span{Start: 0, End: 4, Kind: "pii:EMAIL"}
	pLong := Span{Start: 0, End: 9, Kind: "pii:PHONE"}
	if got := mergeSpans(nil, []Span{pShort, pLong}); !spansEqual(got, []Span{pLong}) {
		t.Errorf("longer PII = %v, want %v", got, []Span{pLong})
	}
	pEarly := Span{Start: 0, End: 4, Kind: "pii:EMAIL"}
	pLate := Span{Start: 2, End: 6, Kind: "pii:PHONE"}
	if got := mergeSpans(nil, []Span{pLate, pEarly}); !spansEqual(got, []Span{pEarly}) {
		t.Errorf("earlier PII = %v, want %v", got, []Span{pEarly})
	}

	// A PII span that overlaps no kept span is kept, and the result is sorted.
	a := Span{Start: 10, End: 12, Kind: "custom"}
	b := Span{Start: 0, End: 2, Kind: "pii:EMAIL"}
	if got := mergeSpans([]Span{a}, []Span{b}); !spansEqual(got, []Span{b, a}) {
		t.Errorf("non-overlap order = %v, want %v", got, []Span{b, a})
	}
}

// TestDetectZeroLengthSecretDoesNotVetoPII pins the empty-value edge case: an
// env assignment with an empty value (`PASSWORD=`) yields a zero-length secret
// span that registers nothing (Registry.GetOrAssign("") returns 0), so it must
// not veto a PII span over the same position or that PII value stays unmasked.
func TestDetectZeroLengthSecretDoesNotVetoPII(t *testing.T) {
	resetPIICache()
	t.Cleanup(resetPIICache)
	old := piiAnalyzerFor
	t.Cleanup(func() { piiAnalyzerFor = old })

	const text = "PASSWORD=\nmail jane@example.com"
	secrets := DetectSecrets(text, nil, DetectOpts{})
	if len(secrets) != 1 || secrets[0].Start != secrets[0].End {
		t.Fatalf("DetectSecrets spans = %v, want one zero-length env span", secrets)
	}
	if idx := NewRegistry(NewNonce()).GetOrAssign("", secrets[0].Kind, "test"); idx != 0 {
		t.Fatalf("GetOrAssign(empty) = %d, want 0", idx)
	}

	// The stub only supplies a PII range covering the empty value's offset; a
	// real email recognizer cannot emit a span containing an assignment.
	piiAnalyzerFor = func([]PIICategory) *analyzer.Analyzer {
		a := analyzer.New()
		a.AddRecognizer(spanRecognizer{start: 0, end: len(text), typ: recognizers.EntityEmail})
		return a
	}
	spans, err := Detect(text, nil, DetectOpts{PIICategories: []PIICategory{PIIContact}})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := []Span{
		{Start: 0, End: len(text), Kind: "pii:EMAIL"},
		{Start: secrets[0].Start, End: secrets[0].End, Kind: secrets[0].Kind},
	}
	if !spansEqual(spans, want) {
		t.Fatalf("Detect spans = %v, want %v (PII kept beside the zero-length secret)", spans, want)
	}
}

// TestDetectKeepsPartiallyOverlappingSecrets pins the registry-coverage fix:
// two custom words at [0,8) and [2,10) both come back from Detect and both are
// registered, so a later standalone occurrence of either value is masked.
// Before the fix mergeSpans dropped the loser and it stayed unmasked. A PII
// group must be enabled: with no groups Detect returns the secret spans
// directly and never reaches mergeSpans (see Detect).
func TestDetectKeepsPartiallyOverlappingSecrets(t *testing.T) {
	// Each value also appears standalone, so substitution yields one token per
	// registered value regardless of the equal-length replacement order.
	text := "abcdefghij / abcdefgh / cdefghij"
	resetPIICache()
	t.Cleanup(resetPIICache)
	spans, err := Detect(text, []string{"abcdefgh", "cdefghij"}, DetectOpts{PIICategories: []PIICategory{PIIContact}})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := []Span{
		{Start: 0, End: 8, Kind: "custom"},
		{Start: 2, End: 10, Kind: "custom"},
		{Start: 13, End: 21, Kind: "custom"},
		{Start: 24, End: 32, Kind: "custom"},
	}
	if !spansEqual(spans, want) {
		t.Fatalf("Detect spans = %v, want %v", spans, want)
	}

	reg := NewRegistry(NewNonce())
	for _, s := range spans {
		reg.GetOrAssign(text[s.Start:s.End], s.Kind, "test")
	}
	if got := len(reg.Snapshot()); got != 2 {
		t.Fatalf("registered %d values, want 2 (both overlapping custom words)", got)
	}
	masked := reg.Substitute(text)
	tokens := TokenPattern.FindAllString(masked, -1)
	distinct := make(map[string]bool, len(tokens))
	for _, tok := range tokens {
		distinct[tok] = true
	}
	if len(distinct) != 2 {
		t.Errorf("substituted text %q has %d distinct tokens, want 2", masked, len(distinct))
	}
	for _, v := range []string{"abcdefgh", "cdefghij"} {
		if strings.Contains(masked, v) {
			t.Errorf("substituted text still leaks registered value %q: %q", v, masked)
		}
	}
}

// TestDetectCustomWordsAndPIICoexist covers a value that is both a custom word
// and an email: one span, one registration, one token, and the value covered.
func TestDetectCustomWordsAndPIICoexist(t *testing.T) {
	const addr = "jane@example.com"
	text := "mail " + addr
	resetPIICache()
	t.Cleanup(resetPIICache)
	spans, err := Detect(text, []string{addr}, DetectOpts{PIICategories: []PIICategory{PIIContact}})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := []Span{{Start: 5, End: 5 + len(addr), Kind: "custom"}}
	if !spansEqual(spans, want) {
		t.Fatalf("Detect spans = %v, want only the custom span %v", spans, want)
	}

	reg := NewRegistry(NewNonce())
	for _, s := range spans {
		reg.GetOrAssign(text[s.Start:s.End], s.Kind, "test")
	}
	if got := len(reg.Snapshot()); got != 1 {
		t.Fatalf("registered %d values, want 1", got)
	}
	masked := reg.Substitute(text)
	if n := len(TokenPattern.FindAllString(masked, -1)); n != 1 {
		t.Errorf("substituted text %q has %d tokens, want 1", masked, n)
	}
	if strings.Contains(masked, addr) {
		t.Errorf("substituted text leaked the address: %q", masked)
	}
}

// TestAllPIICategoriesMatchConfig pins the closed set against the config
// validator. It lives here because internal/config cannot import this package
// (redact imports config), so the two lists can only drift silently.
func TestAllPIICategoriesMatchConfig(t *testing.T) {
	names := make([]string, 0, len(AllPIICategories))
	for _, c := range AllPIICategories {
		names = append(names, string(c))
	}
	if err := config.ValidatePIICategories(names); err != nil {
		t.Fatalf("ValidatePIICategories(%v) = %v, want nil", names, err)
	}
	if err := config.ValidatePIICategories([]string{"not_a_category"}); err == nil {
		t.Fatal("ValidatePIICategories accepted an unknown category")
	}
}

func TestDetectTotalOrder(t *testing.T) {
	text := "mail jane@example.com card 4111111111111111"
	orders := [][]PIICategory{
		{PIIContact, PIIFinancial},
		{PIIFinancial, PIIContact},
	}

	t.Cleanup(resetPIICache)
	old := piiAnalyzerFor
	t.Cleanup(func() { piiAnalyzerFor = old })
	var analyzes atomic.Int64
	piiAnalyzerFor = func(cats []PIICategory) *analyzer.Analyzer {
		analyzes.Add(1)
		return analyzerFor(cats)
	}

	var first []Span
	var firstIndexes map[string]int
	for i, order := range orders {
		// Both orders canonicalize to one memo key, so without a reset the
		// second pass would replay the first and prove nothing about order.
		resetPIICache()
		spans, err := Detect(text, nil, DetectOpts{PIICategories: order})
		if err != nil {
			t.Fatalf("Detect order %v: %v", order, err)
		}
		reg := NewRegistry("abc123")
		indexes := make(map[string]int, len(spans))
		for _, s := range spans {
			indexes[text[s.Start:s.End]] = reg.GetOrAssign(text[s.Start:s.End], s.Kind, "test")
		}
		if i == 0 {
			first, firstIndexes = spans, indexes
			continue
		}
		if !spansEqual(first, spans) {
			t.Errorf("span order differs: %v vs %v", first, spans)
		}
		for value, idx := range firstIndexes {
			if indexes[value] != idx {
				t.Errorf("registry index for %q = %d, want %d", value, indexes[value], idx)
			}
		}
	}
	// Both passes must have scanned; a memo replay would leave this at 1.
	if got := analyzes.Load(); got != int64(len(orders)) {
		t.Errorf("analyzer provider called %d times, want %d", got, len(orders))
	}
}

func TestDetectErrorPropagates(t *testing.T) {
	resetPIICache()
	t.Cleanup(resetPIICache)
	old := piiAnalyzerFor
	t.Cleanup(func() { piiAnalyzerFor = old })

	piiAnalyzerFor = func([]PIICategory) *analyzer.Analyzer {
		a := analyzer.New()
		a.AddRecognizer(errRecognizer{})
		return a
	}
	spans, err := Detect("error probe text", nil, DetectOpts{PIICategories: []PIICategory{PIIContact}})
	if err == nil {
		t.Fatalf("Detect returned no error, spans %v", spans)
	}
	if spans != nil {
		t.Errorf("Detect returned spans alongside an error: %v", spans)
	}

	piiAnalyzerFor = func([]PIICategory) *analyzer.Analyzer {
		a := analyzer.New()
		a.AddRecognizer(panicRecognizer{})
		return a
	}
	spans, err = Detect("panic probe text", nil, DetectOpts{PIICategories: []PIICategory{PIIContact}})
	if err == nil {
		t.Fatalf("Detect recovered a panic without an error, spans %v", spans)
	}
	if spans != nil {
		t.Errorf("Detect returned spans after a panic: %v", spans)
	}
}

func TestDetectConcurrent(t *testing.T) {
	const (
		goroutines = 8
		iterations = 50
	)
	opts := DetectOpts{PIICategories: []PIICategory{PIIContact}}

	// Count provider calls (one per genuine memo miss). The baselines below miss
	// eight times and the goroutines must miss all eight again after the reset;
	// without that reset this stays at eight.
	var analyzes atomic.Int64
	piiAnalyzerFor = func(cats []PIICategory) *analyzer.Analyzer {
		analyzes.Add(1)
		return analyzerFor(cats)
	}
	t.Cleanup(func() { piiAnalyzerFor = analyzerFor })

	texts := make([]string, goroutines)
	wants := make([][]Span, goroutines)
	for g := range texts {
		// One distinct text per goroutine, so each one's first call misses the
		// memo and shares *analyzer.Analyzer with the other goroutines.
		texts[g] = fmt.Sprintf("mail jane%d@example.com or call 555-123-4567", g)
		want, err := Detect(texts[g], nil, opts)
		if err != nil {
			t.Fatalf("baseline Detect: %v", err)
		}
		if !hasPIISpan(want) {
			t.Fatalf("baseline found no PII in %q", texts[g])
		}
		wants[g] = want
	}
	// The baselines warmed all eight keys; clear them so the goroutines reach
	// Analyze instead of replaying memo entries.
	resetPIICache()
	t.Cleanup(resetPIICache)

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				got, err := Detect(texts[g], nil, opts)
				if err != nil {
					errs <- err
					return
				}
				if !spansEqual(wants[g], got) {
					errs <- errors.New("concurrent result differs from baseline")
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := analyzes.Load(); got != int64(2*goroutines) {
		t.Errorf("analyzer provider called %d times, want %d: goroutines did not reach Analyze", got, 2*goroutines)
	}
}

func TestNoAnalyzerWhenCategoriesEmpty(t *testing.T) {
	resetPIICache()
	t.Cleanup(resetPIICache)
	old := piiAnalyzerFor
	t.Cleanup(func() { piiAnalyzerFor = old })

	calls := 0
	piiAnalyzerFor = func([]PIICategory) *analyzer.Analyzer {
		calls++
		return analyzer.New()
	}

	if _, err := Detect("mail jane@example.com", nil, DetectOpts{}); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if calls != 0 {
		t.Errorf("Detect with empty categories called the analyzer provider %d times", calls)
	}

	spans, err := detectPII("mail jane@example.com", nil, nil, false)
	if err != nil {
		t.Fatalf("detectPII: %v", err)
	}
	if spans != nil {
		t.Errorf("detectPII with empty categories returned %v", spans)
	}
	if calls != 0 {
		t.Errorf("detectPII with empty categories called the analyzer provider %d times", calls)
	}
}

// fillPIIMemo puts n distinct filler keys, forcing at least one rotation if n
// crosses a generation boundary.
func fillPIIMemo(prefix string, n int) {
	for i := 0; i < n; i++ {
		piiMemoPut(fmt.Sprintf("%s %d", prefix, i), []Span{{Start: 0, End: 1, Kind: "pii:EMAIL"}})
	}
}

func TestPIICacheSurvivesOneRotation(t *testing.T) {
	resetPIICache()
	t.Cleanup(resetPIICache)

	keep := piiMemoKey("keep me", []PIICategory{PIIContact}, nil, false)
	piiMemoPut(keep, []Span{{Start: 0, End: 1, Kind: "pii:EMAIL"}})

	// Fill the current generation to its bound, then cross it: the next put
	// rotates the full generation into "previous".
	fillPIIMemo("filler", piiMemoGenerationSize-1)
	if got := len(piiMemo.current); got != piiMemoGenerationSize {
		t.Fatalf("current generation holds %d entries, want %d", got, piiMemoGenerationSize)
	}
	piiMemoPut("rotate now", []Span{{Start: 0, End: 1, Kind: "pii:EMAIL"}})

	if _, ok := piiMemoGet(keep); !ok {
		t.Fatal("entry did not survive one rotation")
	}
	if got := len(piiMemo.current); got != 1 {
		t.Errorf("current generation holds %d entries after rotation, want 1", got)
	}
	if got := len(piiMemo.previous); got != piiMemoGenerationSize {
		t.Errorf("previous generation holds %d entries, want %d", got, piiMemoGenerationSize)
	}
}

func TestPIICacheGoneAfterTwoRotations(t *testing.T) {
	resetPIICache()
	t.Cleanup(resetPIICache)

	keep := piiMemoKey("keep me", []PIICategory{PIIContact}, nil, false)
	piiMemoPut(keep, []Span{{Start: 0, End: 1, Kind: "pii:EMAIL"}})

	// Rotation one: keep moves into previous.
	fillPIIMemo("first", piiMemoGenerationSize-1)
	piiMemoPut("rotate one", []Span{{Start: 0, End: 1, Kind: "pii:EMAIL"}})
	if _, ok := piiMemoGet(keep); !ok {
		t.Fatal("precondition failed: entry did not survive the first rotation")
	}

	// Rotation two: previous is replaced, so keep is evicted.
	fillPIIMemo("second", piiMemoGenerationSize-1)
	piiMemoPut("rotate two", []Span{{Start: 0, End: 1, Kind: "pii:EMAIL"}})
	if _, ok := piiMemoGet(keep); ok {
		t.Fatal("entry survived two rotations")
	}
	if _, ok := piiMemoGet("rotate two"); !ok {
		t.Error("the newest entry is missing from the current generation")
	}
}

func TestPIICacheConcurrentRotation(t *testing.T) {
	resetPIICache()
	t.Cleanup(resetPIICache)

	const goroutines = 8
	// Each goroutine writes two full generations of its own keys, so rotations
	// happen while every other goroutine reads and writes. A key put by one
	// goroutine can be evicted by another goroutine's rotation at any moment,
	// so this test asserts only that the cache stays consistent and bounded;
	// the -race detector is what proves the locking.
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 2*piiMemoGenerationSize; i++ {
				key := fmt.Sprintf("rotator %d %d", g, i)
				piiMemoPut(key, []Span{{Start: 0, End: 1, Kind: "pii:EMAIL"}})
				piiMemoGet(key)
			}
		}(g)
	}
	wg.Wait()

	// Two generations cap the live set at 2*size, and a fresh put is readable.
	if got := piiMemoSize(); got > 2*piiMemoGenerationSize {
		t.Errorf("memo holds %d entries, want at most %d", got, 2*piiMemoGenerationSize)
	}
	probe := "concurrent probe"
	piiMemoPut(probe, []Span{{Start: 0, End: 1, Kind: "pii:EMAIL"}})
	if _, ok := piiMemoGet(probe); !ok {
		t.Error("a put made after all goroutines finished is not readable")
	}
}

func TestPIICacheReturnsCopies(t *testing.T) {
	resetPIICache()
	t.Cleanup(resetPIICache)
	old := piiAnalyzerFor
	t.Cleanup(func() { piiAnalyzerFor = old })

	const email = "jane@example.com"
	const text = "mail " + email
	piiAnalyzerFor = func([]PIICategory) *analyzer.Analyzer {
		a := analyzer.New()
		a.AddRecognizer(spanRecognizer{
			start: strings.Index(text, email),
			end:   strings.Index(text, email) + len(email),
			typ:   recognizers.EntityEmail,
		})
		return a
	}
	cats := []PIICategory{PIIContact}
	key := piiMemoKey(text, cats, nil, false)

	// Put path: the cache must snapshot the slice it is handed, so a later
	// mutation of that slice cannot rewrite the entry.
	posted := []Span{{Start: 5, End: 21, Kind: "pii:EMAIL"}}
	piiMemoPut(key, posted)
	posted[0] = Span{Start: 0, End: 1, Kind: "mutated"}
	stored, ok := piiMemoGet(key)
	if !ok || len(stored) != 1 {
		t.Fatalf("memo entry = %v, ok=%v", stored, ok)
	}
	if stored[0].Kind != "pii:EMAIL" {
		t.Errorf("put mutation reached the stored entry: %v", stored)
	}

	// Miss path: mutating what detectPII returned must not touch the entry.
	resetPIICache()
	got, err := detectPII(text, cats, nil, false)
	if err != nil {
		t.Fatalf("detectPII: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("detectPII returned %v, want one span", got)
	}
	got[0] = Span{Start: 0, End: 1, Kind: "mutated"}

	stored, ok = piiMemoGet(key)
	if !ok || len(stored) != 1 {
		t.Fatalf("memo entry = %v, ok=%v", stored, ok)
	}
	if stored[0].Kind != "pii:EMAIL" {
		t.Errorf("caller mutation reached the stored entry: %v", stored)
	}

	// Get path: two reads must not share backing storage.
	first, ok := piiMemoGet(key)
	if !ok || len(first) != 1 {
		t.Fatalf("memo read = %v, ok=%v", first, ok)
	}
	first[0] = Span{Start: 0, End: 1, Kind: "mutated"}
	second, _ := piiMemoGet(key)
	if second[0].Kind != "pii:EMAIL" {
		t.Errorf("read mutation reached the stored entry: %v", second)
	}

	// detectPII's hit path returns a copy too.
	hit, err := detectPII(text, cats, nil, false)
	if err != nil {
		t.Fatalf("detectPII hit: %v", err)
	}
	if len(hit) != 1 {
		t.Fatalf("detectPII hit returned %v, want one span", hit)
	}
	hit[0] = Span{Start: 0, End: 1, Kind: "mutated"}
	after, _ := piiMemoGet(key)
	if after[0].Kind != "pii:EMAIL" {
		t.Errorf("hit-path mutation reached the stored entry: %v", after)
	}
}

func benchmarkPIIText() string {
	const line = "Contact jane@example.com or call 555-123-4567. Card 4111111111111111. IBAN GB82 WEST 1234 5698 7654 32. "
	return strings.Repeat(line, 500) // 104 bytes * 500 = ~50 KB
}

func BenchmarkDetectPII_50KB(b *testing.B) {
	base := benchmarkPIIText()
	sets := []struct {
		name string
		cats []PIICategory
	}{
		{"contact", []PIICategory{PIIContact}},
		{"contact_financial", []PIICategory{PIIContact, PIIFinancial}},
		{"all", AllPIICategories},
	}
	for _, set := range sets {
		b.Run(set.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				// A unique suffix per iteration: a fixed 26-rune cycle would be
				// satisfied by the memo after the first 26 and measure hashing.
				text := base + fmt.Sprintf("#%d", i)
				if _, err := Detect(text, nil, DetectOpts{PIICategories: set.cats}); err != nil {
					b.Fatalf("Detect: %v", err)
				}
			}
		})
	}
}

func TestPIIGroupFloorBoundary(t *testing.T) {
	resetPIICache()
	t.Cleanup(resetPIICache)
	old := piiAnalyzerFor
	t.Cleanup(func() { piiAnalyzerFor = old })

	cases := []struct {
		name       string
		typ        recognizers.EntityType
		category   PIICategory
		confidence float64
		wantKept   bool
	}{
		{"network below floor", recognizers.EntityIP, PIINetwork, 0.89, false},
		{"network at floor", recognizers.EntityIP, PIINetwork, 0.90, true},
		{"network url below floor", recognizers.EntityURL, PIINetwork, 0.85, false},
		{"network url above floor", recognizers.EntityURL, PIINetwork, 0.99, true},
		{"money below floor", recognizers.EntityMoney, PIIMoney, 0.84, false},
		{"money at floor", recognizers.EntityMoney, PIIMoney, 0.85, true},
		{"default group at analyzer floor", recognizers.EntityEmail, PIIContact, 0.70, true},
		{"default group below analyzer floor", recognizers.EntityEmail, PIIContact, 0.69, false},
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text := fmt.Sprintf("floor probe %d", i)
			piiAnalyzerFor = func([]PIICategory) *analyzer.Analyzer {
				a := analyzer.New()
				a.AddRecognizer(scoreRecognizer{start: 0, end: len(text), typ: c.typ, confidence: c.confidence})
				return a
			}
			got, err := detectPII(text, []PIICategory{c.category}, nil, false)
			if err != nil {
				t.Fatalf("detectPII: %v", err)
			}
			if kept := len(got) == 1; kept != c.wantKept {
				t.Fatalf("kept=%v, want %v (spans %v)", kept, c.wantKept, got)
			}
		})
	}
}

// TestPIIGroupFloorsKeepCanonicalSamples pins that the per-group floors sit
// below the upstream scores of every canonical sample, so enabling a group
// never silently stops redacting it.
func TestPIIGroupFloorsKeepCanonicalSamples(t *testing.T) {
	resetPIICache()
	t.Cleanup(resetPIICache)

	cases := []struct {
		group  PIICategory
		sample string
		typ    recognizers.EntityType
	}{
		{PIIContact, "mail jane@example.com", recognizers.EntityEmail},
		{PIIContact, "call +447911123456 now", recognizers.EntityPhone},
		{PIIFinancial, "card 4111111111111111", recognizers.EntityCreditCard},
		{PIIFinancial, "IBAN: DE89370400440532013000", recognizers.EntityIBAN},
		{PIIIdentity, "ssn 123-45-6789", recognizers.EntitySSN},
		{PIIIdentity, "Passport Number: 322996077 is shown.", recognizers.EntityPassport},
		{PIILegal, "case 23-CV-0001", recognizers.EntityCaseNumber},
		{PIINetwork, "host 10.0.0.1", recognizers.EntityIP},
		{PIINetwork, "see https://example.com/x now", recognizers.EntityURL},
		{PIIMoney, "total $1,234.56 due", recognizers.EntityMoney},
	}

	for _, c := range cases {
		t.Run(string(c.group)+"/"+string(c.typ), func(t *testing.T) {
			ents, err := analyzerFor([]PIICategory{c.group}).Analyze(context.Background(), c.sample)
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			var score float64
			found := false
			for _, e := range ents {
				if e.Type == c.typ {
					found = true
					score = e.Confidence
				}
			}
			if !found {
				t.Fatalf("analyzer produced no %s in %q: %+v", c.typ, c.sample, ents)
			}
			if floor := piiGroupFloor(c.group); score < floor {
				t.Fatalf("%s scored %v, below its group floor %v", c.typ, score, floor)
			}

			spans, err := detectPII(c.sample, []PIICategory{c.group}, nil, false)
			if err != nil {
				t.Fatalf("detectPII: %v", err)
			}
			if got := countPIIKinds(spans, piiKindPrefix+string(c.typ)); got == 0 {
				t.Fatalf("floor filter dropped %s from %q: %v", c.typ, c.sample, spans)
			}
		})
	}
}

// TestPIIEntityGroupsCoverRecognizers fails when a recognizer is wired without
// an entry in piiEntityGroups, or a mapped type no recognizer can emit.
func TestPIIEntityGroupsCoverRecognizers(t *testing.T) {
	samples := map[string]struct {
		text string
		typ  recognizers.EntityType
	}{
		"regex_email":             {"mail jane@example.com", recognizers.EntityEmail},
		"regex_phone":             {"call +447911123456 now", recognizers.EntityPhone},
		"checksum_luhn":           {"4111111111111111", recognizers.EntityCreditCard},
		"checksum_iban":           {"IBAN: DE89370400440532013000", recognizers.EntityIBAN},
		"finance_swift_bic":       {"Send to SWIFT code DEUTDEFF today.", recognizers.EntitySWIFTBIC},
		"finance_us_routing":      {"Bank routing number: 123456780 here.", recognizers.EntityUSRouting},
		"finance_card_cvv":        {"CVV 123 was entered.", recognizers.EntityCardCVV},
		"finance_bank_account":    {"account number 093-284359714 is open", recognizers.EntityBankAccount},
		"finance_pin":             {"The account PIN is 482913 for phone banking.", recognizers.EntityPIN},
		"finance_lei":             {"Entity LEI 54930084UKLVMY22DS16 registered.", recognizers.EntityLEI},
		"finance_isin":            {"ISIN US0378331005", recognizers.EntityISIN},
		"finance_ethereum":        {"Send to 0x1a2B3C4d5e6F70819a2B3C4d5E6F70819a2B3C4D today.", recognizers.EntityEthereum},
		"regex_bitcoin":           {"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", recognizers.EntityBitcoin},
		"regex_ssn":               {"123-45-6789", recognizers.EntitySSN},
		"identity_passport":       {"Passport Number: 322996077 is shown.", recognizers.EntityPassport},
		"identity_driver_license": {"Driver's license number: D42533932 checked.", recognizers.EntityDriverLicense},
		"identity_tax_id":         {"My tax ID is 43-5478236 for filing.", recognizers.EntityTaxID},
		"identity_date_of_birth":  {"Date of birth: 1984-03-02, next of kin.", recognizers.EntityDateOfBirth},
		"national_sg_nric":        {"Applicant NRIC S1234567D is on file.", recognizers.EntitySGNRIC},
		"national_my_ic":          {"Her MyKad 630916-12-3456 was copied.", recognizers.EntityMYIC},
		"national_th_id":          {"Citizen ID 3-1006-00445-63-5 on the form.", recognizers.EntityTHID},
		"national_ph_tin":         {"TIN 123-456-789 for the head office", recognizers.EntityPHTIN},
		"national_in_gstin":       {"Supplier GSTIN 27AAACR5055K1Z7 on the invoice.", recognizers.EntityINGSTIN},
		"national_in_pan":         {"PAN AAAPZ1234C", recognizers.EntityINPAN},
		"national_sg_uen":         {"UEN 53412378W", recognizers.EntitySGUEN},
		"national_my_ssm":         {"SSM 1312525-A", recognizers.EntityMYSSM},
		"national_my_tin":         {"TIN IG4455667788", recognizers.EntityMYTIN},
		"national_my_passport":    {"Passport No: A12345678", recognizers.EntityMYPassport},
		"national_sg_passport":    {"Passport Number: E1234567A on file.", recognizers.EntitySGPassport},
		"legal_case_number":       {"23-CV-0001", recognizers.EntityCaseNumber},
		"regex_ip":                {"10.0.0.1", recognizers.EntityIP},
		"regex_url":               {"https://example.com/x", recognizers.EntityURL},
		"finance_money":           {"$1,234.56", recognizers.EntityMoney},
	}

	wired := 0
	for _, cat := range AllPIICategories {
		for _, r := range recognizersFor(cat) {
			wired++
			s, ok := samples[r.Name()]
			if !ok {
				t.Fatalf("recognizer %q has no canonical sample; add one and map its entity type in piiEntityGroups", r.Name())
			}
			ents, err := r.Recognize(context.Background(), s.text)
			if err != nil {
				t.Fatalf("%s.Recognize: %v", r.Name(), err)
			}
			found := false
			for _, e := range ents {
				group, mapped := piiEntityGroups[e.Type]
				if !mapped {
					t.Errorf("entity type %q from %s is not in piiEntityGroups", e.Type, r.Name())
					continue
				}
				if e.Type == s.typ {
					found = true
					if group != cat {
						t.Errorf("%s emitted %s mapped to %q, want group %q", r.Name(), e.Type, group, cat)
					}
				}
			}
			if !found {
				t.Fatalf("%s produced no %s in %q: %+v", r.Name(), s.typ, s.text, ents)
			}
		}
	}
	if len(samples) != wired {
		t.Fatalf("sample table has %d entries but %d recognizers are wired", len(samples), wired)
	}
	if len(piiEntityGroups) != wired {
		t.Fatalf("piiEntityGroups has %d entries but %d recognizers are wired", len(piiEntityGroups), wired)
	}
}

func BenchmarkDetectPII_MemoHit(b *testing.B) {
	text := benchmarkPIIText()
	cats := []PIICategory{PIIContact}
	if _, err := detectPII(text, cats, nil, false); err != nil {
		b.Fatalf("warm-up detectPII: %v", err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := detectPII(text, cats, nil, false); err != nil {
			b.Fatalf("detectPII: %v", err)
		}
	}
}
