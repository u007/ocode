package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

// Default advisor model when OPENCODE_ADVISOR_MODEL is not set.
const defaultAdvisorModel = "deepseek/deepseek-v4-pro"

// advisorSystemPrompt instructs the advisor model on its role. It is updated
// to inform the model that it has tools available for exploring the codebase.
const advisorSystemPrompt = `You are a strategic advisor for a coding agent. You have access to tools that let you explore the codebase and research external resources. Use them as needed to understand the context before advising.

Available tools:
- read, glob, grep, list, lsp — explore the local codebase
- bash — run shell commands (git log, cat, etc.)
- webfetch, websearch — research external documentation
- repo_clone, repo_overview — inspect external libraries

Your advice must be actionable — tell the executor:
- What to do next
- What order to proceed in
- What to watch out for
- What not to do

Key heuristics:
- Prefer the simplest approach that meets the spec
- Flag approaches that create maintenance burden
- If the executor is stuck or looping, suggest a different approach
- If tests or evidence contradict an assumption, say so explicitly

Before advising, use your tools to gather context. Then respond in under 300 words. Use enumerated steps. Do NOT write production code — only advise.`

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
}

func (t AdvisorTool) Name() string { return "advisor" }
func (t AdvisorTool) Description() string {
	return "Consult a strategic advisor (backed by a stronger reviewer model, configurable via OPENCODE_ADVISOR_MODEL; defaults to DeepSeek V4 Pro) that can explore the codebase with tools and provide a concise plan or course correction."
}
func (t AdvisorTool) Parallel() bool { return false }

func (t AdvisorTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "advisor",
		"description": "Consult a strategic advisor (backed by a stronger reviewer model, configurable; defaults to DeepSeek V4 Pro). The advisor can explore the codebase using tools before providing advice.\n\nCall advisor BEFORE substantive work — before writing code, editing files, committing to an interpretation, or building on an assumption. If the task requires orientation first (finding files, reading code, fetching docs), do that, then call advisor. Orientation is NOT substantive work, though the advisor can also do its own orientation if you feed it limited context.\n\nAlso call advisor:\n- When stuck — errors recurring, approach not converging, results that don't fit\n- When considering a change of approach\n- When you believe the task is complete. BEFORE this call, make your deliverable durable: write the file, save the result, commit the change\n\nOn tasks longer than a few steps, call advisor at least once before committing to an approach and once before declaring done. On short reactive turns where tool output directly dictates the next action, skip advisor.\n\nOptional tool args providerID and modelID override the preset model for this one call; leave them blank to use the configured default.\n\nRequired tool arg prompt must include the context the advisor needs to start exploring and provide useful advice.\n\nGive the advice serious weight. Only override if you have primary-source evidence that contradicts a specific claim. Surface conflicts in another advisor call rather than silently switching approaches.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "All context the advisor needs to provide guidance — current state, goal, obstacles, approach considered. The advisor may also use its own tools to explore further.",
				},
				"providerID": map[string]interface{}{
					"type":        "string",
					"description": "Optional provider override for this advisor call (e.g. \"anthropic\"). Leave blank to use configured default.",
				},
				"modelID": map[string]interface{}{
					"type":        "string",
					"description": "Optional model override for this advisor call (e.g. \"claude-sonnet-4-6\"). Leave blank to use configured default.",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

// resolveAdvisorModel returns the model string to use for the advisor.
// Priority: per-call overrides > OPENCODE_ADVISOR_MODEL env var > config > default.
func (t AdvisorTool) resolveModel(providerID, modelID string) string {
	// 1. Per-call overrides take highest priority.
	if providerID != "" && modelID != "" {
		return providerID + "/" + modelID
	}
	if providerID != "" && modelID == "" {
		// provider given but no model — use env/config default's model with this provider
		if cfgModel := t.configModel(); cfgModel != "" {
			return providerID + "/" + cfgModel
		}
		return providerID + "/deepseek-v4-pro"
	}

	// 2. OPENCODE_ADVISOR_MODEL env var.
	envModel := os.Getenv("OPENCODE_ADVISOR_MODEL")
	if envModel != "" {
		return envModel
	}

	// 3. Config from ocode.json [advisor] section.
	if t.cfg != nil {
		ac := t.cfg.Ocode.Advisor
		if ac.Provider != "" && ac.Model != "" {
			return ac.Provider + "/" + ac.Model
		}
		if ac.Model != "" {
			return ac.Model
		}
	}

	// 4. Built-in default.
	return defaultAdvisorModel
}

// configModel returns just the model name from config (no provider prefix).
func (t AdvisorTool) configModel() string {
	if t.cfg == nil {
		return ""
	}
	return t.cfg.Ocode.Advisor.Model
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
		Prompt     string `json:"prompt"`
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
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

	// Resolve the model to use for this call.
	modelStr := t.resolveModel(params.ProviderID, params.ModelID)
	emitDebug("ADVISOR", fmt.Sprintf("calling model: %s", modelStr))

	// Create an LLM client for the advisor model (may differ from the main agent's model).
	client := NewClient(t.cfg, modelStr)
	if client == nil {
		return "", fmt.Errorf("could not create client for advisor model %q: check provider credentials and config", modelStr)
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
			return strings.TrimSpace(resp.Content), nil
		}

	advisorAgent := NewAgent(client, advisorTools, t.cfg)

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
	return result, nil
}
