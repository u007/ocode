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
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/commands"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

type runOptions struct {
	prompt         string
	model          string
	agentName      string
	sessionID      string
	cont           bool
	fork           bool
	files          stringSliceFlag
	format         string
	title          string
	attach         string
	port           int
	yolo           bool
	dangerous      bool
	permissionMode string
	command        string
	share          bool
	dir            string
	variant        string
	thinking       bool
	username       string
	password       string
}

func parseRunArgs(args []string) (runOptions, []string, error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var opts runOptions
	fs.StringVar(&opts.prompt, "prompt", "", "Prompt text")
	fs.StringVar(&opts.prompt, "p", "", "Prompt text")
	fs.StringVar(&opts.model, "model", "", "Model to use")
	fs.StringVar(&opts.model, "m", "", "Model to use")
	fs.StringVar(&opts.agentName, "agent", "", "Agent name")
	fs.StringVar(&opts.sessionID, "session", "", "Session ID")
	fs.StringVar(&opts.sessionID, "s", "", "Session ID")
	fs.BoolVar(&opts.cont, "continue", false, "Continue last session")
	fs.BoolVar(&opts.cont, "c", false, "Continue last session")
	fs.BoolVar(&opts.fork, "fork", false, "Fork from last session")
	fs.Var(&opts.files, "file", "File(s) to attach to message")
	fs.Var(&opts.files, "f", "File(s) to attach to message")
	fs.StringVar(&opts.format, "format", "default", "Output format (default/json/summary)")
	fs.StringVar(&opts.title, "title", "", "Session title")
	fs.StringVar(&opts.attach, "attach", "", "Attach to running serve instance URL")
	fs.IntVar(&opts.port, "port", 0, "Serve port (for --attach)")
	fs.BoolVar(&opts.yolo, "yolo", false, "Allow tools and shell commands without permission prompts")
	fs.BoolVar(&opts.dangerous, "dangerously-skip-permissions", false, "Auto-approve permissions that are not explicitly denied")
	fs.StringVar(&opts.permissionMode, "permission-mode", "", "LLM auto-permission mode: auto or off")
	fs.StringVar(&opts.command, "command", "", "Slash command to run; positional message is used as command arguments")
	fs.BoolVar(&opts.share, "share", false, "Share the session (accepted for OpenCode compatibility)")
	fs.StringVar(&opts.dir, "dir", "", "Directory to run in")
	fs.StringVar(&opts.variant, "variant", "", "Model variant (accepted for OpenCode compatibility)")
	fs.BoolVar(&opts.thinking, "thinking", false, "Show thinking blocks (accepted for OpenCode compatibility)")
	fs.StringVar(&opts.username, "username", "", "Basic auth username for --attach")
	fs.StringVar(&opts.username, "u", "", "Basic auth username for --attach")
	fs.StringVar(&opts.password, "password", "", "Basic auth password for --attach")
	fs.StringVar(&opts.password, "P", "", "Basic auth password for --attach")

	if err := fs.Parse(args); err != nil {
		return opts, nil, err
	}
	return opts, fs.Args(), nil
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
	fmt.Println("  -format <default|json|summary>")
	fmt.Println("                            Output format (default: default)")
	fmt.Println("  -title <text>             Session title")
	fmt.Println("  -attach <url>             Attach to running serve instance URL")
	fmt.Println("  -port <port>              Serve port (for --attach)")
	fmt.Println("  -yolo                     Allow tools and shell commands without permission prompts")
	fmt.Println("  --dangerously-skip-permissions")
	fmt.Println("                            Auto-approve permissions (alias for -yolo)")
	fmt.Println("  --permission-mode <auto|off>")
	fmt.Println("                            Enable or disable LLM auto-permission (default: off)")
	fmt.Println("  -command <name>           Slash command to run; positional message used as args")
	fmt.Println("  -dir <path>               Directory to run in")
	fmt.Println("  -username, -u <name>      Basic auth username for --attach")
	fmt.Println("  -password, -P <pass>      Basic auth password for --attach")
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

	opts, positional, err := parseRunArgs(args)
	if err != nil {
		return err
	}
	_ = opts.share
	_ = opts.variant
	_ = opts.thinking

	if opts.dir != "" {
		if err := os.Chdir(opts.dir); err != nil {
			return fmt.Errorf("failed to change directory to %s: %w", opts.dir, err)
		}
		session.SetWorkDir(opts.dir)
	}

	if opts.attach != "" {
		promptText, err := resolveRunInput(opts.prompt, positional, opts.files, opts.command)
		if err != nil {
			return err
		}
		return runAttach(opts.attach, opts.port, promptText, opts.format, opts.sessionID, opts.username, opts.password)
	}

	promptText, err := resolveRunInput(opts.prompt, positional, opts.files, opts.command)
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

	if opts.model != "" {
		cfg.Model = opts.model
	}
	if opts.yolo || opts.dangerous {
		cfg.Ocode.Permissions.Mode = string(agent.PermissionModeYOLO)
	}

	if opts.permissionMode != "" {
		switch strings.ToLower(opts.permissionMode) {
		case "auto":
			if cfg.Ocode.Permissions.Auto == nil {
				cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true}
			} else {
				cfg.Ocode.Permissions.Auto.Enabled = true
			}
		case "off":
			if cfg.Ocode.Permissions.Auto == nil {
				cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: false}
			} else {
				cfg.Ocode.Permissions.Auto.Enabled = false
			}
		default:
			return fmt.Errorf("invalid --permission-mode %q (want auto or off)", opts.permissionMode)
		}
	}

	modelStr := cfg.Model
	if modelStr == "" {
		return fmt.Errorf("no model configured (set OPENCODE_MODEL or model in config)")
	}

	client := agent.NewClient(cfg, modelStr)
	if client == nil {
		return fmt.Errorf("failed to create LLM client for model %q", modelStr)
	}

	tools, lspMgr := tool.LoadBuiltins(cfg)
	ag := agent.NewAgent(client, tools, cfg, lspMgr)
	ag.LoadExternalTools(cfg)
	// Only install an OnPermissionAsk override when the user explicitly opted
	// into yolo / dangerously-skip-permissions. Otherwise leave the callback
	// nil so the agent's default sentinel/deny path runs and prompts surface to
	// the caller normally.
	if opts.yolo || opts.dangerous {
		ag.OnPermissionAsk = func(req agent.PermissionRequest) agent.PermissionResponse {
			return agent.PermissionResponse{Level: agent.PermissionAllow}
		}
	}

	if opts.agentName != "" {
		ag.SetMode(agent.Mode(opts.agentName))
	}

	var messages []agent.Message

	if opts.sessionID != "" {
		s, err := session.Load(opts.sessionID)
		if err != nil {
			return fmt.Errorf("load session: %w", err)
		}
		messages = s.Messages
	} else if opts.cont {
		sessions, err := session.List()
		if err == nil && len(sessions) > 0 {
			messages = sessions[0].Messages
			opts.sessionID = sessions[0].ID
		}
	} else if opts.fork {
		sessions, err := session.List()
		if err == nil && len(sessions) > 0 {
			messages = sessions[0].Messages
		}
	}

	startTime := time.Now()

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

	if err := session.Save(opts.sessionID, opts.title, allMessages, nil); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	if opts.format == "json" {
		return outputJSONEvents(resp, opts.sessionID)
	}

	if opts.format == "summary" {
		// Pass the full history (original messages + new messages) so that
		// every tool call across the entire run is captured.
		return outputSummary(allMessages, opts.sessionID, modelStr, startTime)
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
