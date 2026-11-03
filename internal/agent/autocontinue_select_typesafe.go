package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/crashguard"
)

// todoPromptIDRe matches the todo item id interpolated into a {todo} resume
// prompt. Ids are assigned as t<N> (tool.assignTodoIDs) and substituted bare.
var todoPromptIDRe = regexp.MustCompile(`^t[0-9]+$`)

// autoContinueTranscriptCap bounds the transcript the verdict call sees. At or
// under it every non-empty message is sent and no selection call is made. Over
// it, Jev scores the messages in batches of the same size, so selection costs
// one call per batch and the number of calls grows with the transcript.
const autoContinueTranscriptCap = 20

// autoContinueSelectConcurrency bounds how many selection batches run at once.
const autoContinueSelectConcurrency = 4

// autoContinueSelectMaxBatches bounds the selection work one turn end may cost.
// Beyond it the OLDEST unpinned messages are left out of the selection without
// being scored: the newest messages bear on the current step, and a chain that
// has grown to hundreds of messages must not multiply the judge bill every turn.
// The messages left out are reported in the verdict state (transcript_selection),
// so the drop is signalled rather than silent.
const autoContinueSelectMaxBatches = 8

// autoContinueSelectTimeout is the deadline for one selection batch: the same
// budget a single Decide call gets everywhere else.
const autoContinueSelectTimeout = typesafeRequestTimeout

// autoContinueSelectMinNoul is the floor for a selection answer: Jev's noul, the
// probability that a message bears on the next step. It is deliberately its own
// constant, not the permission or verdict floor. A dropped message hides context
// the verdict needs, while a kept one only costs tokens, so the bar is "more
// likely relevant than not".
const autoContinueSelectMinNoul = 0.5

// autoContinueSelectCacheCap bounds the score cache. On overflow the cache is
// emptied and rebuilt by later calls, so it cannot grow without limit.
const autoContinueSelectCacheCap = 2000

// typesafeScore is one message's cached selection outcome. Only scored
// outcomes are cached: a fail-open unscored message is never stored, so the
// next call asks Jev about it again.
type typesafeScore struct {
	keep   bool
	score  float64
	scored bool
}

// typesafeScoreCache remembers each message's selection outcome across turn
// ends, so a turn end scores only the messages it has not seen. Entries are keyed
// by typesafeMessageKey (content, not index: compaction rebases the transcript).
// They are valid only for the original request in scope they were scored under.
// A new human turn changes scope, so a lookup under another scope misses, and
// the stale entries are replaced only by a successful store.
type typesafeScoreCache struct {
	mu      sync.Mutex
	scope   string
	entries map[string]typesafeScore
}

// lookup returns the cached outcome for key under scope.
func (c *typesafeScoreCache) lookup(scope, key string) (typesafeScore, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.scope != scope {
		return typesafeScore{}, false
	}
	s, ok := c.entries[key]
	return s, ok
}

// store records the outcomes of one fully successful selection under scope. A
// new scope, or a store that would overflow autoContinueSelectCacheCap, starts
// from an empty map.
func (c *typesafeScoreCache) store(scope string, fresh map[string]typesafeScore) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil || c.scope != scope || len(c.entries)+len(fresh) > autoContinueSelectCacheCap {
		c.entries = make(map[string]typesafeScore, len(fresh))
		c.scope = scope
	}
	for k, s := range fresh {
		c.entries[k] = s
	}
}

// clear empties the cache.
func (c *typesafeScoreCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scope = ""
	c.entries = nil
}

// ResetSelectScores empties the selection score cache. The TUI calls it where it
// starts a new session on the same agent (/new, /clear). Scope already keeps a
// new chain from reusing old scores, so this only releases the memory.
func (a *Agent) ResetSelectScores() {
	a.selectScores.clear()
}

// typesafeHash is the sha256 hex of role, a NUL separator and the trimmed
// content. It keys the score cache, both per message and per original request.
func typesafeHash(role, content string) string {
	sum := sha256.Sum256([]byte(role + "\x00" + strings.TrimSpace(content)))
	return hex.EncodeToString(sum[:])
}

// typesafeSelected is one transcript message and its selection outcome. Pinned
// messages (the original request and the last assistant reply) are always kept
// and never scored. A scored message carries Jev's noul in score. An unscored
// message was kept by fail-open (missing or untrustworthy answer) and is never
// dropped for budget, because ranking it as 0 would drop it first.
type typesafeSelected struct {
	msg    Message
	pinned bool
	keep   bool
	scored bool
	score  float64
}

// typesafeNonEmpty returns the messages whose content is not blank, in order.
// The cap in autoContinueTranscriptCap counts these.
func typesafeNonEmpty(messages []Message) []Message {
	out := make([]Message, 0, len(messages))
	for _, m := range messages {
		if strings.TrimSpace(m.Content) != "" {
			out = append(out, m)
		}
	}
	return out
}

// selectTypesafeTranscript returns the messages the verdict call sees, in
// original order. transcript must already be non-empty-filtered. At or under
// autoContinueTranscriptCap everything is kept with no call. Over it the
// unpinned messages are scored by client, the verdict's own decision backend,
// and only the kept ones are returned. A selection error means nothing was
// selected, so the caller must not judge.
func (a *Agent) selectTypesafeTranscript(client Decider, transcript []Message) ([]typesafeSelected, error) {
	origIdx := typesafeOriginalRequestIndex(transcript)
	lastIdx := lastAssistantIndex(transcript)
	sel := make([]typesafeSelected, len(transcript))
	for i, m := range transcript {
		sel[i] = typesafeSelected{msg: m, pinned: i == origIdx || i == lastIdx, keep: true}
	}
	if len(transcript) > autoContinueTranscriptCap {
		if err := a.scoreSelectedTranscript(client, transcript, sel, origIdx, lastIdx); err != nil {
			return nil, err
		}
	}
	kept := make([]typesafeSelected, 0, len(sel))
	for _, s := range sel {
		if s.keep {
			kept = append(kept, s)
		}
	}
	return kept, nil
}

// scoreSelectedTranscript asks client, batch by batch, how relevant each unpinned
// message is to the original request and the last reply, and records the outcome
// on sel. Messages already in the score cache are applied directly and not sent.
// Batches run concurrently, each under its own deadline, and all of them stop
// when the agent is stopped (Cancel). Any batch error or stop fails the whole
// selection, and no partial result is used or cached.
func (a *Agent) scoreSelectedTranscript(client Decider, transcript []Message, sel []typesafeSelected, origIdx, lastIdx int) error {
	parent, cancelParent := stopChContext(a.StopCh())
	defer cancelParent()
	// Messages scored under another original request or another last reply are
	// not reused: the lookup misses on scope, and only a successful store below
	// replaces the entries.
	origContent, lastContent := "", ""
	if origIdx >= 0 {
		origContent = transcript[origIdx].Content
	}
	if lastIdx >= 0 {
		lastContent = transcript[lastIdx].Content
	}
	scopeKey := typesafeHash("user", origContent) + typesafeHash("assistant", lastContent)
	keys := make([]string, len(sel))
	var scoreable []int
	for i := range sel {
		if sel[i].pinned {
			continue
		}
		keys[i] = typesafeHash(transcript[i].Role, transcript[i].Content)
		if s, ok := a.selectScores.lookup(scopeKey, keys[i]); ok {
			sel[i].keep, sel[i].scored, sel[i].score = s.keep, s.scored, s.score
			continue
		}
		scoreable = append(scoreable, i)
	}
	// Bound the work one turn end may cost. The newest messages are the ones that
	// bear on the current step, so the OLDEST beyond the batch ceiling are left
	// out of the selection unscored. The verdict state reports the total dropped
	// (transcript_selection), so they are not removed silently.
	if maxScoreable := autoContinueSelectMaxBatches * autoContinueTranscriptCap; len(scoreable) > maxScoreable {
		over := len(scoreable) - maxScoreable
		for _, i := range scoreable[:over] {
			sel[i].keep = false
		}
		scoreable = scoreable[over:]
	}
	original, lastReply := "", ""
	if origIdx >= 0 {
		original = typesafeCapContent(strings.TrimSpace(transcript[origIdx].Content))
	}
	if lastIdx >= 0 {
		lastReply = typesafeCapContent(strings.TrimSpace(transcript[lastIdx].Content))
	}

	var batches [][]int
	for start := 0; start < len(scoreable); start += autoContinueTranscriptCap {
		batches = append(batches, scoreable[start:min(start+autoContinueTranscriptCap, len(scoreable))])
	}
	results := make([]selectBatchResult, len(batches))
	sem := make(chan struct{}, autoContinueSelectConcurrency)
	var wg sync.WaitGroup
launch:
	for b := range batches {
		select {
		case <-parent.Done():
			break launch
		case sem <- struct{}{}:
		}
		wg.Add(1)
		idx := b
		crashguard.Go(func() {
			defer wg.Done()
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(parent, autoContinueSelectTimeout)
			defer cancel()
			results[idx] = a.judgeSelectBatch(ctx, client, transcript, batches[idx], original, lastReply)
			// One retry per batch: a single transient backend failure must not
			// switch auto-continue off for the turn. No retry when the batch ran
			// out of time or the agent is stopping — there is nothing to retry
			// with, and the whole selection fails closed in either case.
			if results[idx].err != nil && ctx.Err() == nil && parent.Err() == nil {
				a.emitDebug("AUTOCONTINUE", fmt.Sprintf("autocontinue_select retrying batch after: %v", results[idx].err))
				results[idx] = a.judgeSelectBatch(ctx, client, transcript, batches[idx], original, lastReply)
			}
		})
	}
	wg.Wait()

	// A stop after the batches answered still voids the selection: nothing
	// scored while the agent was stopping may be used or cached.
	if err := parent.Err(); err != nil {
		return fmt.Errorf("selection stopped: %w", err)
	}
	for _, r := range results {
		if r.err != nil {
			return r.err
		}
	}
	// Only now, with every batch answered, is anything cached. A message left
	// unscored by fail-open is not stored, so the next call retries it.
	fresh := make(map[string]typesafeScore, len(scoreable))
	for b, batch := range batches {
		for _, i := range batch {
			id := selectMessageID(i)
			sel[i].keep = results[b].keep[id]
			if score, ok := results[b].scores[id]; ok {
				sel[i].scored = true
				sel[i].score = score
				fresh[keys[i]] = typesafeScore{keep: sel[i].keep, score: score, scored: true}
			}
		}
	}
	a.selectScores.store(scopeKey, fresh)
	return nil
}

// selectBatchResult is one batch's outcome, keyed by selectMessageID.
type selectBatchResult struct {
	keep   map[string]bool
	scores map[string]float64
	err    error
}

// judgeSelectBatch sends one batch as a single Decide call with one noul question
// per message. The state carries the original request and the last reply so Jev
// judges each message against them, not in isolation.
func (a *Agent) judgeSelectBatch(ctx context.Context, client Decider, transcript []Message, batch []int, original, lastReply string) selectBatchResult {
	ids := make([]string, 0, len(batch))
	entries := make([]map[string]any, 0, len(batch))
	questions := make(map[string]TypesafeQuestion, len(batch))
	for _, i := range batch {
		id := selectMessageID(i)
		ids = append(ids, id)
		entries = append(entries, map[string]any{
			"id":      id,
			"role":    transcript[i].Role,
			"content": typesafeCapContent(strings.TrimSpace(transcript[i].Content)),
		})
		questions[id] = TypesafeQuestion{Type: "noul", Instructions: autoContinueSelectInstructions(id)}
	}
	state := map[string]any{
		"original_request": original,
		"last_reply":       lastReply,
		"messages":         entries,
	}
	keep, scores, err := a.judgeNoulCandidates(ctx, client, "AUTOCONTINUE", "autocontinue_select", ids, state, questions, autoContinueSelectMinNoul)
	return selectBatchResult{keep: keep, scores: scores, err: err}
}

// selectMessageID names a transcript message in the selection request. It is the
// message's index in the transcript, so ids stay unique across batches.
func selectMessageID(i int) string {
	return fmt.Sprintf("m%d", i)
}

// autoContinueSelectInstructions is the per-message noul question.
func autoContinueSelectInstructions(id string) string {
	return fmt.Sprintf(`You are selecting the context an AI coding assistant needs to decide its next step. The state holds the user's original_request, the assistant's last_reply, and a batch of earlier conversation messages (messages).
Decide how relevant the message with id "%s" is to deciding what should happen next, given original_request.
Answer yes when the message bears on that request: a requirement, decision, result, error, file or state change, or open question the next step depends on.
Answer no when it is unrelated to the request or is superseded routine chatter.`, id)
}

// fitTypesafeTranscript returns the transcript to send. While the state built
// from it is over decisionStateBudgetBytes, it drops the lowest-scoring scored
// message (ties go to the earlier message). The measure is the whole state
// (todo items, turn_ended and the rest), not only the tail, and it is the
// marshalled size, the same one prepareDecisionState checks. Pinned and unscored
// messages are never dropped. If they alone exceed the budget the state goes over
// budget, and prepareDecisionState projects it, signalled through _projection, or
// refuses it.
//
// Dropping k messages always removes the same k in the same order, so the answer
// is the smallest k whose state fits. Dropping more never grows the marshalled
// state, so the search is a binary search over k: O(log n) builds, not one per
// dropped message.
func fitTypesafeTranscript(kept []typesafeSelected, build func([]Message) map[string]any) ([]Message, error) {
	var order []int
	for i, s := range kept {
		if !s.pinned && s.scored {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(x, y int) bool { return kept[order[x]].score < kept[order[y]].score })

	dropFirst := func(k int) []Message {
		gone := make(map[int]bool, k)
		for _, i := range order[:k] {
			gone[i] = true
		}
		tail := make([]Message, 0, len(kept)-k)
		for i, s := range kept {
			if !gone[i] {
				tail = append(tail, s.msg)
			}
		}
		return tail
	}
	fits := func(k int) (bool, error) {
		size, err := marshalledDecisionStateSize(build(dropFirst(k)))
		if err != nil {
			return false, err
		}
		return size <= decisionStateBudgetBytes, nil
	}

	lo, hi := 0, len(order)
	for lo < hi {
		mid := lo + (hi-lo)/2
		ok, err := fits(mid)
		if err != nil {
			return nil, err
		}
		if ok {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	// lo is the smallest fitting k, or len(order) when no k fits.
	return dropFirst(lo), nil
}

// typesafeOriginalRequestIndex returns the index of the message that opened the
// current auto-continue chain: the most recent genuine human message. Resume
// prompts and [ocode:...] injections are skipped. A human turn starts a new chain,
// so the scan runs backwards. A forward scan would return the session's first
// request, which is stale in any long session. Returns -1 when there is none.
func typesafeOriginalRequestIndex(transcript []Message) int {
	for i := len(transcript) - 1; i >= 0; i-- {
		m := transcript[i]
		if m.Role != "user" {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" || strings.HasPrefix(content, "[ocode:") || isAutoContinueResumeText(content) {
			continue
		}
		return i
	}
	return -1
}

// isAutoContinueResumeText reports whether content is one of the fixed resume
// prompts the harness sends: every autoContinueActions prompt and the step-limit
// prompt. A {todo} prompt is matched by the fixed text on either side of the
// placeholder plus a todo item id (t<N>) in the placeholder's place, since the
// id is interpolated before sending. Prose that merely shares the fixed text
// does not match.
func isAutoContinueResumeText(content string) bool {
	if content == AutoContinueStepLimitPrompt {
		return true
	}
	for _, act := range autoContinueActions {
		if act.Prompt == "" {
			continue
		}
		head, tail, templated := strings.Cut(act.Prompt, "{todo}")
		if !templated {
			if content == act.Prompt {
				return true
			}
			continue
		}
		if len(content) < len(head)+len(tail) || !strings.HasPrefix(content, head) || !strings.HasSuffix(content, tail) {
			continue
		}
		if todoPromptIDRe.MatchString(content[len(head) : len(content)-len(tail)]) {
			return true
		}
	}
	return false
}

// lastAssistantIndex returns the index of the last assistant message, or -1.
func lastAssistantIndex(transcript []Message) int {
	for i := len(transcript) - 1; i >= 0; i-- {
		if transcript[i].Role == "assistant" {
			return i
		}
	}
	return -1
}
