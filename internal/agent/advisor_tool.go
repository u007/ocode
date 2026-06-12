package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// Default advisor model when OPENCODE_ADVISOR_MODEL is not set.
const defaultAdvisorModel = "deepseek/deepseek-v4-pro"

// advisorSystemPrompt instructs the advisor model on its role and available
// exploration tools. The prompt explicitly encourages the advisor to proactively
// use tools to investigate the codebase before giving advice.
const advisorSystemPrompt = `You are a strategic advisor for a coding agent. You have access to tools that let you explore the codebase and research external resources.

YOUR EXPLORATION SUB-AGENT CAPABILITIES:
You can and SHOULD proactively use your tools to investigate before advising. Do not rely solely on context provided in the prompt — verify, explore, and discover on your own. Your tools are:

- read — read file contents to understand implementation details
- glob — find files matching patterns (e.g. **/*.go, **/test_*.go)
- grep — search code for patterns, function names, error messages
- list — list directory contents to understand project structure
- lsp — get code intelligence: definitions, references, hover info, diagnostics
- bash — run shell commands (git log, git diff, go test, etc.)
- webfetch — fetch and read web pages for documentation
- websearch — search the web for solutions, library docs, best practices
- repo_clone, repo_overview — inspect external libraries and dependencies

EXPLORATION STRATEGY:
1. Start by understanding the project structure (list, glob)
2. Read relevant files to understand current implementation
3. Use grep to find all usages of key functions/types
4. Use lsp for deep code intelligence (goToDefinition, findReferences)
5. Check tests to understand expected behavior
6. Research external docs if working with libraries/APIs
7. Use git commands to understand history and context

YOUR ADVICE MUST BE ACTIONABLE — tell the executor:
- What to do next (specific files, functions, line numbers when possible)
- What order to proceed in
- What to watch out for (edge cases, pitfalls, regressions)
- What not to do

Key heuristics:
- Prefer the simplest approach that meets the spec
- Flag approaches that create maintenance burden
- If the executor is stuck or looping, suggest a different approach
- If tests or evidence contradict an assumption, say so explicitly
- Cite specific file paths and line numbers when referencing code

Respond in under 300 words. Use enumerated steps. Do NOT write production code — only advise.`

// advisorRecursionGuard prevents nested advisor calls.
var advisorRecursionGuard atomic.Bool

// advisorAllowedTools lists the tool names the advisor sub-agent may use.
var advisorAllowedTools = []string{
	"read", "glob", "grep", "list", "lsp",
	"bash", "bash_output", "kill_shell",
	"webfetch", "websearch",
	"repo_clone", "repo_overview",
}

// AdvisorTool lets the agent consult a stronger model for strategic guidance.
// The advisor receives its own set of exploration tools so it can investigate
// the codebase, research documentation, and then provide informed advice.
type AdvisorTool struct {
	cfg       *config.Config
	mainAgent *Agent
	workDir   string
}

func (t AdvisorTool) Name() string { return "advisor" }
func (t AdvisorTool) Description() string {
	return "Consult a strategic advisor (backed by a configurable model) that can proactively explore the codebase with tools and provide a concise plan or course correction."
}
func (t AdvisorTool) Parallel() bool { return false }

func (t AdvisorTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name": "advisor",
		"description": "Consult a strategic advisor (backed by a configurable model) that can proactively explore the codebase with tools and provide a concise plan or course correction.\n\n" +
			"The advisor model is resolved with this priority: OPENCODE_ADVISOR_MODEL env var > ocode.json [advisor] config > built-in default. Use the /advisor command to preset which model the advisor uses.\n\n" +
			"Call advisor BEFORE substantive work — before writing code, editing files, committing to an interpretation, or building on an assumption. If the task requires orientation first (finding files, reading code, fetching docs), do that, then call advisor. Orientation is NOT substantive work, though the advisor can also do its own orientation if you feed it limited context.\n\n" +
			"Also call advisor:\n" +
			"- When stuck — errors recurring, approach not converging, results that don't fit\n" +
			"- When considering a change of approach\n" +
			"- When you believe the task is complete. BEFORE this call, make your deliverable durable: write the file, save the result, commit the change\n\n" +
			"On tasks longer than a few steps, call advisor at least once before committing to an approach and once before declaring done. On short reactive turns where tool output directly dictates the next action, skip advisor.\n\n" +
			"Required tool arg prompt must include enough concrete context so the advisor can often avoid redundant exploration: user goal, constraints, files/paths already inspected, key findings or command outputs, attempts so far, and the exact decision/questions you want advice on.\n\n" +
			"The advisor has its own exploration sub-agent tools (read, glob, grep, lsp, bash, websearch, etc.) and will proactively investigate the codebase before advising. You do NOT need to pre-explore everything — the advisor will discover details on its own.\n\n" +
			"Give the advice serious weight. Only override if you have primary-source evidence that contradicts a specific claim. Surface conflicts in another advisor call rather than silently switching approaches.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type": "string",
					"description": "All context the advisor needs to provide guidance — include user goal, constraints, files/lines already inspected, key evidence/outputs, attempts so far, and the exact decision/questions you want advice on. " +
						"The advisor will proactively explore the codebase with its own exploration sub-agent tools to find details and verify assumptions — you do NOT need to pre-explore everything.",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

// resolveModel returns the model string to use for the advisor.
// Priority: OPENCODE_ADVISOR_MODEL env var > config > default.
// The model is preset via the /advisor command or config, not per-call.
func (t AdvisorTool) resolveModel() string {
	// 1. OPENCODE_ADVISOR_MODEL env var.
	envModel := os.Getenv("OPENCODE_ADVISOR_MODEL")
	if envModel != "" {
		return envModel
	}

	// 2. Config from ocode.json [advisor] section.
	if t.cfg != nil {
		ac := t.cfg.Ocode.Advisor
		if ac.Provider != "" && ac.Model != "" {
			return ac.Provider + "/" + ac.Model
		}
		if ac.Model != "" {
			return ac.Model
		}
	}

	// 3. Built-in default.
	return defaultAdvisorModel
}

// getAdvisorTools returns a filtered set of exploration tools from the main
// agent's tool map, including only those the advisor is allowed to use.
func (t AdvisorTool) getAdvisorTools() []tool.Tool {
	if t.mainAgent == nil {
		return nil
	}
	allTools := t.mainAgent.GetTools()
	allowed := make(map[string]bool, len(advisorAllowedTools))
	for _, name := range advisorAllowedTools {
		allowed[name] = true
	}
	result := make([]tool.Tool, 0, len(advisorAllowedTools))
	for _, tl := range allTools {
		if allowed[tl.Name()] {
			result = append(result, tl)
		}
	}
	return result
}

func (t AdvisorTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if strings.TrimSpace(params.Prompt) == "" {
		return "", fmt.Errorf("advisor prompt is required and must not be empty")
	}

	// Recursion guard — prevent nested advisor calls.
	if !advisorRecursionGuard.CompareAndSwap(false, true) {
		return "", fmt.Errorf("advisor tool cannot be called recursively")
	}
	defer advisorRecursionGuard.Store(false)

	// Claude Code path: use the Claude Code CLI (claude -p) instead of an
	// LLM API client when the advisor is configured for claude-code.
	if t.cfg != nil && t.cfg.Ocode.Advisor.ClaudeCode {
		modelName := t.cfg.Ocode.Advisor.Model
		if modelName == "" {
			modelName = "claude-sonnet-4-6"
		}
		emitDebug("ADVISOR", fmt.Sprintf("calling Claude Code CLI with model: %s", modelName))
		result, err := executeClaudeCodeAdvisor(modelName, params.Prompt, t.workDir)
		if err != nil {
			return "", fmt.Errorf("claude code advisor call failed: %w", err)
		}
		return result, nil
	}

	// Resolve the model to use (preset via /advisor or config, not per-call).
	modelStr := t.resolveModel()
	emitDebug("ADVISOR", fmt.Sprintf("calling model: %s", modelStr))

	// Create an LLM client for the advisor model (may differ from the main agent's model).
	// If the resolved model can't be created (bad provider/credentials/config) and it
	// isn't already the built-in default, show a notice and fall back to the preset
	// default advisor model rather than failing the call outright.
	var fallbackNotice string
	client := NewClient(t.cfg, modelStr)
	if client == nil {
		if modelStr != defaultAdvisorModel {
			fallbackNotice = fmt.Sprintf("⚠ advisor model %q unavailable (check provider credentials and config); falling back to default model %q", modelStr, defaultAdvisorModel)
			emitDebug("ADVISOR", fallbackNotice)
			modelStr = defaultAdvisorModel
			client = NewClient(t.cfg, modelStr)
		}
		if client == nil {
			return "", fmt.Errorf("could not create client for advisor model %q: check provider credentials and config", modelStr)
		}
	}

	// Create a sub-agent with exploration tools so the advisor can investigate
	// the codebase and research before providing its strategic advice.
	advisorTools := t.getAdvisorTools()
	if len(advisorTools) == 0 {
		// Fallback: no tools available (e.g. main agent not set). Use a plain
		// single-turn chat so the advisor still returns something useful.
		emitDebug("ADVISOR", "no exploration tools available; falling back to plain chat")
		messages := []Message{
			{Role: "system", Content: advisorSystemPrompt},
			{Role: "user", Content: params.Prompt},
		}
		resp, err := client.Chat(messages, nil)
		if err != nil {
			return "", fmt.Errorf("advisor call failed: %w", err)
		}
		if resp == nil || strings.TrimSpace(resp.Content) == "" {
			return "", fmt.Errorf("advisor returned no advice (plain chat fallback). model=%q", modelStr)
		}
		return withNotice(fallbackNotice, strings.TrimSpace(resp.Content)), nil
	}

	advisorAgent := NewAgent(client, advisorTools, t.cfg, t.mainAgent.lspMgr)

	// Set the spec with the advisor system prompt and restrict tools to the
	// exploration set. We set spec directly (not via SetSpec) so the resolved
	// advisor client is preserved — SetSpec would try to swap clients based on
	// spec.Model, which we intentionally leave empty.
	advisorAgent.spec = &AgentSpec{
		SystemPrompt: advisorSystemPrompt,
		Tools:        advisorAllowedTools,
		MaxSteps:     60,
	}
	advisorAgent.mode = ModeBuild // neutral mode so the mode prompt doesn't interfere

	// Share the parent's permission manager so any already-approved tools
	// (accumulated during the session) are inherited. Pre-allow the advisor's
	// exploration tools so it can work without pausing for permission.
	if t.mainAgent != nil && t.mainAgent.Permissions() != nil {
		advisorAgent.permissions = t.mainAgent.Permissions()
	}
	for _, name := range advisorAllowedTools {
		advisorAgent.permissions.SetRule(name, PermissionAllow)
	}

	// Run the agentic tool loop with the user's context prompt.
	messages := []Message{{Role: "user", Content: params.Prompt}}
	resp, err := advisorAgent.Step(messages)
	if err != nil {
		return "", fmt.Errorf("advisor call failed: %w", err)
	}

	// Extract all assistant messages into the final advice.
	var b strings.Builder
	for _, m := range resp {
		if m.Role == "assistant" && m.Content != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(m.Content)
		}
	}
	result := strings.TrimSpace(b.String())
	if result == "" {
		return "", fmt.Errorf("advisor returned no advice after agentic run. model=%q", modelStr)
	}

	return withNotice(fallbackNotice, result), nil
}

// withNotice prepends a notice string (if non-empty) to the advisor response,
// separated by a blank line.
func withNotice(notice, content string) string {
	if notice == "" {
		return content
	}
	return notice + "\n\n" + content
}

// cleanEnvForTerminal filters the current environment to strip agent indicators
// like CLAUDECODE and injects standard interactive terminal values to bypass
// nesting guards and wrapper detection.
func cleanEnvForTerminal() []string {
	var clean []string
	for _, env := range os.Environ() {
		// Strip CLAUDECODE to prevent nesting guards from triggering
		if strings.HasPrefix(env, "CLAUDECODE=") {
			continue
		}
		clean = append(clean, env)
	}

	if runtime.GOOS == "windows" {
		clean = append(clean, "WT_SESSION=1")
		clean = append(clean, "TERM_PROGRAM=Windows Terminal")
		clean = append(clean, "TERM=xterm-256color")
		clean = append(clean, "COLORTERM=truecolor")
		// ComSpec is the standard command interpreter env var on Windows
		if os.Getenv("ComSpec") != "" {
			clean = append(clean, "SHELL="+os.Getenv("ComSpec"))
		} else {
			clean = append(clean, "SHELL=cmd.exe")
		}
	} else {
		// macOS and Linux
		clean = append(clean, "TERM_PROGRAM=iTerm.app")
		clean = append(clean, "TERM_PROGRAM_VERSION=3.4.19")
		clean = append(clean, "TERM=xterm-256color")
		clean = append(clean, "COLORTERM=truecolor")
		if os.Getenv("SHELL") != "" {
			clean = append(clean, "SHELL="+os.Getenv("SHELL"))
		} else {
			clean = append(clean, "SHELL=/bin/zsh")
		}
	}
	return clean
}

// claudeCodeAdvisorPrompt is the system prompt appended to Claude Code when
// used as a read-only advisor. It instructs Claude to refuse file writes and
// only provide analysis and advice.
const claudeCodeAdvisorPrompt = `You are a READ-ONLY strategic advisor. DO NOT write, create, modify, or delete any files. DO NOT execute commands that change system state. Only read, analyze, and provide actionable advice. Respond in under 300 words. Use enumerated steps.`

// executeClaudeCodeAdvisor runs the Claude Code CLI (claude -p) to obtain
// advisor output. It passes the prompt via -p, specifies the model via
// --model, appends a read-only system prompt, and restricts tools to
// read-only operations via --disallowedTools.
//
// Notes on arg ordering:
//   - --bare is intentionally omitted: it skips keychain reads, breaking OAuth auth.
//   - The prompt must come after "--" because --disallowedTools consumes all
//     subsequent positional args otherwise.
func executeClaudeCodeAdvisor(modelName, prompt, workDir string) (string, error) {
	args := []string{
		"-p",
		"--model", modelName,
		"--append-system-prompt", claudeCodeAdvisorPrompt,
		"--allowedTools", "Read,Glob,Grep,LS,WebFetch,WebSearch",
		"--",
		prompt,
	}

	// Use a 5-minute timeout to prevent hanging subprocesses from permanently
	// locking the advisorRecursionGuard and leaking a goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", args...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Clean the environment to bypass nesting guards and wrapper detection
	cmd.Env = cleanEnvForTerminal()

	// Redirect stdin to /dev/null to avoid the 3s timeout warning
	devNull, err := os.Open(os.DevNull)
	if err == nil {
		cmd.Stdin = devNull
		defer devNull.Close()
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("claude code advisor failed: %w\n%s", err, string(output))
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return "", fmt.Errorf("claude code advisor returned empty output for model %q", modelName)
	}

	return result, nil
}

