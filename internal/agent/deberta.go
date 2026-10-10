package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// debertaProvider is the provider id of the local DeBERTa-v3 judge. A judge
// model is configured as "deberta/<model>" (for example "deberta/deberta-v3-base-nli"),
// the same way "typesafe/<model>" is configured for Jev.
const debertaProvider = "deberta"

// debertaDefaultURL is where the local judge sidecar listens unless
// OCODE_DEBERTA_URL points elsewhere. See plugins/deberta-judge/server.py.
const debertaDefaultURL = "http://127.0.0.1:8765"

// debertaRequestTimeout bounds one /systemone round trip when the caller
// supplied no deadline. A CPU forward pass per question is slower than Jev's
// hosted round trip, so this is looser than typesafeRequestTimeout's 30s only
// in intent: callers with a tighter budget (the relevance judges) impose their
// own deadline through the context, which this never extends.
const debertaRequestTimeout = 30 * time.Second

// ErrDebertaDecisionOnly is returned by DebertaClient.Chat. The DeBERTa judge is
// a classifier: it answers typed questions and never generates text, so it can
// only be selected as a judge model, never for chat, compaction or small-model
// roles.
var ErrDebertaDecisionOnly = errors.New("deberta models are decision-only (no chat); use them as a judge model")

// DebertaClient is a Decider backed by a local DeBERTa-v3 NLI sidecar. It speaks
// the same System One wire contract as TypesafeClient (state + typed questions in,
// TypesafeResponse out), so the judges and the shared state budget treat it
// identically. The differences are deliberate:
//
//   - No API key. The sidecar runs on the user's machine and is not authenticated;
//     resolveDecider therefore needs no key check for this type.
//   - No fallback. A failing sidecar is a visible error. Silently answering with
//     Jev would make "which model decided this?" unanswerable from the log.
type DebertaClient struct {
	Model   string
	BaseURL string
}

func newDebertaClient(model, baseURL string) *DebertaClient {
	return &DebertaClient{Model: model, BaseURL: strings.TrimRight(baseURL, "/")}
}

// debertaBaseURL returns the sidecar URL: OCODE_DEBERTA_URL when set, else the
// loopback default.
func debertaBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("OCODE_DEBERTA_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return debertaDefaultURL
}

// Decide is DecideCtx with a Background context.
func (c *DebertaClient) Decide(state any, questions map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	return c.DecideCtx(context.Background(), state, questions)
}

// DecideCtx posts one /systemone request to the sidecar. The deadline handling
// matches TypesafeClient.DecideCtx: a caller deadline is respected and never
// extended, and a Background caller gets debertaRequestTimeout.
func (c *DebertaClient) DecideCtx(ctx context.Context, state any, questions map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	if len(questions) == 0 {
		return nil, errors.New("deberta: at least one question is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, debertaRequestTimeout)
		defer cancel()
	}
	body, err := c.marshalRequestBody(state, questions)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("deberta: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("deberta: request to %s failed (is the local judge running? see plugins/deberta-judge): %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("deberta: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newProviderStatusError("deberta", resp.StatusCode, string(raw), resp.Header)
	}
	var out TypesafeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("deberta: decode response: %w", err)
	}
	return &out, nil
}

// marshalRequestBody builds the request. The shared state guard runs first, so an
// over-budget state is refused here exactly as it is for Jev and clef.
func (c *DebertaClient) marshalRequestBody(state any, questions map[string]TypesafeQuestion) ([]byte, error) {
	prepared, _, err := prepareDecisionState(state)
	if err != nil {
		return nil, fmt.Errorf("deberta: %w", err)
	}
	body, err := json.Marshal(map[string]any{
		"model":     c.Model,
		"state":     prepared,
		"questions": questions,
	})
	if err != nil {
		return nil, fmt.Errorf("deberta: marshal request: %w", err)
	}
	return body, nil
}

// Chat always fails: the judge is a classifier, not a chat model.
func (c *DebertaClient) Chat(_ []Message, _ []map[string]any) (*Message, error) {
	return nil, ErrDebertaDecisionOnly
}

func (c *DebertaClient) GetProvider() string { return debertaProvider }
func (c *DebertaClient) GetModel() string    { return c.Model }
