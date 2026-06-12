package acp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// sessionBridge holds the per-session agent and conversation state.
type sessionBridge struct {
	id       string
	ag       *agent.Agent
	messages []agent.Message
	cwd      string

	mu       sync.Mutex
	inFlight bool // true while a session/prompt turn is running

	// Per-turn state, reset at the start of each prompt call.
	pendingMu    sync.Mutex
	pendingTools map[string]string // toolCallID → toolName
	deltaEmitted bool              // true if at least one OnDelta text chunk arrived
}

// newSessionBridge creates an agent session for the given config and working directory.
func newSessionBridge(cfg *config.Config, cwd string) (*sessionBridge, error) {
	model := cfg.Model
	if model == "" {
		model = os.Getenv("OPENCODE_MODEL")
	}
	if model == "" {
		return nil, fmt.Errorf("no model configured (set Model in config or OPENCODE_MODEL env)")
	}

	client := agent.NewClient(cfg, model)
	if client == nil {
		return nil, fmt.Errorf("failed to create LLM client for model %q", model)
	}

	tools, lspMgr := tool.LoadBuiltins(cfg)
	ag := agent.NewAgent(client, tools, cfg, lspMgr)
	ag.LoadExternalTools(cfg)

	if cwd != "" {
		if err := os.Chdir(cwd); err != nil {
			fmt.Fprintf(os.Stderr, "acp: warning: chdir %s: %v\n", cwd, err)
		}
	}

	return &sessionBridge{
		id:           session.NewSessionID(),
		ag:           ag,
		messages:     nil,
		cwd:          cwd,
		pendingTools: make(map[string]string),
	}, nil
}

// tryStartPrompt atomically claims the in-flight slot.
// Returns false if a prompt is already running for this session.
func (b *sessionBridge) tryStartPrompt() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inFlight {
		return false
	}
	b.inFlight = true
	return true
}

func (b *sessionBridge) endPrompt() {
	b.mu.Lock()
	b.inFlight = false
	b.mu.Unlock()
}

// prompt runs one agentic turn.
//
// sendUpdate is called from goroutines that fire agent callbacks — it must be
// goroutine-safe (the caller serializes writes behind a mutex).
//
// sendPermRequest is called synchronously from the OnPermissionAsk callback
// (which itself runs inside agent.Step on the prompt goroutine). It must block
// until the client responds; the caller's read loop keeps running concurrently.
//
// Returns ("end_turn", nil), ("cancelled", nil), or ("", err) on step error.
func (b *sessionBridge) prompt(
	content []contentBlock,
	sendUpdate func(sessionUpdate),
	sendPermRequest func(toolName, rule string) string,
) (string, error) {
	// Reset per-turn tracking.
	b.pendingMu.Lock()
	b.pendingTools = make(map[string]string)
	b.deltaEmitted = false
	b.pendingMu.Unlock()

	b.messages = append(b.messages, agent.Message{
		Role:    "user",
		Content: flattenContent(content),
	})

	b.ag.ResetCancellation()

	b.ag.OnDelta = func(kind, text string) {
		switch kind {
		case "text":
			b.pendingMu.Lock()
			b.deltaEmitted = true
			b.pendingMu.Unlock()
			sendUpdate(sessionUpdate{
				Kind:    "agent_message_chunk",
				Content: []contentBlock{{Type: "text", Text: text}},
			})
		case "reasoning":
			sendUpdate(sessionUpdate{
				Kind:    "agent_thought_chunk",
				Content: []contentBlock{{Type: "text", Text: text}},
			})
		}
	}

	b.ag.OnMessage = func(msg agent.Message) {
		switch msg.Role {
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				// Emit full text as one chunk if the provider didn't stream deltas.
				if msg.Content != "" {
					b.pendingMu.Lock()
					already := b.deltaEmitted
					b.pendingMu.Unlock()
					if !already {
						sendUpdate(sessionUpdate{
							Kind:    "agent_message_chunk",
							Content: []contentBlock{{Type: "text", Text: msg.Content}},
						})
						b.pendingMu.Lock()
						b.deltaEmitted = true
						b.pendingMu.Unlock()
					}
				}
				for _, tc := range msg.ToolCalls {
					b.pendingMu.Lock()
					b.pendingTools[tc.ID] = tc.Function.Name
					b.pendingMu.Unlock()
					sendUpdate(sessionUpdate{
						Kind:       "tool_call",
						ToolCallID: tc.ID,
						Title:      makeTitle(tc.Function.Name, tc.Function.Arguments),
						ToolKind:   mapToolKind(tc.Function.Name),
						Status:     "pending",
					})
				}
			} else if msg.Content != "" {
				// Final assistant message with no tool calls (non-streaming provider).
				b.pendingMu.Lock()
				already := b.deltaEmitted
				b.pendingMu.Unlock()
				if !already {
					sendUpdate(sessionUpdate{
						Kind:    "agent_message_chunk",
						Content: []contentBlock{{Type: "text", Text: msg.Content}},
					})
					b.pendingMu.Lock()
					b.deltaEmitted = true
					b.pendingMu.Unlock()
				}
			}

		case "tool":
			b.pendingMu.Lock()
			_, known := b.pendingTools[msg.ToolID]
			b.pendingMu.Unlock()
			if known {
				status := "completed"
				if strings.HasPrefix(msg.Content, "Error:") || strings.HasPrefix(msg.Content, "error:") {
					status = "failed"
				}
				sendUpdate(sessionUpdate{
					Kind:       "tool_call_update",
					ToolCallID: msg.ToolID,
					Status:     status,
					Content:    []contentBlock{{Type: "text", Text: msg.Content}},
				})
			}
		}
	}

	b.ag.OnPermissionAsk = func(req agent.PermissionRequest) agent.PermissionResponse {
		selected := sendPermRequest(req.ToolName, req.Rule)
		switch selected {
		case "allow-always":
			return agent.PermissionResponse{Level: agent.PermissionAllow, PersistRule: true}
		case "allow-once":
			return agent.PermissionResponse{Level: agent.PermissionAllow}
		default: // "reject-once", "cancelled", or unknown
			return agent.PermissionResponse{Level: agent.PermissionDeny}
		}
	}

	resp, stepErr := b.ag.Step(b.messages)

	// Determine stop reason before touching any state.
	var stopReason string
	select {
	case <-b.ag.Done():
		stopReason = "cancelled"
	default:
		if stepErr != nil {
			return "", stepErr
		}
		stopReason = "end_turn"
	}

	if stepErr == nil {
		b.messages = append(b.messages, resp...)
		_ = session.Save(b.id, "", b.messages, nil)
	}

	return stopReason, nil
}

// cancel signals the agent's Step loop to stop.
func (b *sessionBridge) cancel() {
	b.ag.Cancel()
}

// flattenContent converts ACP content blocks into a single user message string.
// text blocks are concatenated; resource blocks append the file content as a
// fenced section; resource_link blocks append a URI reference line.
func flattenContent(blocks []contentBlock) string {
	var main strings.Builder
	var ctx strings.Builder

	for _, block := range blocks {
		switch block.Type {
		case "text":
			if main.Len() > 0 {
				main.WriteByte('\n')
			}
			main.WriteString(block.Text)
		case "resource":
			if block.Resource != nil {
				ctx.WriteString("\n\n--- Context: ")
				ctx.WriteString(block.Resource.URI)
				ctx.WriteString(" ---\n")
				ctx.WriteString(block.Resource.Text)
				ctx.WriteString("\n---")
			}
		case "resource_link":
			ctx.WriteString("\n\n[Reference: ")
			ctx.WriteString(block.URI)
			ctx.WriteByte(']')
		}
	}

	result := main.String()
	if ctx.Len() > 0 {
		result += ctx.String()
	}
	return result
}

// mapToolKind maps an ocode tool name to the ACP tool_call kind field.
func mapToolKind(toolName string) string {
	switch toolName {
	case "read":
		return "read"
	case "write", "edit", "multi_edit", "apply_patch":
		return "edit"
	case "bash", "bash_output":
		return "execute"
	case "grep", "glob":
		return "search"
	case "webfetch":
		return "fetch"
	default:
		return "other"
	}
}

// makeTitle builds a human-readable title for a tool_call update.
func makeTitle(toolName, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err == nil {
		for _, key := range []string{"path", "file_path", "command", "query", "url", "pattern", "glob", "prompt"} {
			if v, ok := args[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					if len(s) > 60 {
						s = s[:57] + "..."
					}
					return toolName + ": " + s
				}
			}
		}
	}
	return toolName
}
