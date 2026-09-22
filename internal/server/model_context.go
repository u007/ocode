package server

import (
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/skill"
)

// buildModelPromptInfo assembles the indicator data for (root, model): the
// matched model-specific custom prompt (kind/path + token estimate) and the
// admitted force-injected Kaizen tuning directives. Returns nil when the model
// has neither — the web then renders no banner, matching the TUI's row which is
// absent for untuned models.
//
// The prompt CONTENT is used only for the token estimate and is not included in
// the payload: this rides the "status" SSE event, which fans out on state
// changes, so it must stay small.
func buildModelPromptInfo(root, model string) *ModelPromptInfo {
	if model == "" {
		return nil
	}
	info := &ModelPromptInfo{}
	res := agent.LoadModelContextWithSourceAt(root, model)
	if res.Kind != "" {
		info.Kind = res.Kind
		info.Path = res.Path
		info.Tokens = len(res.Content) / 4
	}
	for _, s := range skill.KaizenSkillsForModel(root, model) {
		if s.Digest == "" {
			continue // only digest-bearing skills are force-injected
		}
		info.Kaizen = append(info.Kaizen, KaizenDirectiveInfo{
			Name:     s.Name,
			TunedFor: s.TunedFor,
			Stack:    s.Stack,
		})
	}
	if info.Kind == "" && len(info.Kaizen) == 0 {
		return nil
	}
	return info
}

// applySessionModelPrompt re-resolves the "◆ Model prompt" indicator for a
// session-tagged snapshot after the caller has replaced MainModel and/or CWD
// with the session's effective values (per-session model override, or a session
// bound to another project root).
//
// buildStatusSnapshot resolves the banner from the process-wide cfg.Model +
// server workDir, so without this a session with its own model override would
// pair the NEW model id with the GLOBAL model's .OCODE.md + Kaizen line — the
// stale "conduct" banner the desktop/web sidebar showed after a model switch.
//
// baseModel/baseCWD are the values the snapshot carried before the caller's
// overrides. When neither changed, the banner buildStatusSnapshot already
// computed is kept so the common path pays no second disk/git scan (resolving a
// .OCODE.md runs git for tracked files — see agent.readContextFileAt).
func applySessionModelPrompt(snap *TUIStatus, baseModel, baseCWD string) {
	if snap.MainModel == baseModel && snap.CWD == baseCWD {
		return
	}
	snap.ModelPrompt = buildModelPromptInfo(snap.CWD, snap.MainModel)
}
