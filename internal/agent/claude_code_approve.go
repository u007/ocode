package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/tool"
)

// claudeCodeAskLabel names the Claude Code bridge as the asker of every
// permission request it raises, and prefixes the tool name so the dialog and
// any persisted rule read as "claude-code:<tool>".
const claudeCodeAskLabel = "claude-code"

// claudeCodeNoSurfaceMessage is the denial returned when the call needs a human
// answer but this session has no surface to ask on.
const claudeCodeNoSurfaceMessage = "approval required, but no approval surface is available in this session"

// decideOnlyKey is the context key for the decide-only flag.
type decideOnlyKey struct{}

// withDecideOnly returns ctx carrying a decide-only flag. When
// executeToolCallWithContext sees it, it sets *flag and skips execution, so the
// whole permission pipeline runs and nothing is run.
func withDecideOnly(ctx context.Context, flag *bool) context.Context {
	return context.WithValue(ctx, decideOnlyKey{}, flag)
}

// decideOnlyFromContext returns the decide-only flag carried by ctx, or nil.
func decideOnlyFromContext(ctx context.Context) *bool {
	flag, _ := ctx.Value(decideOnlyKey{}).(*bool)
	return flag
}

// mapClaudeCodeTool translates a Claude Code tool call into the ocode tool and
// arguments whose permission pipeline should decide it. mapped is false for a
// tool with no ocode counterpart, which is not an error: the caller routes it
// to the human asker instead. err reports input that does not decode into the
// shape the mapping needs.
func mapClaudeCodeTool(toolName string, input json.RawMessage) (name string, args json.RawMessage, mapped bool, err error) {
	switch toolName {
	case "Bash":
		var in struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return "", nil, true, err
		}
		args, err := json.Marshal(map[string]string{"command": in.Command})
		return "bash", args, true, err
	case "Write":
		var in struct {
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return "", nil, true, err
		}
		args, err := json.Marshal(map[string]string{"path": in.FilePath, "content": in.Content})
		return "write", args, true, err
	case "Edit":
		var in struct {
			FilePath  string `json:"file_path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return "", nil, true, err
		}
		args, err := json.Marshal(map[string]string{"path": in.FilePath, "search": in.OldString, "replace": in.NewString})
		return "edit", args, true, err
	case "MultiEdit":
		// Claude Code's MultiEdit is one file with an ordered edit list, which is
		// exactly ocode's multiedit shape (top-level file_path, per-edit
		// oldString/newString/replaceAll).
		var in struct {
			FilePath string `json:"file_path"`
			Edits    []struct {
				OldString  string `json:"old_string"`
				NewString  string `json:"new_string"`
				ReplaceAll bool   `json:"replace_all"`
			} `json:"edits"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return "", nil, true, err
		}
		type ocodeEdit struct {
			OldString  string `json:"oldString"`
			NewString  string `json:"newString"`
			ReplaceAll bool   `json:"replaceAll,omitempty"`
		}
		edits := make([]ocodeEdit, 0, len(in.Edits))
		for _, e := range in.Edits {
			edits = append(edits, ocodeEdit{OldString: e.OldString, NewString: e.NewString, ReplaceAll: e.ReplaceAll})
		}
		args, err := json.Marshal(map[string]any{"file_path": in.FilePath, "edits": edits})
		return "multiedit", args, true, err
	case "NotebookEdit":
		// The judge and the dialog need the new cell source, not just the path.
		var in struct {
			NotebookPath string `json:"notebook_path"`
			NewSource    string `json:"new_source"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return "", nil, true, err
		}
		args, err := json.Marshal(map[string]string{"path": in.NotebookPath, "content": in.NewSource})
		return "write", args, true, err
	case "WebFetch":
		var in struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return "", nil, true, err
		}
		args, err := json.Marshal(map[string]string{"url": in.URL})
		return "webfetch", args, true, err
	}
	return "", nil, false, nil
}

// claudeCodeElevationMessage is the denial for a sudo/su command. The bridge
// never collects a root password, so the command has to be run by the user.
const claudeCodeElevationMessage = "root elevation (sudo/su) is not supported through Claude Code; run that command yourself"

// decideClaudeCodeToolUse decides whether ocode approves a tool call the Claude
// Code CLI wants to run. It runs ocode's normal permission pipeline (static
// rules, /ban, mode gate, network guard, auto-permission judge, then the human
// ask) but never executes the tool: Claude Code runs it itself. A mapped tool
// goes through runToolCallWithContext in decide-only mode. Any other tool skips
// the pipeline and goes straight to the attributed human asker. Human asks go
// through the bridge asker under ctx, so cancelling ctx ends a pending ask.
func (a *Agent) decideClaudeCodeToolUse(ctx context.Context, toolName string, input json.RawMessage, toolUseID string) (allowed bool, message string) {
	var probe any
	if err := json.Unmarshal(input, &probe); err != nil {
		return false, fmt.Sprintf("denied: %s input is not valid JSON: %v", toolName, err)
	}
	name, args, mapped, err := mapClaudeCodeTool(toolName, input)
	if err != nil {
		return false, fmt.Sprintf("denied: %s input does not match the expected shape: %v", toolName, err)
	}
	if !mapped {
		return a.askClaudeCodeUnmapped(ctx, toolName, input)
	}
	// Refuse before the pipeline: a sudo/su command would raise a root password
	// dialog, and the bridge must not collect or store that secret.
	if isElevationApproval(name, args) {
		return false, claudeCodeElevationMessage
	}

	var decided bool
	ctx = withDecideOnly(ctx, &decided)
	// The per-call asker always wins over the agent's own OnPermissionAsk, so a
	// host-wide asker can never answer a Claude Code approval. With no bridge
	// asker installed, a human-needed call is denied as having no surface.
	var needsHuman bool
	if a.bridgePermAsker != nil {
		ctx = withPermissionAsker(ctx, attributePermAsker(a.bridgeAsker(ctx), claudeCodeAskLabel))
	} else {
		ctx = withPermissionAsker(ctx, func(PermissionRequest) PermissionResponse {
			needsHuman = true
			return PermissionResponse{Level: PermissionDeny}
		})
	}
	text, err := a.runToolCallWithContext(ctx, name, args, nil, claudeCodeAskLabel+":"+toolUseID)
	if decided {
		return true, ""
	}
	if needsHuman {
		return false, claudeCodeNoSurfaceMessage
	}
	if err != nil {
		return false, err.Error()
	}
	if strings.HasPrefix(text, tool.SentinelPermissionAsk) {
		return false, claudeCodeNoSurfaceMessage
	}
	if text == "" {
		return false, fmt.Sprintf("denied: ocode did not approve %s", toolName)
	}
	return false, text
}

// bridgeAsker adapts a.bridgePermAsker to the plain asker shape, binding it to
// ctx so a request cancelled by the CLI (or by Close) ends the ask. The caller
// must have checked a.bridgePermAsker is non-nil.
func (a *Agent) bridgeAsker(ctx context.Context) func(PermissionRequest) PermissionResponse {
	ask := a.bridgePermAsker
	return func(req PermissionRequest) PermissionResponse {
		return ask(ctx, req)
	}
}

// askClaudeCodeUnmapped asks the human for a Claude Code tool that has no ocode
// counterpart (WebSearch, mcp__*, Agent, ...). No judge and no rules apply, and
// an allow is not persisted: the request names a Claude Code tool, which no
// ocode rule matches.
func (a *Agent) askClaudeCodeUnmapped(ctx context.Context, toolName string, input json.RawMessage) (bool, string) {
	if a.bridgePermAsker == nil {
		return false, claudeCodeNoSurfaceMessage
	}
	label := claudeCodeAskLabel + ":" + toolName
	req := PermissionRequest{
		ToolName: label,
		Args:     input,
		Scope:    PermissionScopeTool,
		Rule:     "tool." + label,
	}
	resp := attributePermAsker(a.bridgeAsker(ctx), claudeCodeAskLabel)(req)
	if resp.Level == PermissionAllow {
		return true, ""
	}
	return false, fmt.Sprintf("denied: %s denied by user", toolName)
}

// claudeCodeQuestion is one entry of Claude Code's AskUserQuestion input.
type claudeCodeQuestion struct {
	Question string `json:"question"`
	Header   string `json:"header"`
	Options  []struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
	MultiSelect bool `json:"multiSelect"`
}

// answerClaudeCodeQuestion shows Claude Code's AskUserQuestion in the host's
// question dialog and returns the updatedInput Claude Code expects: the
// original input plus answers keyed by question text. A multi-select answer
// joins its labels with ", "; a typed answer uses the typed text.
func (a *Agent) answerClaudeCodeQuestion(ctx context.Context, input json.RawMessage) (allowed bool, updatedInput json.RawMessage, message string) {
	var in struct {
		Questions []claudeCodeQuestion `json:"questions"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return false, nil, fmt.Sprintf("denied: AskUserQuestion input does not match the expected shape: %v", err)
	}
	if len(in.Questions) == 0 {
		return false, nil, "denied: AskUserQuestion has no questions"
	}
	prompts := make([]tool.QuestionPrompt, 0, len(in.Questions))
	for _, q := range in.Questions {
		p := tool.QuestionPrompt{Header: q.Header, Question: q.Question, Multiple: q.MultiSelect}
		for _, o := range q.Options {
			p.Options = append(p.Options, tool.QuestionOption{Label: o.Label, Description: o.Description})
		}
		prompts = append(prompts, p)
	}
	// With auto-continue or an evaluable goal on, the question judge gets the
	// first look. It answers only when the transcript already determines the
	// answer; anything else falls through to the human dialog below.
	if sets, ok := a.judgeClaudeCodeQuestion(ctx, prompts); ok {
		return claudeCodeQuestionUpdatedInput(input, sets)
	}
	if a.questionAsker == nil {
		return false, nil, claudeCodeNoSurfaceMessage
	}
	sets, dismissed := a.questionAsker(ctx, prompts)
	if dismissed {
		return false, nil, "the user dismissed the question without answering"
	}
	return claudeCodeQuestionUpdatedInput(input, sets)
}

// claudeCodeAnswerText joins one question's answers the way Claude Code expects:
// a typed answer uses the typed text, a choice its label, and multi-select
// answers are joined with ", ".
func claudeCodeAnswerText(set tool.QuestionAnswerSet) string {
	var parts []string
	for _, ans := range set.Answers {
		if strings.TrimSpace(ans.Text) != "" {
			parts = append(parts, ans.Text)
		} else if ans.Label != "" {
			parts = append(parts, ans.Label)
		}
	}
	return strings.Join(parts, ", ")
}

// claudeCodeQuestionUpdatedInput builds the updatedInput Claude Code expects
// from the answer sets. The judge and the human dialog share it, so both paths
// key and join answers the same way.
func claudeCodeQuestionUpdatedInput(input json.RawMessage, sets []tool.QuestionAnswerSet) (allowed bool, updatedInput json.RawMessage, message string) {
	answers := make(map[string]string, len(sets))
	for _, set := range sets {
		answers[set.Question] = claudeCodeAnswerText(set)
	}
	var original map[string]any
	if err := json.Unmarshal(input, &original); err != nil {
		return false, nil, fmt.Sprintf("denied: AskUserQuestion input is not an object: %v", err)
	}
	original["answers"] = answers
	out, err := json.Marshal(original)
	if err != nil {
		return false, nil, fmt.Sprintf("denied: encode AskUserQuestion answers: %v", err)
	}
	return true, out, ""
}

// claudeCodeTurnKey carries the per-run state of one Claude Code turn. The
// approval bridge has no transcript or run state of its own.
type claudeCodeTurnKey struct{}

// claudeCodeTurn is the state one chatClaudeCode run shares with its approval
// bridge: the ocode transcript the question judge reads, the judge answers this
// run has given (the cap is per run), and the notice lines those answers leave
// for the transcript row.
type claudeCodeTurn struct {
	messages []Message

	mu           sync.Mutex
	judgeAnswers int      // judge answers committed in this run
	judgePending int      // judge calls in flight, reserved against the cap
	judgeNotices []string // answer lines not yet moved into the transcript row
}

// newClaudeCodeTurn starts the state for one run over messages.
func newClaudeCodeTurn(messages []Message) *claudeCodeTurn {
	return &claudeCodeTurn{messages: messages}
}

// withClaudeCodeTurn returns ctx carrying the run's state.
func withClaudeCodeTurn(ctx context.Context, turn *claudeCodeTurn) context.Context {
	return context.WithValue(ctx, claudeCodeTurnKey{}, turn)
}

// claudeCodeTurnFromContext returns the run state carried by ctx, and false when
// none was attached.
func claudeCodeTurnFromContext(ctx context.Context) (*claudeCodeTurn, bool) {
	turn, ok := ctx.Value(claudeCodeTurnKey{}).(*claudeCodeTurn)
	return turn, ok && turn != nil
}

// reserveJudge reserves one judge call for this run. It refuses once the
// committed answers plus the calls still in flight reach AutoContinueChainCap,
// so concurrent approvals cannot overshoot the cap.
func (t *claudeCodeTurn) reserveJudge() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.judgeAnswers+t.judgePending >= AutoContinueChainCap {
		return false
	}
	t.judgePending++
	return true
}

// releaseJudge gives back a reservation whose judge call did not answer, so a
// deferral does not use up the cap.
func (t *claudeCodeTurn) releaseJudge() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.judgePending--
}

// commitJudgeAnswer turns a reservation into a counted answer and queues its
// notice lines.
func (t *claudeCodeTurn) commitJudgeAnswer(notices []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.judgePending--
	t.judgeAnswers++
	t.judgeNotices = append(t.judgeNotices, notices...)
}

// takeJudgeNotices returns and clears the queued notice lines.
func (t *claudeCodeTurn) takeJudgeNotices() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.judgeNotices
	t.judgeNotices = nil
	return out
}

// claudeCodeApproveForTurn wraps fn so every approval request it receives carries
// the run's state. The bridge passes its own per-request context, so the state
// is attached to that context, not to the chat's.
func claudeCodeApproveForTurn(fn ClaudeCodeApproveFunc, turn *claudeCodeTurn) ClaudeCodeApproveFunc {
	return func(ctx context.Context, toolName string, input json.RawMessage, toolUseID string) (bool, json.RawMessage, string) {
		return fn(withClaudeCodeTurn(ctx, turn), toolName, input, toolUseID)
	}
}

// claudeCodeJudgeNoticeLines renders one transcript line per judged question.
// Whitespace is folded so each question and answer stays on one row.
func claudeCodeJudgeNoticeLines(sets []tool.QuestionAnswerSet) []string {
	lines := make([]string, 0, len(sets))
	for _, set := range sets {
		lines = append(lines, "AskUserQuestion answered by the auto-continue judge: "+
			strings.Join(strings.Fields(set.Question), " ")+" → "+
			strings.Join(strings.Fields(claudeCodeAnswerText(set)), " "))
	}
	return lines
}

// judgeClaudeCodeQuestion offers an AskUserQuestion to the auto-continue question
// judge when auto-continue or an evaluable goal is on. It returns the judge's
// answer sets only when the judge answered. Every other outcome (gate off, no
// run state, cap reached, judge error, deferral) returns false so the human
// dialog runs. Each judge answer counts toward the run's cap and queues a notice
// line for the transcript row; a deferral counts for nothing.
func (a *Agent) judgeClaudeCodeQuestion(ctx context.Context, prompts []tool.QuestionPrompt) ([]tool.QuestionAnswerSet, bool) {
	if !a.AutoContinueEnabled() && !a.GoalEvaluable() {
		return nil, false
	}
	turn, ok := claudeCodeTurnFromContext(ctx)
	if !ok {
		a.emitDebug("CLAUDE-CODE", "question judge skipped: the approval request carries no run state")
		return nil, false
	}
	if !turn.reserveJudge() {
		a.emitDebug("CLAUDE-CODE", fmt.Sprintf("question judge skipped: this run already has %d judge answers, asking the user", AutoContinueChainCap))
		return nil, false
	}
	dec, err := a.AutoContinueQuestionJudgeSync(turn.messages, prompts, nil)
	if err != nil {
		turn.releaseJudge()
		a.emitDebug("CLAUDE-CODE", fmt.Sprintf("question judge failed, asking the user: %v", err))
		return nil, false
	}
	if !dec.Answered {
		turn.releaseJudge()
		a.emitDebug("CLAUDE-CODE", "question judge did not answer, asking the user: "+dec.Detail)
		return nil, false
	}
	turn.commitJudgeAnswer(claudeCodeJudgeNoticeLines(dec.Answers))
	a.emitDebug("CLAUDE-CODE", "question judge answered on the user's behalf: "+dec.Detail)
	return dec.Answers, true
}
