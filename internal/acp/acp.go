package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/session"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

type InputMessage struct {
	Type      string `json:"type"`
	Content   string `json:"content"`
	SessionID string `json:"sessionId,omitempty"`
}

type OutputMessage struct {
	Type      string `json:"type"`
	Content   string `json:"content,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Message   string `json:"message,omitempty"`
}

type sessionState struct {
	agent    *agent.Agent
	messages []agent.Message
	model    string
	id       string
}

func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	model := cfg.Model
	if model == "" {
		model = os.Getenv("OPENCODE_MODEL")
	}
	if model == "" {
		return fmt.Errorf("no model configured")
	}

	sessions := make(map[string]*sessionState)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var msg InputMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			writeOutput(OutputMessage{Type: "error", Message: fmt.Sprintf("invalid input: %v", err)})
			continue
		}

		if msg.Type != "message" {
			writeOutput(OutputMessage{Type: "error", Message: fmt.Sprintf("unknown message type: %s", msg.Type)})
			continue
		}

		if msg.Content == "" {
			writeOutput(OutputMessage{Type: "error", Message: "content is required"})
			continue
		}

		ss, err := getOrCreateSession(sessions, cfg, msg.SessionID, model)
		if err != nil {
			writeOutput(OutputMessage{Type: "error", Message: err.Error()})
			continue
		}

		ss.messages = append(ss.messages, agent.Message{Role: "user", Content: msg.Content})

		resp, err := ss.agent.Step(ss.messages)
		if err != nil {
			writeOutput(OutputMessage{Type: "error", Message: fmt.Sprintf("agent error: %v", err)})
			continue
		}

		ss.messages = append(ss.messages, resp...)

		var content strings.Builder
		for _, m := range resp {
			if m.Role == "assistant" && m.Content != "" {
				content.WriteString(m.Content)
			}
		}

		_ = session.Save(ss.id, "", ss.messages, nil)

		writeOutput(OutputMessage{
			Type:      "response",
			Content:   content.String(),
			SessionID: ss.id,
		})

		writeOutput(OutputMessage{
			Type:      "done",
			SessionID: ss.id,
		})
	}

	return scanner.Err()
}

func getOrCreateSession(sessions map[string]*sessionState, cfg *config.Config, sessionID, model string) (*sessionState, error) {
	if sessionID != "" {
		if ss, ok := sessions[sessionID]; ok {
			return ss, nil
		}

		s, err := session.Load(sessionID)
		if err == nil {
			client := agent.NewClient(cfg, model)
			if client == nil {
				return nil, fmt.Errorf("failed to create LLM client")
			}

			tools := tool.LoadBuiltins(cfg)
			ag := agent.NewAgent(client, tools, cfg)
			ag.LoadExternalTools(cfg)

			ss := &sessionState{
				agent:    ag,
				messages: s.Messages,
				model:    model,
				id:       sessionID,
			}
			sessions[sessionID] = ss
			return ss, nil
		}
	}

	id := time.Now().Format("2006-01-02-150405")
	client := agent.NewClient(cfg, model)
	if client == nil {
		return nil, fmt.Errorf("failed to create LLM client")
	}

	tools := tool.LoadBuiltins(cfg)
	ag := agent.NewAgent(client, tools, cfg)
	ag.LoadExternalTools(cfg)

	ss := &sessionState{
		agent:    ag,
		messages: nil,
		model:    model,
		id:       id,
	}
	sessions[id] = ss
	return ss, nil
}

func writeOutput(msg OutputMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal output: %v\n", err)
		return
	}
	fmt.Println(string(data))
}
