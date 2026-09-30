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
//
// Kept deliberately short. A long rule list costs the small summariser model
// more attention than the rules earn, and every rule has to win against the
// ones above it; four plain rules carry the same contract as the previous,
// longer form. The bigger-picture opener is deliberately NOT here: it lives in
// speechSummaryRecapClause so it stays gated on message length.
const speechSummarySystemPrompt = `Rewrite a coding assistant's reply so a developer can LISTEN to it instead of reading it.

- Talk about code, never read it: no source, diffs, stack traces, command lines, paths, URLs or tables. Say what changed instead, e.g. "the retry limit is now three".
- Plain spoken sentences only: no markdown, no headings, no bullets, no emoji.
- Two to five short sentences. Keep what matters: decisions, conclusions, caveats, numbers. Name a file by its short name, and only when which file matters.
- If the reply is already short plain prose, return it unchanged. If it is only a question or a request, keep it as that.

Output only the spoken text, with no preamble, labels or quotation marks.`

// speechSummaryRecapClause is appended to the system prompt when, and only
// when, the message is long (speechSummaryRecapMinChars). It is the
// bigger-picture half of the feature: a long reply summarised as a flat
// two-to-five sentences leaves the listener with no way in, so the opening
// sentence answers "what was this whole thing about, and what does it mean for
// me" BEFORE the detail — a listener who stopped halfway still caught the point.
//
// Two wording choices are load-bearing:
//
//   - "the detail must not restate it". Without that, a model treats the opener
//     as sentence one and shrinks the summary to compensate, which loses more
//     detail than the opener gains.
//   - The opener is prose with no label. cleanSpeechSummary strips a recognised
//     "Summary:"-style first line, so a labelled opener would be deleted along
//     with its label — the one sentence the feature exists to guarantee.
const speechSummaryRecapClause = `

This reply is long, so open with the big picture: one sentence on what the whole reply did or decided and what it means for the listener, in words that stand on their own. Then the summary above, whose detail must not restate it. Write it as plain prose, with no label, heading or line break before what follows.`

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
	// speechSummarySkipChars is the length at or below which a message is
	// treated as "already short plain prose" and spoken verbatim, with no
	// summariser call at all. The summariser's own prompt asks the model to
	// return such a message "almost unchanged" (speechSummarySystemPrompt), so
	// the call buys no rewrite and costs a paid round trip plus its latency on
	// what is usually a first, impatient click. The value is deliberately
	// generous — roughly 20 seconds of speech at 160 words per minute — because
	// a reply that short rarely hides a decision worth compressing. Length alone
	// is NOT the test: see speechTextHasSpeakableArtifact.
	speechSummarySkipChars = 400
	// speechSummaryRecapMinChars is the length at or above which the summary
	// must OPEN with a one-line recap of the whole message before the detail
	// (speechSummaryRecapClause). Below it the recap is not requested: a recap
	// of a short reply just restates the summary's only sentence, and a short
	// reply carrying an artifact needs the description, not an orientation
	// line. The value is roughly half a minute of speech at 160 words per
	// minute — long enough that a listener needs orienting, short enough that a
	// two-to-five sentence summary plus the opener still fits. It was lowered
	// from 1200 (about a minute of speech) because by a minute in the listener
	// has usually stopped orienting and is only waiting for the point; 800 is
	// where the point still arrives before the attention is gone. It must stay
	// ABOVE speechSummarySkipChars: at or below that a message is spoken
	// verbatim, so a recap threshold there would be unreachable on length and
	// could only fire for a short artifact message, which is precisely the case
	// that does not want one. TestSpeechSummaryRecapMinCharsSitsAboveTheSkipGate
	// holds that ordering.
	speechSummaryRecapMinChars = 800
	// speechSummaryPromptVersion is salted into the summary cache key. The
	// cache is keyed on the message text and the model id, so a PROMPT change
	// is invisible to it: without a salt, every summary written in the last
	// speechSummaryCacheTTL would keep serving output from the previous prompt
	// (here: no recap) for up to 24 hours. Bump it whenever the prompt or the
	// recap threshold changes; the cost is one paid round trip per recently
	// summarised message, which is the intended trade.
	speechSummaryPromptVersion = "3"
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

// speechSummaryPromptFor returns the system prompt for one message: the base
// prompt, plus the recap clause when the message is long enough to need
// orienting (speechSummaryRecapMinChars). Exported through this helper rather
// than inlined so the length test has exactly one home, and so a future prompt
// variant cannot be added on a path that forgot to ask whether it applies.
func speechSummaryPromptFor(text string) string {
	if len([]rune(text)) < speechSummaryRecapMinChars {
		return speechSummarySystemPrompt
	}
	return speechSummarySystemPrompt + speechSummaryRecapClause
}

// speechTextNeedsRewrite reports whether text is worth a summariser round
// trip. False means "speak it as-is", which SummarizeForSpeech signals by
// returning "" — the same signal every other early exit uses (no client,
// timeout, provider error, empty model output), so no caller needs to know this
// gate exists.
//
// The asymmetry is deliberate. Calling the model on a short message wastes a
// paid round trip; NOT calling it on a short code block or diff reads source
// code aloud, which is the exact failure the summariser was added to prevent.
// So length is only half the test, and the artifact scan errs towards firing.
func speechTextNeedsRewrite(text string) bool {
	if len([]rune(text)) > speechSummarySkipChars {
		return true
	}
	return speechTextHasSpeakableArtifact(text)
}

// speechTextHasSpeakableArtifact reports whether a short message carries
// something that must not be dictated verbatim: code, a diff, a path, a URL, a
// table or a crash trace. It is a cheap scan for syntax fingerprints, not a
// parse — anything that would be read out as gibberish counts.
//
// Two known limits, stated rather than hidden:
//
//   - A table that reached us as RENDERED text has no pipes left to find. The
//     normal path is rendered: web/src/components/Speech/speechUtils.ts takes
//     textContent and the web strips markdown before synthesis, so a short
//     table is read as its bare cell contents. Widening this to "many very
//     short lines" would misread an ordinary bulleted answer, which speaks
//     perfectly well, so it is left out.
//   - The message may be raw markdown source (the at-bottom auto-speak
//     fallback, or a terminal selection), which is why the backtick and pipe
//     checks exist at all: on that path the fences and pipes are still intact.
func speechTextHasSpeakableArtifact(text string) bool {
	// One backtick covers both a fence and inline code.
	if strings.Contains(text, "`") {
		return true
	}
	if strings.Contains(text, "http://") || strings.Contains(text, "https://") {
		return true
	}
	// A diff hunk header.
	if strings.Contains(text, "@@") {
		return true
	}
	// Crash output, which has no useful shape of its own to key on otherwise.
	for _, marker := range []string{
		"panic:",
		"Traceback (most recent call last)",
		"Exception in thread",
		"fatal error:",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if speechLineIsCodeish(line) {
			return true
		}
	}
	return false
}

// speechLineIsCodeish reports whether one line carries a code-shaped
// fingerprint: a diff marker, a shell prompt, a markdown table row, or a
// filesystem path.
func speechLineIsCodeish(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return false
	}
	// A diff marker with NO space after it. Markdown REQUIRES whitespace after
	// "- " / "+ ", so a bare "-foo" is a diff and never a list item — this is
	// what keeps an ordinary bulleted answer from reading as a diff.
	if len(trimmed) >= 2 && (trimmed[0] == '+' || trimmed[0] == '-') && trimmed[1] != ' ' && trimmed[1] != '\t' {
		return true
	}
	if strings.HasPrefix(trimmed, "$ ") {
		return true
	}
	// A table row has at least two cells, hence at least two pipes.
	if strings.Count(trimmed, "|") >= 2 {
		return true
	}
	for _, field := range strings.Fields(trimmed) {
		if speechTokenLooksLikePath(field) {
			return true
		}
	}
	return false
}

// speechTokenLooksLikePath reports whether a whitespace-delimited token names a
// file. It insists on a recognisable extension, because a slash alone is
// ordinary prose: "3/4", "and/or", "on/off" and "input/output" all contain one
// and none of them is a path.
func speechTokenLooksLikePath(tok string) bool {
	// Strip the quoting and sentence punctuation a token picks up when copied
	// out of a sentence, so `internal/x.go`, "main.go" and (parse.go) are judged
	// on content. A leading "~" is deliberately NOT stripped — it is how a
	// home-relative path is spelled.
	tok = strings.Trim(tok, "\"'`()[]{}<>,;:!?")
	tok = strings.TrimRight(tok, ".")
	// "main.go:42" is a stack-trace reference, not a filename with a colon
	// extension. Only a purely numeric tail is dropped, so "C:\src" is safe.
	if i := strings.IndexByte(tok, ':'); i > 1 && isAllDigits(tok[i+1:]) {
		tok = tok[:i]
	}
	if tok == "" {
		return false
	}
	if strings.HasPrefix(tok, "~/") || strings.HasPrefix(tok, "./") || strings.HasPrefix(tok, "../") {
		return true
	}
	if strings.Contains(tok, `\`) {
		// A Windows path or a Go import path. A backslash alone is not
		// conclusive, so require an extension too.
		return strings.Contains(tok, ".")
	}
	if last := strings.LastIndexByte(tok, '/'); last >= 0 {
		// Path shape, so the extension only has to look like one.
		return hasFileExtension(tok[last+1:])
	}
	// A BARE filename ("I edited main.go") is common enough in short replies to
	// be worth catching, but "e.g", "U.S", "1.2.3" and "3.14" are not files. One
	// dot, an alphabetic extension and a stem of at least two characters
	// separate them.
	if dot := strings.LastIndexByte(tok, '.'); strings.Count(tok, ".") == 1 &&
		len(tok)-dot-1 >= 2 && hasFileExtension(tok) {
		return true
	}
	return false
}

// hasFileExtension reports whether s ends in a plausible extension: one dot,
// then up to eight letters, with a non-empty stem. Numeric tails ("3.14",
// "v1.2") fail the letter test, which is what keeps versions out.
func hasFileExtension(s string) bool {
	dot := strings.LastIndexByte(s, '.')
	if dot <= 0 {
		return false
	}
	ext := s[dot+1:]
	if ext == "" || len(ext) > 8 {
		return false
	}
	for i := 0; i < len(ext); i++ {
		c := ext[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// SummarizeForSpeech rewrites text into spoken prose via the speech-summary
// model, caching the result. It returns "" when summarising is impossible or
// pointless (no client, short plain prose, timeout, provider error, empty model
// output) — the caller is expected to fall back to speaking the original text,
// so speech NEVER fails because the summariser did.
func (a *Agent) SummarizeForSpeech(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	// Checked before the client is resolved and before the cache is read: a
	// short plain-prose message has no summary to look up (nothing ever writes
	// one for it) and no client to pay for. See speechTextNeedsRewrite for why
	// short-and-artifact-free, and not merely short, is the bar.
	if !speechTextNeedsRewrite(text) {
		a.emitDebug("SPEECH", fmt.Sprintf("speaking %d chars verbatim; no summary needed", len([]rune(text))))
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
	// Measured on the full text, not the possibly-truncated body: a message
	// long enough to be truncated is over speechSummaryMaxInputChars, which is
	// far above speechSummaryRecapMinChars, so the two can only ever agree.
	// Reading it from the full text keeps the rule "long message, long recap"
	// stated in terms of what the user wrote.
	messages := []Message{
		{Role: "system", Content: speechSummaryPromptFor(text)},
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

// speechSummaryCacheKey derives the entry name from the input text, the model
// AND the prompt version: the same message summarised by two models must not
// collide, and the text is hashed rather than stored so the cache directory
// never leaks message contents into filenames. speechSummaryPromptVersion is in
// the hash because a cache entry is the output of a PROMPT, and a prompt edit
// would otherwise keep replaying the previous prompt's output for the whole TTL.
func speechSummaryCacheKey(text, modelID string) string {
	return speechSummaryCacheKeyFor(text, modelID, speechSummaryPromptVersion)
}

// speechSummaryCacheKeyFor is the version-parameterised form of the key, so a
// test can prove the prompt version is actually hashed in rather than trusting
// the constant to stay wired up. Production always passes
// speechSummaryPromptVersion.
func speechSummaryCacheKeyFor(text, modelID, promptVersion string) string {
	sum := sha256.Sum256([]byte(promptVersion + "\x00" + modelID + "\x00" + text))
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
