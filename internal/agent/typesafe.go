package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrTypesafeDecisionOnly is returned by TypesafeClient.Chat: TypeSafe's
// System One models (Jev) answer typed questions (choice / score / noul) and
// never generate text, so they cannot drive a chat or tool-call loop. Select
// them as the auto-permission judge instead (/permissions model).
var ErrTypesafeDecisionOnly = errors.New("typesafe models are decision-only (no chat); use them as the permission model")

// typesafeRequestTimeout bounds one /systemone round-trip. Jev answers in well
// under a second; the TypeSafe SDK's own per-attempt default is 10s.
const typesafeRequestTimeout = 30 * time.Second

// TypesafeClient talks to the TypeSafe AI System One API
// (POST {BaseURL}/systemone). It satisfies LLMClient so the shared client
// factory, permission-model picker, and config validation can treat
// "typesafe/<model>" like any other provider/model id, but the only useful
// call is Decide.
type TypesafeClient struct {
	APIKey  string
	Model   string
	BaseURL string
}

func newTypesafeClient(apiKey, model, baseURL string) *TypesafeClient {
	return &TypesafeClient{APIKey: apiKey, Model: model, BaseURL: strings.TrimRight(baseURL, "/")}
}

// TypesafeQuestion is one typed question. Type is "choice", "score" or "noul".
// Criteria is the option map for choice questions (label -> description, empty
// description allowed); it is omitted for noul.
type TypesafeQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

// TypesafeAnswer is the union of the three answer shapes; only the fields for
// the question's Type are populated.
type TypesafeAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitzero"`
	Noul          float64            `json:"noul,omitzero"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitzero"`
}

type TypesafeUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type TypesafeResponse struct {
	Model   string                    `json:"model"`
	Answers map[string]TypesafeAnswer `json:"answers"`
	Usage   TypesafeUsage             `json:"usage"`
}

// Decide sends state plus typed questions and returns the typed answers, under
// the fixed typesafeRequestTimeout budget. It is DecideCtx with a Background
// context, kept for the three high-stakes callers (the permission,
// auto-continue and relevance judges) whose stakes justify the 30s wait.
// state may be a string, a JSON object/array, or nil.
func (c *TypesafeClient) Decide(state any, questions map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	return c.DecideCtx(context.Background(), state, questions)
}

// DecideCtx is Decide with a caller-supplied context. The request is built with
// ctx, so a cancelled context aborts before or while sending, and a context
// deadline bounds the round trip — the relevance judge's short
// searchJudgeTimeout budget relies on this. When ctx carries no deadline,
// typesafeRequestTimeout is applied so a Background caller cannot hang forever;
// a caller that supplies its own deadline is never extended beyond it.
//
// state may be a string, a JSON object/array, or nil.
func (c *TypesafeClient) DecideCtx(ctx context.Context, state any, questions map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	if len(questions) == 0 {
		return nil, errors.New("typesafe: at least one question is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, typesafeRequestTimeout)
		defer cancel()
	}
	body, err := json.Marshal(map[string]any{
		"state":     state,
		"model":     c.Model,
		"questions": questions,
	})
	if err != nil {
		return nil, fmt.Errorf("typesafe: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("typesafe: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// No http.Client.Timeout: the request context governs the deadline (with
	// the fallback applied above), so a per-call budget can shorten it and a
	// cancelled context aborts the in-flight request.
	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("typesafe: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("typesafe: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newProviderStatusError("typesafe", resp.StatusCode, string(raw), resp.Header)
	}
	var out TypesafeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("typesafe: decode response: %w", err)
	}
	return &out, nil
}

// Chat always fails: see ErrTypesafeDecisionOnly.
func (c *TypesafeClient) Chat(_ []Message, _ []map[string]any) (*Message, error) {
	return nil, ErrTypesafeDecisionOnly
}

func (c *TypesafeClient) GetProvider() string { return "typesafe" }
func (c *TypesafeClient) GetModel() string    { return c.Model }
