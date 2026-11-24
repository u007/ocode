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
	piiMemo.entries = make(map[string][]Span)
	piiMemo.mu.Unlock()
}

// piiMemoSize reports the number of live memo entries.
func piiMemoSize() int {
	piiMemo.mu.Lock()
	defer piiMemo.mu.Unlock()
	return len(piiMemo.entries)
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

func TestMergeSpansOverlapPrecedence(t *testing.T) {
	// A secret beats a longer PII span that contains it.
	secret := Span{Start: 5, End: 8, Kind: "aws_key"}
	pii := Span{Start: 4, End: 12, Kind: "pii:EMAIL"}
	if got := mergeSpans([]Span{secret}, []Span{pii}); !spansEqual(got, []Span{secret}) {
		t.Errorf("secret vs PII = %v, want %v", got, []Span{secret})
	}

	// Among secrets, the longer span wins.
	short := Span{Start: 0, End: 5, Kind: "custom"}
	long := Span{Start: 0, End: 9, Kind: "env_secret"}
	if got := mergeSpans([]Span{short, long}, nil); !spansEqual(got, []Span{long}) {
		t.Errorf("longer secret = %v, want %v", got, []Span{long})
	}

	// Equal lengths: the earlier start wins.
	early := Span{Start: 0, End: 4, Kind: "custom"}
	late := Span{Start: 2, End: 6, Kind: "custom"}
	if got := mergeSpans([]Span{late, early}, nil); !spansEqual(got, []Span{early}) {
		t.Errorf("earlier secret = %v, want %v", got, []Span{early})
	}

	// Non-overlapping spans are all kept in (Start, End, Kind) order.
	a := Span{Start: 10, End: 12, Kind: "custom"}
	b := Span{Start: 0, End: 2, Kind: "custom"}
	if got := mergeSpans([]Span{a}, []Span{b}); !spansEqual(got, []Span{b, a}) {
		t.Errorf("non-overlap order = %v, want %v", got, []Span{b, a})
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

func TestPIICacheOverflowClears(t *testing.T) {
	resetPIICache()
	t.Cleanup(resetPIICache)
	old := piiAnalyzerFor
	t.Cleanup(func() { piiAnalyzerFor = old })
	piiAnalyzerFor = func([]PIICategory) *analyzer.Analyzer { return analyzer.New() }

	cats := []PIICategory{PIIContact}
	for i := 0; i < piiMemoMaxSize; i++ {
		if _, err := detectPII(fmt.Sprintf("probe %d", i), cats, nil, false); err != nil {
			t.Fatalf("detectPII probe %d: %v", i, err)
		}
	}
	if got := piiMemoSize(); got != piiMemoMaxSize {
		t.Fatalf("memo holds %d entries, want %d", got, piiMemoMaxSize)
	}
	if _, ok := piiMemoGet(piiMemoKey("probe 0", cats, nil, false)); !ok {
		t.Fatal("first probe missing from a full memo")
	}

	// One more insert crosses the bound and must drop every old entry.
	if _, err := detectPII("overflow probe", cats, nil, false); err != nil {
		t.Fatalf("detectPII overflow probe: %v", err)
	}
	if got := piiMemoSize(); got != 1 {
		t.Fatalf("memo holds %d entries after overflow, want 1", got)
	}
	if _, ok := piiMemoGet(piiMemoKey("probe 0", cats, nil, false)); ok {
		t.Error("pre-overflow entry survived the clear")
	}
	if _, ok := piiMemoGet(piiMemoKey("overflow probe", cats, nil, false)); !ok {
		t.Error("overflowing entry was not stored")
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
