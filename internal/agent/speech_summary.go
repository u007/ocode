package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/crashguard"
)

// speechSummarySystemPrompt turns an assistant message into something worth
// listening to. The audience is a developer with the message playing in the
// background, not someone reading along, so the ordering is deliberate: keep
// the meaning, and DESCRIBE technical artifacts rather than quoting them. The
// existing speech path already avoids reading markdown *syntax* (it speaks the
// rendered DOM), but it still reads code, diffs and paths aloud verbatim —
// which is what this rewrite exists to fix.
const speechSummarySystemPrompt = `You rewrite a coding assistant's reply so it can be READ ALOUD to a developer who is listening, not reading.

Rewrite it as spoken prose:
- Keep the meaning: decisions, conclusions, caveats, and any numbers the listener needs.
- SUMMARISE code and technical artifacts instead of quoting them. Never read out source code, diffs, patch hunks, stack traces, command lines, log dumps, file paths, URLs or table rows. Where such an artifact matters, say in one short clause what it does or what changed, e.g. "it raises the retry limit to three" or "the parser now rejects an empty key".
- Name files by their short name without a directory path, and only when which file matters.
- Plain sentences only: no markdown, no headings, no bullet characters, no asterisks, no emoji, no code formatting, no URLs.
- Prefer two to five short sentences. If the reply is already short plain prose, return it almost unchanged.
- If the reply is only a question or a request for the listener, keep it as that question or request.

Output ONLY the spoken text, with no preamble, labels or quotation marks.`

const (
	// speechSummaryTimeoutSeconds bounds one rewrite. Generous relative to
	// compaction's per-batch budget because this is a single small request and a
	// slow local model is a supported configuration.
	speechSummaryTimeoutSeconds = 60
	// speechSummaryMaxInputChars caps what is sent. A spoken summary of a
	// 60k-char reply is pointless — the tail is almost always more code — and the
	// cap keeps a pathological message from turning one click into a large bill.
	speechSummaryMaxInputChars = 40000
	// speechSummaryMaxOutputChars guards against a model that ignores the
	// "short" instruction and streams back the whole input.
	speechSummaryMaxOutputChars = 4000
	// speechSummaryCacheTTL is how long a generated summary is reused. Replay
	// and repeated "speak this message" clicks are common, and each miss is a
	// paid round trip.
	speechSummaryCacheTTL = 24 * time.Hour
	// speechSummaryPruneInterval throttles the opportunistic sweep so a busy
	// server does not rescan the cache directory on every write.
	speechSummaryPruneInterval = time.Hour
)

// speechSummaryTimeout is speechSummaryTimeoutSeconds as a Duration. A var (not
// a const use) so a test can shorten it to prove that a blocking client is
// actually cancelled by the context instead of waiting the full minute.
var speechSummaryTimeout = speechSummaryTimeoutSeconds * time.Second

// speechSummaryClient resolves the model used for speech summaries.
//
// Resolution order: the configured speech_summary_model; else the small model
// when its gate is on; else a no-thinking copy of the main client. Sharing
// smallModelOrMainClient/overrideModelClient with compaction is intentional —
// the "which provider does this model id mean" logic is subtle (see
// overrideModelClient) and duplicating it once already produced a silent
// wrong-backend bug.
func (a *Agent) speechSummaryClient() LLMClient {
	if a.config == nil {
		if noThink := a.noThinkingClient(); noThink != nil {
			return noThink
		}
		return a.client
	}
	// Never let a spoken summary spend the user's reasoning budget: the output
	// is one short paragraph of prose, so extended thinking is pure cost.
	cfg := *a.config
	cfg.ThinkingBudget = 0

	model := strings.TrimSpace(cfg.Ocode.SpeechSummaryModel)
	if model == "" {
		return a.smallModelOrMainClient(&cfg)
	}
	return a.overrideModelClient(&cfg, "", model)
}

// contextChatter is implemented by the real HTTP client (*GenericClient). The
// summariser must run its LLM call under the speechSummaryTimeout context so a
// timeout actually cancels the in-flight request instead of merely abandoning
// the wait and leaving the goroutine (and its provider call) running. Test
// stubs and non-standard client implementations offer only Chat and take the
// uncancellable fallback path.
type contextChatter interface {
	ChatWithContext(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error)
}

// chatWithOptionalContext prefers the cancellable path when the client offers
// it, so ctx cancellation (the speech-summary timeout, or shutdown) interrupts
// the provider call. It falls back to the contextless Chat for clients that
// cannot honor a context.
func chatWithOptionalContext(ctx context.Context, client LLMClient, messages []Message) (*Message, error) {
	if cc, ok := client.(contextChatter); ok {
		return cc.ChatWithContext(ctx, messages, nil)
	}
	return client.Chat(messages, nil)
}

// SummarizeForSpeech rewrites text into spoken prose via the speech-summary
// model, caching the result. It returns "" when summarising is impossible
// (no client, timeout, provider error, empty model output) — the caller is
// expected to fall back to speaking the original text, so speech NEVER fails
// because the summariser did.
func (a *Agent) SummarizeForSpeech(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	client := a.speechSummaryClient()
	if client == nil {
		return ""
	}

	key := speechSummaryCacheKey(text, speechSummaryModelID(client))
	if cached, ok := readSpeechSummaryCache(key); ok {
		return cached
	}

	body := text
	if len(body) > speechSummaryMaxInputChars {
		// Truncate on a rune boundary so a multi-byte character is never split.
		body = truncateRunes(body, speechSummaryMaxInputChars)
	}

	ctx, cancel := context.WithTimeout(context.Background(), speechSummaryTimeout)
	defer cancel()
	done := make(chan summaryResult, 1)
	messages := []Message{
		{Role: "system", Content: speechSummarySystemPrompt},
		{Role: "user", Content: body},
	}
	crashguard.Go(func() {
		resp, err := chatWithOptionalContext(ctx, client, messages)
		if err != nil {
			done <- summaryResult{err: err}
			return
		}
		a.RecordSideUsageFromMessage(resp)
		done <- summaryResult{content: resp.Content}
	})

	select {
	case <-ctx.Done():
		a.emitDebug("SPEECH", fmt.Sprintf("speech summary cancelled/timed out after %ds", int(speechSummaryTimeout.Seconds())))
		return ""
	case r := <-done:
		if r.err != nil {
			a.emitDebug("SPEECH", "speech summary failed: "+r.err.Error())
			return ""
		}
		summary := cleanSpeechSummary(r.content)
		if summary == "" {
			a.emitDebug("SPEECH", "speech summary was empty after cleaning; falling back to full text")
			return ""
		}
		writeSpeechSummaryCache(key, summary)
		return summary
	}
}

// cleanSpeechSummary normalises model output into speakable text, returning ""
// when nothing usable is left. Models leak markdown and reasoning preambles
// ("Here is the summary:") even when told not to, and speaking those is exactly
// what this feature exists to avoid.
func cleanSpeechSummary(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Strip a small set of fences some models add despite the instruction.
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.Trim(s, "`")
	// Drop a leading PREAMBLE the model added despite the instruction, e.g.
	// "Here is the spoken summary:". The match is deliberately narrow: a first
	// line that merely ends in a colon is usually a real sentence ("It works:
	// because…"), and eating that would silently delete content from the
	// summary. Only recognisable preambles are removed.
	if i := strings.IndexByte(s, '\n'); i > 0 {
		if isSpeechPreamble(s[:i]) {
			s = strings.TrimSpace(s[i+1:])
		}
	}
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	s = strings.TrimSpace(s)
	if len([]rune(s)) > speechSummaryMaxOutputChars {
		s = truncateRunes(s, speechSummaryMaxOutputChars)
	}
	// A "summary" that is only punctuation or a stray tag is not worth speaking.
	if strings.TrimFunc(s, func(r rune) bool {
		return r == '.' || r == '-' || r == '*' || r == '`' || r == '"' || r == ' '
	}) == "" {
		return ""
	}
	return s
}

// isSpeechPreamble reports whether a first line is a model-added label rather
// than content. It requires BOTH a trailing colon and one of a small set of
// recognisable shapes, because a plain "…ends with a colon" test deletes real
// prose ("It works: because the limit is 3.") from the spoken summary.
func isSpeechPreamble(line string) bool {
	head := strings.ToLower(strings.TrimSpace(line))
	if !strings.HasSuffix(head, ":") {
		return false
	}
	head = strings.TrimSuffix(head, ":")
	// A bare label: one word, e.g. "Summary:" / "Spoken:".
	if !strings.Contains(head, " ") {
		return len([]rune(head)) > 0
	}
	// A short sentence-shaped lead-in that announces a summary.
	if len([]rune(head)) > 60 {
		return false
	}
	return strings.HasPrefix(head, "here") || strings.Contains(head, "summar")
}

// truncateRunes cuts s to at most n runes, never splitting a multi-byte rune.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	i := 0
	for idx := range s {
		if i == n {
			return s[:idx]
		}
		i++
	}
	return s
}

// speechSummaryModelID identifies the model that produced a summary, so two
// different models never share a cache entry for the same message.
func speechSummaryModelID(client LLMClient) string {
	provider, model := client.GetProvider(), client.GetModel()
	if provider == "" {
		return model
	}
	return provider + "/" + model
}

// speechSummaryCacheDir is the on-disk cache root. It lives under the OS temp
// dir (not the project or the global data dir) because a summary is a
// reproducible convenience, not user data: losing it costs one round trip, and
// the OS is free to reap /tmp at will. Entries are also swept on write (see
// writeSpeechSummaryCache).
func speechSummaryCacheDir() string {
	return filepath.Join(os.TempDir(), "ocode-speech-summaries")
}

// speechSummaryCacheKey derives the entry name from the input text AND the
// model: the same message summarised by two models must not collide, and the
// text is hashed rather than stored so the cache directory never leaks message
// contents into filenames.
func speechSummaryCacheKey(text, modelID string) string {
	sum := sha256.Sum256([]byte(modelID + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

// readSpeechSummaryCache returns a cached summary, or ok=false. Any error
// (missing file, unreadable, empty) is a plain miss — the cache is advisory and
// must never surface to the caller.
func readSpeechSummaryCache(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(speechSummaryCacheDir(), key+".txt"))
	if err != nil {
		return "", false
	}
	// A file older than the TTL is a miss and is left for the sweep.
	if info, statErr := os.Stat(filepath.Join(speechSummaryCacheDir(), key+".txt")); statErr == nil {
		if time.Since(info.ModTime()) > speechSummaryCacheTTL {
			return "", false
		}
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", false
	}
	return s, true
}

// lastSpeechSummaryPrune throttles the sweep. An atomic rather than a mutex
// because it is a single timestamp read on the hot write path.
var lastSpeechSummaryPrune atomic.Int64

// writeSpeechSummaryCache stores a summary and opportunistically sweeps expired
// entries. All failures are silent: a cache that cannot be written must not
// turn a successful summary into a failed one.
func writeSpeechSummaryCache(key, summary string) {
	if key == "" || summary == "" {
		return
	}
	dir := speechSummaryCacheDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, key+".txt"), []byte(summary), 0o600); err != nil {
		return
	}
	now := time.Now()
	last := lastSpeechSummaryPrune.Load()
	if now.Unix()-last < int64(speechSummaryPruneInterval.Seconds()) {
		return
	}
	// CompareAndSwap so exactly one concurrent writer performs the sweep.
	if lastSpeechSummaryPrune.CompareAndSwap(last, now.Unix()) {
		pruneSpeechSummaries(dir, now)
	}
}

// pruneSpeechSummaries removes cache entries older than the TTL, plus any file
// left in a partial state. It is extracted so the sweep is directly testable
// without waiting for the throttle window.
func pruneSpeechSummaries(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > speechSummaryCacheTTL {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
