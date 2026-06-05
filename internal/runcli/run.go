package runcli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/commands"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/session"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func printRunUsage() {
	fmt.Println("Usage: ocode run [options] [message...]")
	fmt.Println()
	fmt.Println("Run a prompt non-interactively (headless mode).")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -prompt, -p <text>        Prompt text (can also use positional args)")
	fmt.Println("  -model, -m <model>        Model to use (overrides config)")
	fmt.Println("  -agent <name>             Agent name/mode to use")
	fmt.Println("  -session, -s <id>         Session ID to resume")
	fmt.Println("  -continue, -c             Continue the most recent session")
	fmt.Println("  -fork                     Fork from the most recent session (new session)")
	fmt.Println("  -file, -f <path>          File(s) to attach to message (can repeat)")
	fmt.Println("  -format <default|json>    Output format (default: default)")
	fmt.Println("  -title <text>             Session title")
	fmt.Println("  -attach <url>             Attach to running serve instance URL")
	fmt.Println("  -port <port>              Serve port (for --attach)")
	fmt.Println("  -yolo                     Allow tools and shell commands without permission prompts")
	fmt.Println("  --dangerously-skip-permissions")
	fmt.Println("                            Auto-approve permissions (alias for -yolo)")
	fmt.Println("  -command <name>           Slash command to run; positional message used as args")
	fmt.Println("  -dir <path>               Directory to run in")
	fmt.Println("  -username, -u <name>      Basic auth username for --attach")
	fmt.Println("  -password, -p <pass>      Basic auth password for --attach")
	fmt.Println("  -h, --help                Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ocode run \"How do I read a file in Go?\"")
	fmt.Println("  ocode run -model gpt-4 -prompt \"Write a hello world\"")
	fmt.Println("  ocode run -file main.go -prompt \"Explain this code\"")
	fmt.Println("  ocode run -attach http://localhost:4096 -prompt \"Continue session\"")
}

func Run(args []string) error {
	// Check for help flag before parsing
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			printRunUsage()
			return nil
		}
	}

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	prompt := fs.String("prompt", "", "Prompt text")
	model := fs.String("model", "", "Model to use")
	fs.StringVar(model, "m", "", "Model to use")
	agentName := fs.String("agent", "", "Agent name")
	sessionID := fs.String("session", "", "Session ID")
	fs.StringVar(sessionID, "s", "", "Session ID")
	cont := fs.Bool("continue", false, "Continue last session")
	fs.BoolVar(cont, "c", false, "Continue last session")
	fork := fs.Bool("fork", false, "Fork from last session")
	var files stringSliceFlag
	fs.Var(&files, "file", "File(s) to attach to message")
	fs.Var(&files, "f", "File(s) to attach to message")
	format := fs.String("format", "default", "Output format (default/json)")
	title := fs.String("title", "", "Session title")
	attach := fs.String("attach", "", "Attach to running serve instance URL")
	port := fs.Int("port", 0, "Serve port (for --attach)")
	yolo := fs.Bool("yolo", false, "Allow tools and shell commands without permission prompts")
	dangerous := fs.Bool("dangerously-skip-permissions", false, "Auto-approve permissions that are not explicitly denied")
	command := fs.String("command", "", "Slash command to run; positional message is used as command arguments")
	share := fs.Bool("share", false, "Share the session (accepted for OpenCode compatibility)")
	dir := fs.String("dir", "", "Directory to run in")
	variant := fs.String("variant", "", "Model variant (accepted for OpenCode compatibility)")
	thinking := fs.Bool("thinking", false, "Show thinking blocks (accepted for OpenCode compatibility)")
	username := fs.String("username", "", "Basic auth username for --attach")
	fs.StringVar(username, "u", "", "Basic auth username for --attach")
	password := fs.String("password", "", "Basic auth password for --attach")
	fs.StringVar(password, "p", "", "Basic auth password for --attach")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = share
	_ = variant
	_ = thinking

	if *dir != "" {
		if err := os.Chdir(*dir); err != nil {
			return fmt.Errorf("failed to change directory to %s: %w", *dir, err)
		}
	}

	if *attach != "" {
		promptText, err := resolveRunInput(*prompt, fs.Args(), files, *command)
		if err != nil {
			return err
		}
		return runAttach(*attach, *port, promptText, *format, *sessionID, *username, *password)
	}

	promptText, err := resolveRunInput(*prompt, fs.Args(), files, *command)
	if err != nil {
		return err
	}
	if promptText == "" {
		return fmt.Errorf("you must provide a message or a command")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	agent.ApplyAgentConfig(cfg)

	if *model != "" {
		cfg.Model = *model
	}
	if *yolo || *dangerous {
		cfg.Ocode.Permissions.Mode = string(agent.PermissionModeYOLO)
	}

	modelStr := cfg.Model
	if modelStr == "" {
		return fmt.Errorf("no model configured (set OPENCODE_MODEL or model in config)")
	}

	client := agent.NewClient(cfg, modelStr)
	if client == nil {
		return fmt.Errorf("failed to create LLM client for model %q", modelStr)
	}

	tools, _ := tool.LoadBuiltins(cfg)
	ag := agent.NewAgent(client, tools, cfg)
	ag.LoadExternalTools(cfg)
	// Only install an OnPermissionAsk override when the user explicitly opted
	// into yolo / dangerously-skip-permissions. Otherwise leave the callback
	// nil so the agent's default sentinel/deny path runs and prompts surface to
	// the caller normally.
	if *yolo || *dangerous {
		ag.OnPermissionAsk = func(req agent.PermissionRequest) agent.PermissionResponse {
			return agent.PermissionResponse{Level: agent.PermissionAllow}
		}
	}

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
		return outputJSONEvents(resp, *sessionID)
	}

	fmt.Println(responseText.String())
	return nil
}

func resolveRunInput(prompt string, positional []string, files []string, command string) (string, error) {
	base := prompt
	if base == "" && len(positional) > 0 {
		base = strings.Join(positional, " ")
	}

	if command != "" {
		cmdPrompt, err := resolveCommandPrompt(command, base)
		if err != nil {
			return "", err
		}
		base = cmdPrompt
	}

	if base == "" && len(files) == 1 {
		// Backward-compatible ocode behavior: with only --file, read the prompt
		// from that file. OpenCode treats --file as an attachment; when a message
		// is present we emulate that below by appending file contents.
		return resolvePrompt("", files[0])
	}

	piped, err := readPipedStdin()
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, 2+len(files))
	if strings.TrimSpace(base) != "" {
		parts = append(parts, base)
	}
	if strings.TrimSpace(piped) != "" {
		parts = append(parts, piped)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", file, err)
		}
		parts = append(parts, fmt.Sprintf("Attached file %s:\n%s", filepath.Base(file), string(data)))
	}
	return strings.Join(parts, "\n"), nil
}

func resolveCommandPrompt(name, args string) (string, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if name == "" {
		return "", fmt.Errorf("command name is required")
	}
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	enabled := make(map[string]bool, len(cfg.Plugins))
	for pluginName, pluginCfg := range cfg.Plugins {
		enabled[pluginName] = pluginCfg.Enabled
	}
	cmd, err := commands.LoadCommand(name, enabled)
	if err != nil {
		return "", err
	}
	if cmd == nil {
		return "", fmt.Errorf("command %q not found", name)
	}
	p := strings.ReplaceAll(cmd.Prompt, "{{args}}", args)
	p = strings.ReplaceAll(p, "{args}", args)
	if !cmd.HasArgs && strings.TrimSpace(args) != "" {
		p = strings.TrimSpace(p) + "\n\nArguments:\n" + args
	}
	return p, nil
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
	piped, err := readPipedStdin()
	if err != nil {
		return "", err
	}
	return piped, nil
}

func readPipedStdin() (string, error) {
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

func outputJSONEvents(messages []agent.Message, sessionID string) error {
	enc := json.NewEncoder(os.Stdout)
	toolCalls := map[string]agent.ToolCall{}

	emit := func(eventType string, data map[string]interface{}) error {
		payload := map[string]interface{}{
			"type":      eventType,
			"sessionID": sessionID,
		}
		for k, v := range data {
			payload[k] = v
		}
		return enc.Encode(payload)
	}

	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			toolCalls[tc.ID] = tc
		}
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) != "" {
			if err := emit("text", map[string]interface{}{
				"part": map[string]interface{}{
					"type": "text",
					"text": msg.Content,
				},
			}); err != nil {
				return err
			}
		}
		if msg.Role != "tool" {
			continue
		}
		part := map[string]interface{}{
			"type": "tool",
			"state": map[string]interface{}{
				"status": "completed",
				"output": msg.Content,
			},
		}
		if tc, ok := toolCalls[msg.ToolID]; ok {
			part["id"] = tc.ID
			part["tool"] = tc.Function.Name
			var input map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err == nil {
				part["state"].(map[string]interface{})["input"] = input
			} else {
				// Surface the unparseable arguments so consumers can debug the
				// upstream tool-call payload instead of seeing the input field
				// silently disappear.
				part["state"].(map[string]interface{})["input_raw"] = tc.Function.Arguments
			}
		} else {
			part["id"] = msg.ToolID
		}
		if err := emit("tool_use", map[string]interface{}{"part": part}); err != nil {
			return err
		}
	}
	return nil
}

func runAttach(baseURL string, port int, promptText, format, sessionID, username, password string) error {
	if port != 0 && !strings.Contains(baseURL, ":") {
		baseURL = fmt.Sprintf("http://localhost:%d", port)
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

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/chat", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if password != "" || username != "" {
		if username == "" {
			username = "opencode"
		}
		req.SetBasicAuth(username, password)
	}
	resp, err := http.DefaultClient.Do(req)
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
