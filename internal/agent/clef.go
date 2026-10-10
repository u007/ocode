package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrClefDecisionOnly is returned by ClefClient.Chat. Clef answers typed questions
// and never generates text, exactly like TypeSafe's System One models, so it must
// never be selected for a chat, compaction or small-model role. The sentinel
// exists so that mistake fails loudly instead of silently producing empty content
// that a caller would treat as a real reply.
var ErrClefDecisionOnly = errors.New("clef models are decision-only (no chat)")

// clefRequestTimeout bounds one /ai/run round trip when the caller supplied no
// deadline of its own.
//
// It is deliberately identical to typesafeRequestTimeout (30s). Cloudflare
// publishes no latency figures for clef, so matching the incumbent is the
// conservative choice: a judge whose timeout moves when a user switches backends
// is a bug that only surfaces after the switch. Callers that care — the relevance
// judges — already impose their own shorter budget via context.
const clefRequestTimeout = 30 * time.Second

// ClefClient talks to a Cloudflare Workers AI decision model
// (@cf/cloudflare/clef or clef-flash).
//
// Clef implements the System One API, so the wire contract is TypesafeQuestion /
// TypesafeResponse and this client deliberately mirrors typesafe.go rather than
// inventing a parallel shape.
//
// It CANNOT reuse GenericClient: clef is served only from the native
// /ai/run/@cf/cloudflare/... endpoint, never from the OpenAI-compatible
// /v1/chat/completions path that every chat provider uses. A GenericClient built
// for a clef model would POST to the chat endpoint and get an error that looks
// like a bad model rather than a wrong endpoint.
type ClefClient struct {
	APIKey    string
	AccountID string
	// Model is the Workers AI model id as it appears in the URL, for example
	// "@cf/cloudflare/clef-flash". The request body needs the shorter selector
	// instead — see clefBodySelector.
	Model string
	// BaseURL is the stored Workers AI credential's OpenAI-compatible base
	// (https://api.cloudflare.com/client/v4/accounts/{id}/ai/v1), carried over
	// from NewClient so clefRunURL can map it onto the native run endpoint. Empty
	// when only an account id is known.
	BaseURL string
	// BaseURLOverride short-circuits clefRunURL so tests can point the client at
	// an httptest server. Test seam only; production always derives the URL.
	BaseURLOverride string
}

func newClefClient(apiKey, accountID, model string) *ClefClient {
	return &ClefClient{APIKey: apiKey, AccountID: accountID, Model: model}
}

// clefRunURL maps the stored Workers AI credential onto clef's native run endpoint.
//
// The stored base is https://api.cloudflare.com/client/v4/accounts/{id}/ai/v1
// (see auth.CloudflareWorkersBaseURL), which is the OpenAI-compatible prefix.
// clef needs /ai/run/@cf/cloudflare/clef-flash instead.
//
// Truncation happens at the "/ai/" marker and the port is deliberately left
// alone. An earlier draft reset the port to "" while trimming, which turned a
// scheme-less "host:port/path" authority into a portless one and hid the very
// variable that decides where the request goes — the same class of bug as the
// loopback-curl work, where a scheme-less host:port reparse silently changed the
// answer.
func clefRunURL(baseURL, accountID, model string) string {
	if baseURL == "" {
		return fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", accountID, model)
	}
	b := strings.TrimRight(baseURL, "/")
	if i := strings.Index(b, "/ai/"); i >= 0 {
		b = b[:i]
	}
	return b + "/ai/run/" + model
}

// Decide is DecideCtx with a Background context, kept for the same
// high-stakes-fail-loudly callers typesafe.go documents.
func (c *ClefClient) Decide(state any, questions map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	return c.DecideCtx(context.Background(), state, questions)
}

// DecideCtx mirrors TypesafeClient.DecideCtx: the same deadline fallback (a
// caller-supplied deadline is never extended), no http.Client.Timeout so the
// context governs, the same 1 MiB response read cap, and the same
// newProviderStatusError on a non-2xx.
//
// It deliberately does NOT fall back to another backend on failure. A silent
// provider switch on the highest-stakes path would make "which model decided
// this?" unanswerable from the log.
func (c *ClefClient) DecideCtx(ctx context.Context, state any, questions map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	if len(questions) == 0 {
		return nil, errors.New("clef: at least one question is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, clefRequestTimeout)
		defer cancel()
	}

	plan := planClefQuestions(questions)
	if plan.dropped > 0 {
		emitDebug("AGENT", fmt.Sprintf("clef: %d of %d questions dropped by the %d-question ceiling; those candidates keep their pre-judge state", plan.dropped, len(questions), clefMaxQuestions))
	}

	body, err := c.buildBody(state, plan.questions)
	if err != nil {
		return nil, err
	}

	url := c.BaseURLOverride
	if url == "" {
		url = clefRunURL(c.BaseURL, c.AccountID, c.Model)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("clef: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("clef: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("clef: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newProviderStatusError(cloudflareWorkersProvider, resp.StatusCode, string(raw), resp.Header)
	}
	out, err := decodeClefResponse(raw)
	if err != nil {
		return nil, err
	}
	// Re-key the answers onto the caller's own ids so every judge keeps reading
	// resp.Answers[<the id it sent>] unchanged, whichever backend answered.
	return plan.demux(out), nil
}

// Chat always fails: see ErrClefDecisionOnly.
func (c *ClefClient) Chat(_ []Message, _ []map[string]any) (*Message, error) {
	return nil, ErrClefDecisionOnly
}

func (c *ClefClient) GetProvider() string { return cloudflareWorkersProvider }
func (c *ClefClient) GetModel() string    { return c.Model }

// buildBody marshals the System One request.
//
// The body-level "model" is Clef's SHORT selector ("clef-flash"), which is a
// different identifier from the URL model id ("@cf/cloudflare/clef-flash").
// Cloudflare's schema constrains the field to ^\s*(clef|clef-flash)\s*$, so
// sending either longer form is rejected — and because isDecisionModel uses the
// same split, getting it wrong also means the backend is never routed at all.
// clefBodySelector takes the last path segment for exactly this reason.
func (c *ClefClient) buildBody(state any, questions map[string]TypesafeQuestion) ([]byte, error) {
	// One shared guard for both backends, so clef and Jev cannot drift apart on
	// what counts as an acceptable state. On refusal this returns an error rather
	// than a trimmed state: the judge then asks the human instead of grading a
	// silently mutilated command.
	prepared, projected, err := prepareDecisionState(state)
	if err != nil {
		return nil, fmt.Errorf("clef: %w", err)
	}
	if projected {
		emitDebug("AGENT", fmt.Sprintf("clef: projected bulky state fields down to fit the %d byte decision budget", decisionStateBudgetBytes))
	}
	body, err := json.Marshal(map[string]any{
		"model":     clefBodySelector(c.Model),
		"state":     prepared,
		"questions": questions,
	})
	if err != nil {
		return nil, fmt.Errorf("clef: marshal request: %w", err)
	}
	return body, nil
}

// Cloudflare's published input schema bounds a clef request two ways: `questions`
// allows at most clefMaxQuestions entries, and each question id may use only
// letters, digits, underscore, dot and hyphen, up to clefMaxIDLen characters.
//
// ocode's natural question keys violate the charset everywhere: the discovery
// judge keys by a doc id like "skill:web-search" or "mcp:github/create_issue", and
// the doc-search and code-search judges key by file path. Without sanitising,
// every clef-backed relevance judge is rejected outright.
const (
	clefMaxQuestions = 64
	clefMaxIDLen     = 100
)

// clefQuestionPlan is the wire-level remapping applied to one request's
// questions, in both directions.
//
// The three maps are deliberately kept as a single unit: questions is what goes
// on the wire, reverse turns an answer back into the caller's own id, and wire is
// the forward view for diagnostics. Trimming one without the others is the bug
// this type exists to make impossible — a surviving reverse entry would let an
// answer for a dropped question be attributed to a live candidate.
type clefQuestionPlan struct {
	// wire maps the caller's original id to the id sent on the wire.
	wire map[string]string
	// reverse maps a wire id back to the caller's original id.
	reverse map[string]string
	// questions is the wire-keyed question map that is actually marshalled.
	questions map[string]TypesafeQuestion
	// dropped counts questions omitted by the ceiling. Their candidates keep
	// their pre-judge state, which is the fail-open direction: a judge may hide a
	// result, never invent one.
	dropped int
}

// planClefQuestions sanitises caller question ids onto clef's legal charset,
// resolves collisions, and applies the ceiling.
//
// Ids are processed in SORTED order. Ranging over the caller's map directly would
// let Go's randomised iteration decide which of two colliding originals wins a
// given wire id, so the same input could produce two different requests — fatal
// for a judge result that is cached, replayed, or compared across runs.
func planClefQuestions(questions map[string]TypesafeQuestion) clefQuestionPlan {
	p := clefQuestionPlan{
		wire:      make(map[string]string, len(questions)),
		reverse:   make(map[string]string, len(questions)),
		questions: make(map[string]TypesafeQuestion, len(questions)),
	}
	if len(questions) == 0 {
		return p
	}
	ids := make([]string, 0, len(questions))
	for id := range questions {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	taken := make(map[string]bool, len(questions))
	for _, id := range ids {
		if len(p.questions) >= clefMaxQuestions {
			// Drop from ALL three maps, not just the wire map: a stale reverse
			// entry is exactly how a dropped candidate acquires a verdict.
			p.dropped++
			continue
		}
		wire := uniqueClefID(sanitiseClefID(id), taken)
		taken[wire] = true
		p.wire[id] = wire
		p.reverse[wire] = id
		p.questions[wire] = questions[id]
	}
	return p
}

// sanitiseClefID rewrites an id onto clef's charset and caps its length.
//
// Every rune outside [A-Za-z0-9_.-] becomes '_'. That substitution is also why
// the length cap can slice bytes rather than runes: the output is pure ASCII by
// construction, so a byte boundary is always a character boundary. A future edit
// that preserved multi-byte characters would invalidate that reasoning, which is
// why the charset test below asserts on the result rather than trusting it.
func sanitiseClefID(id string) string {
	if id == "" {
		// An empty id is illegal (the charset requires 1..100) and would also
		// collide with nothing, so it needs a placeholder rather than "".
		return "_"
	}
	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		if clefLegalRune(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := b.String()
	if len(s) > clefMaxIDLen {
		s = s[:clefMaxIDLen]
	}
	return s
}

// clefLegalRune reports whether r is inside clef's published id charset.
//
// NOTE the deliberate absence of '~'. A tilde-based collision suffix reads as
// harmless in code review but would make every genuinely colliding id illegal,
// turning a silent wrong-verdict risk into a total rejection of that one request
// — and only on the exact inputs the guard exists to handle.
func clefLegalRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '_' || r == '.' || r == '-':
		return true
	}
	return false
}

// uniqueClefID returns a legal id derived from base that no other id has claimed.
//
// The numeric suffix is built by SHORTENING base to make room. Appending to an id
// that is already at the length cap would produce an illegal longer id — and for
// the common case here (two long paths that share a prefix) base is exactly at
// the cap, so that mistake is the default rather than an edge case.
func uniqueClefID(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		suffix := "-" + strconv.Itoa(n)
		// The suffix is ALWAYS appended. Truncating base but forgetting the
		// append yields a candidate equal to the already-taken base, which spins
		// here forever — the collision is only reachable on the exact inputs this
		// guard exists for.
		cand := base + suffix
		if len(cand) > clefMaxIDLen {
			cand = base[:clefMaxIDLen-len(suffix)] + suffix
		}
		if !taken[cand] {
			return cand
		}
	}
}

// demux re-keys a clef response from wire ids back onto the caller's own ids.
//
// Two properties are load-bearing and both are tested:
//
//   - An answer for a wire id that was never sent is DISCARDED. Honouring it
//     would let a backend response inject a verdict about a candidate nobody put
//     to the judge.
//   - A question the backend did not answer stays ABSENT from the map. Absence is
//     the fail-open signal every judge already reads via the two-value form. A
//     materialised zero would be read as a confidence-0 verdict, i.e. the exact
//     silent veto this part exists to prevent.
func (p clefQuestionPlan) demux(resp *TypesafeResponse) *TypesafeResponse {
	out := &TypesafeResponse{Answers: map[string]TypesafeAnswer{}}
	if resp == nil {
		return out
	}
	out.Model = resp.Model
	out.Usage = resp.Usage
	for wire, ans := range resp.Answers {
		orig, ok := p.reverse[wire]
		if !ok {
			continue
		}
		out.Answers[orig] = ans
	}
	return out
}

// decodeClefResponse accepts both shapes Cloudflare's native run endpoint may
// return: the bare System One body ({"answers": ...}) and the standard
// Cloudflare envelope ({"result": {"answers": ...}, "success": true}).
// An envelope reporting success=false, or one with neither shape, is an error:
// an empty Answers map would read as "no verdict" and hide the failure.
func decodeClefResponse(raw []byte) (*TypesafeResponse, error) {
	var env struct {
		Result  *TypesafeResponse `json:"result"`
		Success *bool             `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Answers map[string]TypesafeAnswer `json:"answers"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("clef: decode response: %w", err)
	}
	if env.Success != nil && !*env.Success {
		msgs := make([]string, 0, len(env.Errors))
		for _, e := range env.Errors {
			msgs = append(msgs, e.Message)
		}
		return nil, fmt.Errorf("clef: api reported failure: %s", strings.Join(msgs, "; "))
	}
	if env.Result != nil {
		return env.Result, nil
	}
	if env.Answers == nil {
		return nil, errors.New("clef: response has neither result nor answers")
	}
	var out TypesafeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("clef: decode response: %w", err)
	}
	return &out, nil
}
