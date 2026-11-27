package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/u007/ocode/internal/crashguard"
)

// Explainer run limits. These are package vars, not constants, so a test can
// shorten a deadline and prove the cancellation path actually fires. Their
// values are the contract: see docs/superpowers/specs/2026-11-25-permission-command-explainer-design.md section 8.
var (
	// toolDigestRunTimeout bounds one explanation end to end, queue time included.
	toolDigestRunTimeout = 80 * time.Second
	// toolDigestModelCallTimeout bounds one direct side-client request.
	toolDigestModelCallTimeout = 25 * time.Second
	// toolDigestModelSteps bounds model round trips, call_parameters included.
	toolDigestModelSteps = 10
	// toolDigestInspectionCalls bounds read/glob/grep/list/inspect_shell calls.
	toolDigestInspectionCalls = 12
	// toolDigestParameterPageBytes is the window of the first page in the
	// initial request, when the context budget admits it.
	toolDigestParameterPageBytes = 8192
	// toolDigestEvidenceResultBytes caps one masked evidence result.
	toolDigestEvidenceResultBytes = 8192
	// toolDigestEvidenceTotalBytes caps cumulative masked evidence per run.
	toolDigestEvidenceTotalBytes = 40960
	// toolDigestMaxConcurrent is the process-wide run semaphore size.
	toolDigestMaxConcurrent = 3
	// toolDigestCacheTTL is how long a successful digest is reused.
	toolDigestCacheTTL = 2 * time.Minute
	// toolDigestCacheEntries is the success-cache capacity (LRU).
	toolDigestCacheEntries = 128
)

// toolDigestEvidenceMarker is appended to an evidence result that was cut.
const toolDigestEvidenceMarker = "\n[evidence truncated]"

// toolDigestFinalStepMessage closes inspection before the last model request.
// Without it, a model that keeps calling tools burns the final step and loses
// all the evidence it gathered to the step-cap error.
const toolDigestFinalStepMessage = "No more inspections. Answer now in Markdown from the evidence above; name any unknown or uninspected bytes instead of calling another tool."

// toolDigestEvidenceScanBytes bounds how much of one raw result is masked. The
// masker costs roughly a microsecond per byte, so an unbounded megabyte result
// would consume the whole run budget. The window is eight times the per-result
// cap, so the retained output comes from the first part of the result, and
// bytes beyond the window were never retained.
const toolDigestEvidenceScanBytes = 8 * 8192

// toolDigestInspectionTools are the project/shell inspection names. Calls to
// them count against toolDigestInspectionCalls whether or not the tool is
// registered in the toolbox.
var toolDigestInspectionTools = map[string]bool{
	"read":          true,
	"glob":          true,
	"grep":          true,
	"list":          true,
	"inspect_shell": true,
}

// toolDigestPageResult is the JSON body of one call_parameters result. Text is
// the exact masked canonical bytes of the window.
type toolDigestPageResult struct {
	OffsetBytes     int    `json:"offset_bytes"`
	NextOffsetBytes int    `json:"next_offset_bytes"`
	TotalBytes      int    `json:"total_bytes"`
	SHA256          string `json:"sha256"`
	RemainingBytes  int    `json:"remaining_bytes"`
	Text            string `json:"text"`
}

// toolDigestMessagesKey carries the loop's live message slice to the tools, so
// call_parameters can admit a page against the real request it would join.
type toolDigestMessagesKey struct{}

func withToolDigestMessages(ctx context.Context, msgs *[]Message) context.Context {
	return context.WithValue(ctx, toolDigestMessagesKey{}, msgs)
}

func toolDigestMessagesFromContext(ctx context.Context) []Message {
	if p, ok := ctx.Value(toolDigestMessagesKey{}).(*[]Message); ok && p != nil {
		return append([]Message(nil), (*p)...)
	}
	return nil
}

// runToolDigestLoop runs one explanation as a bounded tool loop over toolbox.
// It owns toolbox: every registered cleanup runs on success, error and
// cancellation. initial holds the caller's user-role messages; the system prompt
// is prepended here. Only the messages of this run are ever sent.
func runToolDigestLoop(ctx context.Context, client LLMClient, toolbox *toolDigestToolbox, systemPrompt string, initial []Message, guard toolDigestContextGuard, usage func(promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens int64, spend *float64), in ToolCallDigestInput) (ToolDigest, error) {
	defer toolbox.Close()
	if client == nil {
		return ToolDigest{}, errors.New("no model is available to explain this call")
	}
	model := modelIDForClient(client)
	if isDecisionModel(model) {
		return ToolDigest{}, fmt.Errorf("model %q is decision-only and cannot explain %s calls", model, in.Tool)
	}
	ctx, cancel := context.WithTimeout(ctx, toolDigestRunTimeout)
	defer cancel()

	messages := make([]Message, 0, len(initial)+1)
	messages = append(messages, Message{Role: "system", Content: systemPrompt})
	messages = append(messages, initial...)
	toolCtx := withToolDigestMessages(ctx, &messages)
	state := &toolDigestRunState{toolbox: toolbox}
	defs := toolbox.Definitions()

	for step := 1; step <= toolDigestModelSteps; step++ {
		if step == toolDigestModelSteps {
			messages = append(messages, Message{Role: "user", Content: toolDigestFinalStepMessage})
		}
		if err := guard.admit(messages); err != nil {
			return ToolDigest{}, fmt.Errorf("%w; the next model request was not sent", err)
		}
		resp, err := callToolDigestModel(ctx, client, messages, defs)
		if err != nil {
			return ToolDigest{}, err
		}
		if usage != nil && resp.Usage != nil {
			recordToolDigestUsage(usage, resp)
		}
		if len(resp.ToolCalls) == 0 {
			summary := cleanToolDigest(resp.Content)
			if summary == "" {
				return ToolDigest{}, errors.New("the model returned an empty explanation")
			}
			return ToolDigest{Summary: summary, Model: model}, nil
		}
		messages = append(messages, Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})
		for _, call := range resp.ToolCalls {
			messages = append(messages, Message{Role: "tool", ToolID: call.ID, Content: state.dispatch(toolCtx, call)})
		}
	}
	return ToolDigest{}, fmt.Errorf("explanation stopped after %d model steps without a final answer", toolDigestModelSteps)
}

// toolDigestRunState is the per-run budget: inspection calls and cumulative
// masked evidence bytes. It is touched only by the loop goroutine.
type toolDigestRunState struct {
	toolbox       *toolDigestToolbox
	inspections   int
	evidenceBytes int
}

// dispatch executes one model tool call and returns the tool result the model
// reads. Budget exhaustion, refusals and unknown names all return a tool result
// that tells the model to answer; only the model-step cap ends the run.
func (s *toolDigestRunState) dispatch(ctx context.Context, call ToolCall) string {
	name := call.Function.Name
	args := json.RawMessage(call.Function.Arguments)
	if !toolDigestInspectionTools[name] {
		out, err := s.toolbox.Execute(ctx, name, args)
		if err != nil {
			return "tool error: " + err.Error()
		}
		return out
	}
	if s.inspections >= toolDigestInspectionCalls {
		return fmt.Sprintf("tool error: the %d-inspection budget is spent; answer from the evidence already gathered", toolDigestInspectionCalls)
	}
	s.inspections++
	if s.evidenceBytes >= toolDigestEvidenceTotalBytes {
		return fmt.Sprintf("tool error: the %d-byte evidence budget is spent; answer from the evidence already gathered", toolDigestEvidenceTotalBytes)
	}
	out, err := s.toolbox.Execute(ctx, name, args)
	if err != nil {
		return "tool error: " + err.Error()
	}
	if len(out) > toolDigestEvidenceScanBytes {
		out = cutToolDigestText(out, toolDigestEvidenceScanBytes)
	}
	return s.capEvidence(maskSecretsForDigest(out))
}

// cutToolDigestText keeps the first n bytes of text, shortened to a UTF-8
// boundary.
func cutToolDigestText(text string, n int) string {
	if len(text) <= n {
		return text
	}
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return text[:n]
}

// capEvidence cuts one masked result to the per-result cap and the cumulative
// remaining budget, on a UTF-8 boundary, and records the bytes it keeps.
func (s *toolDigestRunState) capEvidence(text string) string {
	limit := toolDigestEvidenceResultBytes
	if room := toolDigestEvidenceTotalBytes - s.evidenceBytes; room < limit {
		limit = room
	}
	if len(text) > limit {
		marker := toolDigestEvidenceMarker
		if limit < len(marker) {
			marker = marker[:limit]
		}
		text = cutToolDigestText(text, limit-len(marker)) + marker
	}
	s.evidenceBytes += len(text)
	return text
}

// callToolDigestModel makes one direct side-client request under the per-call
// deadline. The request runs in its own goroutine so a client that ignores its
// context cannot hold the run past the deadline.
func callToolDigestModel(ctx context.Context, client LLMClient, messages []Message, defs []map[string]interface{}) (*Message, error) {
	callCtx, cancel := context.WithTimeout(ctx, toolDigestModelCallTimeout)
	defer cancel()
	type reply struct {
		msg *Message
		err error
	}
	done := make(chan reply, 1)
	crashguard.Go(func() {
		msg, err := chatToolDigestWithContext(callCtx, client, messages, defs)
		done <- reply{msg: msg, err: err}
	})
	select {
	case r := <-done:
		if r.err != nil {
			return nil, fmt.Errorf("explanation model call failed: %w", r.err)
		}
		if r.msg == nil {
			return nil, errors.New("explanation model call returned no message")
		}
		return r.msg, nil
	case <-callCtx.Done():
		if ctx.Err() != nil {
			return nil, toolDigestRunEndError(ctx)
		}
		return nil, fmt.Errorf("explanation model call timed out after %s", toolDigestModelCallTimeout)
	}
}

// chatToolDigestWithContext is chatWithOptionalContext with the tool catalog.
func chatToolDigestWithContext(ctx context.Context, client LLMClient, messages []Message, defs []map[string]interface{}) (*Message, error) {
	if cc, ok := client.(contextChatter); ok {
		return cc.ChatWithContext(ctx, messages, defs)
	}
	return client.Chat(messages, defs)
}

// toolDigestRunEndError names why the run context ended: the overall deadline
// or a caller cancel.
func toolDigestRunEndError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("explain timed out after %ds", int(toolDigestRunTimeout.Seconds()))
	}
	return errors.New("explain cancelled")
}

// recordToolDigestUsage reports one response's usage, pricing it the way
// Agent.RecordSideUsageFromMessage does.
func recordToolDigestUsage(usage func(int64, int64, int64, int64, *float64), msg *Message) {
	var pt, ct, crt, cwt int64
	if msg.Usage.PromptTokens != nil {
		pt = *msg.Usage.PromptTokens
	}
	if msg.Usage.CompletionTokens != nil {
		ct = *msg.Usage.CompletionTokens
	}
	if msg.Usage.CacheReadTokens != nil {
		crt = *msg.Usage.CacheReadTokens
	}
	if msg.Usage.CacheWriteTokens != nil {
		cwt = *msg.Usage.CacheWriteTokens
	}
	var spend *float64
	if msg.Model != "" {
		spend = msg.Usage.Spend(msg.Model)
	}
	usage(pt, ct, crt, cwt, spend)
}

// newToolDigestRunnerToolbox returns the runner's toolbox: call_parameters, then
// the wrapped read, glob, grep and list tools, then inspect_shell when the
// mandatory sandbox is available.
func newToolDigestRunnerToolbox(snap toolDigestSnapshot, payload *toolDigestPayload, guard toolDigestContextGuard) (*toolDigestToolbox, error) {
	if payload == nil {
		return nil, errors.New("tool digest toolbox: payload is required")
	}
	box := &toolDigestToolbox{}
	box.add(newToolDigestParamsTool(payload, guard))
	if err := registerToolDigestFileTools(box, snap); err != nil {
		return nil, err
	}
	if err := registerInspectShellTool(box, snap); err != nil {
		return nil, err
	}
	return box, nil
}

// toolDigestParamsOf returns the run's call_parameters tool, so the caller can
// record the initial page as covered.
func toolDigestParamsOf(box *toolDigestToolbox) *toolDigestParamsTool {
	p, _ := box.tools["call_parameters"].(*toolDigestParamsTool)
	return p
}

// toolDigestParamsTool pages the run's masked canonical payload. It keeps the
// byte ranges it has delivered, so an over-budget refusal can name what is still
// uninspected.
type toolDigestParamsTool struct {
	payload *toolDigestPayload
	guard   toolDigestContextGuard

	mu      sync.Mutex
	covered []toolDigestRange
}

// toolDigestRange is a half-open byte range [start, end).
type toolDigestRange struct{ start, end int }

func newToolDigestParamsTool(payload *toolDigestPayload, guard toolDigestContextGuard) *toolDigestParamsTool {
	return &toolDigestParamsTool{payload: payload, guard: guard}
}

func (t *toolDigestParamsTool) Name() string { return "call_parameters" }

func (t *toolDigestParamsTool) Description() string {
	return "Read one exact window of the pending call's masked parameters. Returns offset_bytes, next_offset_bytes, total_bytes, sha256, remaining_bytes and the exact page text."
}

func (t *toolDigestParamsTool) Parallel() bool { return false }

func (t *toolDigestParamsTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        t.Name(),
		"description": t.Description(),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"offset_bytes": map[string]interface{}{
					"type":        "integer",
					"minimum":     0,
					"description": "Byte offset into the canonical payload; must start a UTF-8 character.",
				},
				"max_bytes": map[string]interface{}{
					"type":        "integer",
					"minimum":     1,
					"maximum":     toolDigestMaxPageBytes,
					"description": "Window size in bytes, at most 8192.",
				},
			},
			"required":             []string{"offset_bytes", "max_bytes"},
			"additionalProperties": false,
		},
	}
}

func (t *toolDigestParamsTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

// ExecuteCtx validates the window, returns the exact page, and refuses a page
// that would not fit the context budget. A refused page is neither appended nor
// clipped.
func (t *toolDigestParamsTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		OffsetBytes *int `json:"offset_bytes"`
		MaxBytes    *int `json:"max_bytes"`
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return "", fmt.Errorf("call_parameters: invalid arguments: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", errors.New("call_parameters: trailing data after the arguments")
	}
	if in.OffsetBytes == nil || in.MaxBytes == nil {
		return "", errors.New("call_parameters: offset_bytes and max_bytes are both required")
	}
	page, err := t.payload.page(*in.OffsetBytes, *in.MaxBytes)
	if err != nil {
		return "", fmt.Errorf("call_parameters: %w", err)
	}
	body, err := toolDigestEncode(toolDigestPageResult{
		OffsetBytes:     page.offset,
		NextOffsetBytes: page.nextOffset,
		TotalBytes:      page.totalBytes,
		SHA256:          page.sha256,
		RemainingBytes:  page.remainingBytes,
		Text:            page.text,
	})
	if err != nil {
		return "", fmt.Errorf("call_parameters: encode page: %w", err)
	}
	candidate := append(toolDigestMessagesFromContext(ctx), Message{Role: "tool", Content: string(body)})
	if err := t.guard.admit(candidate); err != nil {
		return "", fmt.Errorf("call_parameters: %w; requested bytes [%d,%d) were not appended; uninspected bytes %s; request a smaller max_bytes", err, page.offset, page.nextOffset, t.uninspected(page.totalBytes))
	}
	t.markCovered(page.offset, page.nextOffset)
	return string(body), nil
}

// markCovered records a delivered range.
func (t *toolDigestParamsTool) markCovered(start, end int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.covered = append(t.covered, toolDigestRange{start: start, end: end})
}

// uninspected lists the byte ranges of [0,total) not yet delivered, in order.
func (t *toolDigestParamsTool) uninspected(total int) string {
	t.mu.Lock()
	ranges := append([]toolDigestRange(nil), t.covered...)
	t.mu.Unlock()
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	var b strings.Builder
	pos := 0
	for _, r := range ranges {
		if r.start > pos {
			fmt.Fprintf(&b, "[%d,%d) ", pos, r.start)
		}
		if r.end > pos {
			pos = r.end
		}
	}
	if pos < total {
		fmt.Fprintf(&b, "[%d,%d) ", pos, total)
	}
	if b.Len() == 0 {
		return "none"
	}
	return strings.TrimSpace(b.String())
}

// buildToolDigestInitialMessages returns the first user message: the manifest,
// the paging rules and, only when the budget admits it, the first exact page.
// The initial page is recorded as covered on params.
func buildToolDigestInitialMessages(systemPrompt string, payload *toolDigestPayload, guard toolDigestContextGuard, params *toolDigestParamsTool) ([]Message, error) {
	manifest := toolDigestManifest(payload)
	page, err := payload.page(0, toolDigestParameterPageBytes)
	if err != nil {
		return nil, fmt.Errorf("tool digest: first parameter page: %w", err)
	}
	withPage := Message{Role: "user", Content: manifest + "\n\n" + toolDigestPageBlock(page)}
	system := Message{Role: "system", Content: systemPrompt}
	if guard.admit([]Message{system, withPage}) == nil {
		params.markCovered(page.offset, page.nextOffset)
		return []Message{withPage}, nil
	}
	noPage := Message{Role: "user", Content: manifest + "\n\nNo parameter page was loaded: the context-window budget does not fit the first page. Request a smaller window with call_parameters (for example max_bytes 1024)."}
	if err := guard.admit([]Message{system, noPage}); err != nil {
		return nil, fmt.Errorf("%w; the manifest alone does not fit", err)
	}
	return []Message{noPage}, nil
}

// toolDigestManifest describes the masked payload by its exact bytes, not its
// values: total length, SHA-256 and each field's length.
func toolDigestManifest(p *toolDigestPayload) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Pending call parameters: canonical masked JSON, total_bytes=%d, sha256=%s.\n", p.totalBytes, p.sha256)
	b.WriteString("Field byte lengths:")
	for _, name := range []string{"tool", "command", "args", "rule", "scope"} {
		fmt.Fprintf(&b, " %s=%d", name, p.fieldBytes[name])
	}
	b.WriteString(".\nPages come from call_parameters with offset_bytes and max_bytes (at most 8192).")
	return b.String()
}

// toolDigestPageBlock wraps the first page so the model reads it as data.
func toolDigestPageBlock(page toolDigestPage) string {
	return fmt.Sprintf("Parameter bytes [%d,%d) of %d:\n<<<BEGIN CALL PARAMETERS>>>\n%s\n<<<END CALL PARAMETERS>>>",
		page.offset, page.nextOffset, page.totalBytes, page.text)
}
