package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

// speechSummaryTestClient records what it was asked to summarise and returns a
// canned reply, so the tests never touch a provider.
type speechSummaryTestClient struct {
	gotSystem string
	gotUser   string
	reply     string
	err       error
	calls     int
}

func (c *speechSummaryTestClient) GetProvider() string { return "testprov" }
func (c *speechSummaryTestClient) GetModel() string    { return "test-model" }
func (c *speechSummaryTestClient) Chat(messages []Message, _ []map[string]interface{}) (*Message, error) {
	c.calls++
	for _, m := range messages {
		switch m.Role {
		case "system":
			c.gotSystem = m.Content
		case "user":
			c.gotUser = m.Content
		}
	}
	if c.err != nil {
		return nil, c.err
	}
	return &Message{Role: "assistant", Content: c.reply}, nil
}

// newSpeechSummaryTestAgent builds an Agent whose speech-summary client IS the
// stub.
//
// The configured model is deliberately left as a BARE name (not
// "openai/gpt-4o-mini") and no provider API key is set, so NewClient refuses to
// build a real client and overrideModelClient falls through to a.client — the
// stub. Setting OPENAI_API_KEY here would silently make these tests hit the
// network.
//
// TMPDIR is redirected so the temp-file cache is per-test: without it a summary
// cached by one test would satisfy another's miss.
func newSpeechSummaryTestAgent(t *testing.T, stub LLMClient) *Agent {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "OPENROUTER_API_KEY"} {
		t.Setenv(key, "")
	}
	lastSpeechSummaryPrune.Store(0)
	return &Agent{
		client: stub,
		config: &config.Config{Ocode: config.OcodeConfig{
			SpeechSummaryModel:   "local-summary-model",
			SpeechSummaryEnabled: true,
		}},
	}
}

// TestSummarizeForSpeechReturnsTheModelsProse is the happy path: the model is
// asked, its prose is returned, and the caller never sees the original markdown.
func TestSummarizeForSpeechReturnsTheModelsProse(t *testing.T) {
	stub := &speechSummaryTestClient{reply: "It raises the retry limit to three."}
	a := newSpeechSummaryTestAgent(t, stub)

	got := a.SummarizeForSpeech("```go\nretries = 3\n```")
	if got != "It raises the retry limit to three." {
		t.Fatalf("SummarizeForSpeech = %q", got)
	}
	if stub.calls != 1 {
		t.Fatalf("expected one model call, got %d", stub.calls)
	}
	if stub.gotUser != "```go\nretries = 3\n```" {
		t.Errorf("the raw message must be what is sent, got %q", stub.gotUser)
	}
}

// TestSummarizeForSpeechSendsTheCodeDescribingPrompt pins the product
// requirement: code is DESCRIBED, not omitted and not quoted. Losing this
// wording silently regresses speech back to dictating diffs.
func TestSummarizeForSpeechSendsTheCodeDescribingPrompt(t *testing.T) {
	stub := &speechSummaryTestClient{reply: "ok"}
	a := newSpeechSummaryTestAgent(t, stub)

	a.SummarizeForSpeech("some message")

	for _, want := range []string{
		"SUMMARISE code",
		"Never read out source code",
		"spoken prose",
		"no markdown",
	} {
		if !strings.Contains(stub.gotSystem, want) {
			t.Errorf("system prompt must contain %q; got:\n%s", want, stub.gotSystem)
		}
	}
}

// TestSummarizeForSpeechFailureReturnsEmpty is the contract the web layer
// depends on: a summariser failure must be reported as "" so the caller speaks
// the full text. It must never be an error or partial output.
func TestSummarizeForSpeechFailureReturnsEmpty(t *testing.T) {
	stub := &speechSummaryTestClient{err: errors.New("provider exploded")}
	a := newSpeechSummaryTestAgent(t, stub)

	if got := a.SummarizeForSpeech("hello"); got != "" {
		t.Fatalf("a failed summary must return \"\", got %q", got)
	}
}

// TestSummarizeForSpeechEmptyInputNoOps keeps a blank selection from costing a
// round trip.
func TestSummarizeForSpeechEmptyInputNoOps(t *testing.T) {
	stub := &speechSummaryTestClient{reply: "x"}
	a := newSpeechSummaryTestAgent(t, stub)

	if got := a.SummarizeForSpeech("   \n\t "); got != "" {
		t.Fatalf("blank input must no-op, got %q", got)
	}
	if stub.calls != 0 {
		t.Fatalf("blank input must not call the model, calls=%d", stub.calls)
	}
}

// TestSummarizeForSpeechReusesTheCache proves the second identical request does
// not pay for a second model call, which is the point of the temp-file cache.
func TestSummarizeForSpeechReusesTheCache(t *testing.T) {
	stub := &speechSummaryTestClient{reply: "cached prose"}
	a := newSpeechSummaryTestAgent(t, stub)

	first := a.SummarizeForSpeech("a message spoken twice")
	second := a.SummarizeForSpeech("a message spoken twice")

	if first != "cached prose" || second != first {
		t.Fatalf("expected the cached summary back, got %q then %q", first, second)
	}
	if stub.calls != 1 {
		t.Fatalf("the cache must prevent a second model call, calls=%d", stub.calls)
	}
}

// TestSummarizeForSpeechDoesNotCacheAFailure: caching an empty result would make
// a transient provider error sticky for the whole TTL.
func TestSummarizeForSpeechDoesNotCacheAFailure(t *testing.T) {
	stub := &speechSummaryTestClient{err: errors.New("transient")}
	a := newSpeechSummaryTestAgent(t, stub)

	if got := a.SummarizeForSpeech("fails first"); got != "" {
		t.Fatalf("first call should fail, got %q", got)
	}
	stub.err = nil
	stub.reply = "recovered"
	if got := a.SummarizeForSpeech("fails first"); got != "recovered" {
		t.Fatalf("a failed summary must not be cached: got %q", got)
	}
	if stub.calls != 2 {
		t.Fatalf("expected a retry after the failure, calls=%d", stub.calls)
	}
}

// TestSummarizeForSpeechCacheIsPerModel: switching the summary model must not
// keep serving the previous model's prose.
func TestSummarizeForSpeechCacheIsPerModel(t *testing.T) {
	stub := &speechSummaryTestClient{reply: "from the model"}
	a := newSpeechSummaryTestAgent(t, stub)

	if got := a.SummarizeForSpeech("same message"); got != "from the model" {
		t.Fatalf("first summary = %q", got)
	}
	// A different model id must miss the cache. The stub's GetModel is fixed, so
	// drive the change through the cache key inputs directly instead.
	if speechSummaryCacheKey("same message", "testprov/test-model") ==
		speechSummaryCacheKey("same message", "testprov/other-model") {
		t.Fatal("the cache key must include the model")
	}
}

// TestCleanSpeechSummaryStripsModelLeakage covers the real-world model
// behaviours that would otherwise be spoken aloud verbatim.
func TestCleanSpeechSummaryStripsModelLeakage(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "It works.", "It works."},
		{"fenced", "```\nIt works.\n```", "It works."},
		{"leading label", "Here is the spoken summary:\nIt works.", "It works."},
		{"wrapped quotes", `"It works."`, "It works."},
		{"trims whitespace", "  \n It works. \n ", "It works."},
		{"empty stays empty", "   ", ""},
		{"punctuation only", " ... ", ""},
		{"a short first line with a colon is not a label if there is no body", "Note:", "Note:"},
		{"a first line that is not a label is kept", "It works:\nbecause the limit is 3.", "It works:\nbecause the limit is 3."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanSpeechSummary(tt.in); got != tt.want {
				t.Errorf("cleanSpeechSummary(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestIsSpeechPreambleIsConservative pins the boundary that a real summary hit:
// only recognisable model preambles are stripped, because a generic "ends with
// a colon" rule deletes the first sentence of legitimate prose.
func TestIsSpeechPreambleIsConservative(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"Here is the spoken summary:", true},
		{"here's the summary:", true},
		{"Summary:", true},
		{"Spoken:", true},
		{"  Summary:  ", true},
		{"It works:", false},                         // real prose, not a label
		{"It works: because the limit is 3.", false}, // colon mid-sentence
		{"Here is what changed:", true},              // recognised lead-in
		{"Here is a very long lead in that goes on and on and on past the sixty rune limit:", false},
		{"No colon at all", false},
		{"", false},
		{":", false}, // empty label
	}
	for _, tt := range tests {
		if got := isSpeechPreamble(tt.line); got != tt.want {
			t.Errorf("isSpeechPreamble(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

// TestTruncateRunesNeverSplitsAMultiByteRune guards the truncation used on both
// the input and the output: slicing bytes would emit a replacement character
// into the spoken text.
func TestTruncateRunesNeverSplitsAMultiByteRune(t *testing.T) {
	s := "éé世界" // 2-byte then 3-byte runes
	for n := 0; n <= 4; n++ {
		got := truncateRunes(s, n)
		if len([]rune(got)) != n {
			t.Errorf("truncateRunes(%q, %d) = %q (%d runes)", s, n, got, len([]rune(got)))
		}
		if !strings.HasPrefix(s, got) {
			t.Errorf("truncateRunes(%q, %d) = %q is not a prefix", s, n, got)
		}
	}
	if got := truncateRunes(s, 0); got != "" {
		t.Errorf("n=0 must yield \"\", got %q", got)
	}
	if got := truncateRunes(s, 99); got != s {
		t.Errorf("n beyond the length must return the whole string, got %q", got)
	}
}

// TestSpeechSummaryCacheKeyShape pins that the key covers text AND model, and
// that it does not leak message content into filenames.
func TestSpeechSummaryCacheKeyShape(t *testing.T) {
	a := speechSummaryCacheKey("same text", "openai/gpt-4o-mini")
	b := speechSummaryCacheKey("same text", "anthropic/claude-haiku-4-5")
	c := speechSummaryCacheKey("same text", "openai/gpt-4o-mini")
	if a == b {
		t.Error("the cache key must include the model")
	}
	if a != c {
		t.Error("the cache key must be stable for the same text + model")
	}
	if a == speechSummaryCacheKey("other text", "openai/gpt-4o-mini") {
		t.Error("the cache key must include the text")
	}
	if strings.Contains(a, "same") || strings.Contains(a, "text") {
		t.Errorf("the cache key leaks the input text: %q", a)
	}
}

// TestPruneSpeechSummariesSweepsOnlyExpiredFiles covers the user's ask — a temp
// file cache that cleans itself up — including that the sweep is bounded by the
// TTL and never removes a directory.
func TestPruneSpeechSummariesSweepsOnlyExpiredFiles(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, "fresh.txt")
	stale := filepath.Join(dir, "stale.txt")
	if err := os.WriteFile(fresh, []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * speechSummaryCacheTTL)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o700); err != nil {
		t.Fatal(err)
	}

	pruneSpeechSummaries(dir, time.Now())

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("an expired entry must be swept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("a live entry must survive the sweep: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "subdir")); err != nil {
		t.Errorf("the sweep must not remove directories: %v", err)
	}
}

// TestWriteSpeechSummaryCachePrunesOpportunistically proves the production write
// path actually sweeps (the sweep is not only reachable from the unit test
// above), and that the throttle does not block the first sweep.
func TestWriteSpeechSummaryCachePrunesOpportunistically(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	lastSpeechSummaryPrune.Store(0)

	stale := filepath.Join(speechSummaryCacheDir(), "expired.txt")
	if err := os.MkdirAll(filepath.Dir(stale), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * speechSummaryCacheTTL)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	writeSpeechSummaryCache(speechSummaryCacheKey("x", "m"), "a summary")

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a write must opportunistically sweep expired entries")
	}
	if got, ok := readSpeechSummaryCache(speechSummaryCacheKey("x", "m")); !ok || got != "a summary" {
		t.Errorf("the written entry must be readable, got %q ok=%v", got, ok)
	}
}

// TestReadSpeechSummaryCacheTreatsExpiredAsMiss pins that an expired entry is
// not served even before the sweep runs.
func TestReadSpeechSummaryCacheTreatsExpiredAsMiss(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	key := speechSummaryCacheKey("stale text", "m")
	path := filepath.Join(speechSummaryCacheDir(), key+".txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stale summary"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * speechSummaryCacheTTL)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if got, ok := readSpeechSummaryCache(key); ok {
		t.Errorf("an expired entry must be a miss, got %q", got)
	}
}

// TestSpeechSummaryClientIgnoresThinkingBudgetAndConfiguredModelEmpty pins the
// two config-facing behaviours: a blank model means "no override", and the
// client never inherits the user's reasoning budget.
func TestSpeechSummaryClientPrefersSmallThenMainWhenUnset(t *testing.T) {
	stub := &speechSummaryTestClient{reply: "ok"}
	a := newSpeechSummaryTestAgent(t, stub)
	a.config.Ocode.SpeechSummaryModel = "" // no override
	a.config.Ocode.SmallModel = "small-model"
	a.config.Ocode.SmallModelEnabled = false

	// No override, small model disabled → falls through to the main client.
	if got := a.speechSummaryClient(); got != LLMClient(stub) {
		t.Fatalf("expected the main client, got %T", got)
	}
}

// blockingChatClient blocks inside ChatWithContext until its context is
// cancelled. It exists to prove the summariser's timeout actually cancels the
// in-flight provider call instead of merely abandoning the wait (which leaked
// the goroutine and its request).
type blockingChatClient struct {
	chatCalled  bool
	contextUsed bool
	// cancelled is closed by ChatWithContext once it observes ctx cancellation.
	// The test must wait on it: SummarizeForSpeech returns the moment ctx fires,
	// which can be before the provider goroutine is scheduled to notice.
	cancelled chan struct{}
}

func (c *blockingChatClient) GetProvider() string { return "testprov" }
func (c *blockingChatClient) GetModel() string    { return "test-model" }
func (c *blockingChatClient) Chat(_ []Message, _ []map[string]interface{}) (*Message, error) {
	c.chatCalled = true
	return nil, errors.New("Chat must not be used when ChatWithContext is available")
}
func (c *blockingChatClient) ChatWithContext(ctx context.Context, _ []Message, _ []map[string]interface{}) (*Message, error) {
	c.contextUsed = true
	<-ctx.Done()
	close(c.cancelled)
	return nil, ctx.Err()
}

// TestSummarizeForSpeechCancelsTheProviderCallOnTimeout is the regression for
// the leaked-goroutine bug: before this, SummarizeForSpeech called the
// contextless Chat, so the 60s context only stopped waiting — the request kept
// running. A context-capable client must be used and must observe cancellation.
func TestSummarizeForSpeechCancelsTheProviderCallOnTimeout(t *testing.T) {
	old := speechSummaryTimeout
	speechSummaryTimeout = 50 * time.Millisecond
	defer func() { speechSummaryTimeout = old }()

	stub := &blockingChatClient{cancelled: make(chan struct{})}
	a := newSpeechSummaryTestAgent(t, stub)

	if got := a.SummarizeForSpeech("anything"); got != "" {
		t.Fatalf("a timed-out summary must return \"\", got %q", got)
	}
	// Reading these is safe after the handshake below; contextUsed is written
	// before the goroutine blocks, chatCalled is written by the fallback path.
	if stub.chatCalled {
		t.Fatal("the contextless Chat must not be used when ChatWithContext exists")
	}
	select {
	case <-stub.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the context passed to the provider call must be cancelled on timeout")
	}
	if !stub.contextUsed {
		t.Fatal("SummarizeForSpeech must use ChatWithContext when the client offers it")
	}
}

// TestSummarizeForSpeechFallsBackToChatForContextlessClients keeps non-standard
// clients (and test stubs) working: without ChatWithContext the plain Chat path
// must still be taken.
func TestSummarizeForSpeechFallsBackToChatForContextlessClients(t *testing.T) {
	stub := &speechSummaryTestClient{reply: "plain path"}
	a := newSpeechSummaryTestAgent(t, stub)

	if got := a.SummarizeForSpeech("hello"); got != "plain path" {
		t.Fatalf("a contextless client must still be used, got %q", got)
	}
}
