package runcli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/session"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

func Run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	prompt := fs.String("prompt", "", "Prompt text")
	model := fs.String("model", "", "Model to use")
	agentName := fs.String("agent", "", "Agent name")
	sessionID := fs.String("session", "", "Session ID")
	cont := fs.Bool("continue", false, "Continue last session")
	fork := fs.Bool("fork", false, "Fork from last session")
	file := fs.String("file", "", "File to read prompt from")
	format := fs.String("format", "text", "Output format (text/json)")
	title := fs.String("title", "", "Session title")
	attach := fs.String("attach", "", "Attach to running serve instance URL")
	port := fs.Int("port", 0, "Serve port (for --attach)")
	fs.Parse(args)

	if *attach != "" {
		return runAttach(*attach, *port, *prompt, *file, *format, *sessionID)
	}

	promptText, err := resolvePrompt(*prompt, *file)
	if err != nil {
		return err
	}
	if promptText == "" {
		return fmt.Errorf("no prompt provided (use --prompt or stdin)")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if *model != "" {
		cfg.Model = *model
	}

	modelStr := cfg.Model
	if modelStr == "" {
		return fmt.Errorf("no model configured (set OPENCODE_MODEL or model in config)")
	}

	client := agent.NewClient(cfg, modelStr)
	if client == nil {
		return fmt.Errorf("failed to create LLM client for model %q", modelStr)
	}

	tools := tool.LoadBuiltins()
	ag := agent.NewAgent(client, tools, cfg)
	ag.LoadExternalTools(cfg)

	if *agentName != "" {
		ag.SetMode(agent.Mode(*agentName))
	}

	var messages []agent.Message

	if *sessionID != "" {
		s, err := session.Load(*sessionID)
		if err != nil {
			return fmt.Errorf("load session: %w", err)
		}
		messages = s.Messages
	} else if *cont {
		sessions, err := session.List()
		if err == nil && len(sessions) > 0 {
			messages = sessions[0].Messages
			*sessionID = sessions[0].ID
		}
	} else if *fork {
		sessions, err := session.List()
		if err == nil && len(sessions) > 0 {
			messages = sessions[0].Messages
		}
	}

	messages = append(messages, agent.Message{Role: "user", Content: promptText})

	resp, err := ag.Step(messages)
	if err != nil {
		return fmt.Errorf("agent step: %w", err)
	}

	var responseText strings.Builder
	for _, m := range resp {
		if m.Role == "assistant" && m.Content != "" {
			responseText.WriteString(m.Content)
		}
	}

	allMessages := append(messages, resp...)

	if *sessionID == "" {
		*sessionID = ""
	}
	if err := session.Save(*sessionID, *title, allMessages, nil); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	if *format == "json" {
		return outputJSON(responseText.String(), *sessionID, *title)
	}

	fmt.Println(responseText.String())
	return nil
}

func resolvePrompt(prompt, file string) (string, error) {
	if prompt != "" {
		return prompt, nil
	}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read file: %w", err)
		}
		return string(data), nil
	}
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	}
	return "", nil
}

func outputJSON(text, sessionID, title string) error {
	out := map[string]interface{}{
		"content":   text,
		"sessionID": sessionID,
		"title":     title,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func runAttach(baseURL string, port int, prompt, file, format, sessionID string) error {
	if port != 0 && !strings.Contains(baseURL, ":") {
		baseURL = fmt.Sprintf("http://localhost:%d", port)
	}

	promptText, err := resolvePrompt(prompt, file)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"content": promptText,
	}
	if sessionID != "" {
		payload["sessionId"] = sessionID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := http.Post(baseURL+"/api/chat", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("attach request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server error %d: %s", resp.StatusCode, string(respBody))
	}

	if format == "json" {
		fmt.Println(string(respBody))
	} else {
		var result struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(respBody, &result); err == nil && result.Content != "" {
			fmt.Println(result.Content)
		} else {
			fmt.Println(string(respBody))
		}
	}
	return nil
}
