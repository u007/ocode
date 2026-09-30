package agent

import (
	"strings"
	"sync/atomic"

	"github.com/u007/ocode/internal/config"
)

// AdvisorConfig is a chat session's resolved advisor configuration: which
// model the advisor call uses, whether it goes through the Claude Code CLI,
// and which completion triggers (checkpoints) fire.
//
// It is PER SESSION, not global. The process-wide config value only seeds a
// session's first resolution (see the server's pin-on-first-use); after that
// the session's own copy governs, so changing the global default never
// retroactively changes an existing chat.
type AdvisorConfig struct {
	// Model is the fully qualified "provider/model" the advisor call uses.
	// Empty means "use the built-in default", and that is a REAL value: a
	// session pinned to the default must resolve to the default rather than
	// falling through to whatever the global default has since become.
	Model string

	// ClaudeCode routes the call through the Claude Code CLI (`claude -p`)
	// instead of an LLM API client.
	ClaudeCode bool

	// Checkpoints is the session's trigger set. Only the names present here
	// fire (see advisor_checkpoint.go).
	Checkpoints []string
}

// AdvisorConfigFromConfig projects the process-wide default onto a session.
// Checkpoints are copied so a later in-place edit of the config slice cannot
// reach into an already-built agent.
func AdvisorConfigFromConfig(cfg *config.Config) AdvisorConfig {
	if cfg == nil {
		return AdvisorConfig{}
	}
	ac := cfg.Ocode.Advisor
	out := AdvisorConfig{
		Model:      ac.QualifiedModel(),
		ClaudeCode: ac.ClaudeCode,
	}
	if len(ac.Checkpoints) > 0 {
		out.Checkpoints = append([]string(nil), ac.Checkpoints...)
	}
	return out
}

// SetAdvisorConfig installs the session's advisor configuration. The handler
// calls this at agent-build time (seeded from the session's own pinned value)
// and again whenever the user changes the model or triggers in that chat, so a
// resident agent and the UI never disagree.
func (a *Agent) SetAdvisorConfig(cfg AdvisorConfig) {
	a.advisorConfig.Store(&cfg)
}

// SetParentAdvisorConfig wires a sub-agent to the parent's advisor
// configuration. Reactive, mirroring SetParentAdvisorEnabled: without it a
// sub-agent's advisor call would fall back to the process-wide seed instead of
// the chat's own model and triggers.
func (a *Agent) SetParentAdvisorConfig(parent *atomic.Pointer[AdvisorConfig]) {
	a.parentAdvisorConfig = parent
}

// ResolvedAdvisorConfig returns the session's advisor configuration. ok=false
// means no per-session configuration was ever installed, in which case callers
// fall back to the process-wide default. A config whose Model is empty still
// returns ok=true: that is a session deliberately pinned to the built-in
// default, and it must NOT fall through to the global.
func (a *Agent) ResolvedAdvisorConfig() (AdvisorConfig, bool) {
	slot := &a.advisorConfig
	if a.parentAdvisorConfig != nil {
		slot = a.parentAdvisorConfig
	}
	cfg := slot.Load()
	if cfg == nil {
		return AdvisorConfig{}, false
	}
	return *cfg, true
}

// AdvisorModel returns the session's qualified advisor model, or "" when the
// session uses the built-in default.
func (a *Agent) AdvisorModel() string {
	cfg, _ := a.ResolvedAdvisorConfig()
	return cfg.Model
}

// AdvisorCheckpoints returns a copy of the session's trigger set.
func (a *Agent) AdvisorCheckpoints() []string {
	cfg, _ := a.ResolvedAdvisorConfig()
	if len(cfg.Checkpoints) == 0 {
		return nil
	}
	return append([]string(nil), cfg.Checkpoints...)
}

// advisorEffectiveConfig is the tool-side view of the same value: the calling
// session's configuration when it has one, otherwise the process-wide default.
// Exported through the tool so the Claude Code branch and resolveModel agree.
func (t AdvisorTool) advisorEffectiveConfig() AdvisorConfig {
	if t.mainAgent != nil {
		if cfg, ok := t.mainAgent.ResolvedAdvisorConfig(); ok {
			return cfg
		}
	}
	return AdvisorConfigFromConfig(t.cfg)
}

// claudeCodeModelName strips the provider prefix from a qualified advisor model
// for the Claude Code CLI, which wants a bare model name ("claude-sonnet-5-5",
// not "claude-code/claude-sonnet-5-5").
func claudeCodeModelName(qualified string) string {
	name := qualified
	if i := strings.Index(qualified, "/"); i >= 0 {
		name = qualified[i+1:]
	}
	if strings.TrimSpace(name) == "" {
		return "claude-sonnet-4-6"
	}
	return name
}
