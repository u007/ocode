package server

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/debuglog"
	"github.com/u007/ocode/internal/skill"
)

// Skill enable/disable management endpoints.
//
// "Enabled" here means DISCOVERABLE: the skill appears in the base-prompt
// catalog, the discovery corpus, the Kaizen digest, the slash-command list and
// the context-budget breakdown, and the skill tool will load it. "Disabled"
// removes it from all of those AND makes the skill tool refuse it — but it does
// NOT retract a body that was already attached to an in-flight turn (see
// internal/skill/enabled.go).
//
// Scope: the sidebar toggles the PROJECT layer (.ocode/settings.json), because a
// skill preference is naturally a property of the repo it was made in. The
// global layer still applies as the default; an "enabled" entry at either scope
// overrides a "disabled" entry at either scope (config.EffectiveDisabledSkills).

// skillManageEntry is one row in the sidebar's Skills list.
type skillManageEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	WhenToUse   string `json:"when_to_use,omitempty"`
	Source      string `json:"source,omitempty"`
	Plugin      string `json:"plugin,omitempty"`
	Enabled     bool   `json:"enabled"`
}

// skillScopeGlobal is the query value that selects the machine-wide layer.
const skillScopeGlobal = "global"

// skillScope returns the requested preference scope and whether it was given.
//
// The scope selects the LAYER a toggle is written to, NOT the project the
// skills are discovered in. Those are two different things: a global toggle
// still has to resolve the skill NAME, and the name is resolved from the
// caller's project.
//
// WHY THIS EXISTS: the Settings → Skills form is a GLOBAL control ("a skill
// switched off here is disabled in every NEW chat on this machine"), but it
// cannot reach the global layer by OMITTING its params, because the save
// target falls back to h.workDir and h.workDir is never empty in a real
// server (NewHandler sets it to os.Getwd(), SetWorkDir overwrites it, and the
// desktop boot sets it to the last-used project). So an un-scoped request
// wrote the PROJECT layer of whatever project the server last used, and the
// form's promise was false.
//
// An unknown scope is an error, not a silent fall-through to the project
// layer: a typo must not quietly write the wrong layer.
func skillScope(r *http.Request) (string, bool, error) {
	if r == nil {
		return "", false, nil
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		return "", false, nil
	}
	if strings.EqualFold(scope, skillScopeGlobal) {
		return skillScopeGlobal, true, nil
	}
	return "", false, fmt.Errorf("unknown skill scope %q (want %q)", scope, skillScopeGlobal)
}

// skillPrefsRoot resolves the project root a skill preference applies to.
//
// Precedence: the session's project root (canonical, home-expanded), then an
// explicit ?path= (which MUST be an exact member of allowedProjectRoots — this
// endpoint writes into the tree), then the server's default project.
//
// The h.workDir fallback ONLY handles the explicit "no project param" case (a
// client that did not identify a session or a path). It is not a stand-in for a
// session's project root: when a session id IS supplied its registered root wins
// above, so session-scoped work always follows that session's project rather
// than the server's default. A final empty result means "no project", and the
// toggle writes the GLOBAL layer.
//
// The scope (?scope=global) does NOT participate here: it selects the LAYER the
// toggle is written to, while this function resolves the PROJECT the skills are
// discovered in.
//
// The cwd is deliberately never consulted: a Finder/Dock-launched desktop app
// boots with cwd "/", so resolving against it would read the wrong project and
// write .ocode/settings.json into "/".
func (h *Handler) skillPrefsRoot(r *http.Request) (string, error) {
	root, err := h.scopedSkillRoot(r)
	if err != nil || root != "" {
		return root, err
	}
	if h.workDir != "" {
		return h.workDir, nil
	}
	// No project: fall through to the GLOBAL layer. Deliberately NOT os.Getwd() —
	// a Finder/Dock-launched desktop app starts with cwd "/", which would both
	// read the wrong project's settings and write .ocode/settings.json into "/".
	return "", nil
}

// scopedSkillRoot resolves the project the CALLER named — its session's root, or
// an explicit ?path= that is a registered project — and returns "" when it named
// none. It never falls back to the server's default project: read-only surfaces
// shared across projects use it so an unscoped request stays project-agnostic.
func (h *Handler) scopedSkillRoot(r *http.Request) (string, error) {
	if r != nil {
		// The session registry holds the CANONICAL project root (already
		// home-expanded by projects.Add on the server that owns it), so it is
		// preferred over a client-supplied path. That matters for remote
		// projects, whose client-side path stays in "~/..." form.
		if sid := strings.TrimSpace(r.URL.Query().Get("session_id")); sid != "" {
			if entry := h.sessions.Lookup(sid); entry != nil && entry.ProjectRoot != "" {
				return entry.ProjectRoot, nil
			}
		}
		if path := strings.TrimSpace(r.URL.Query().Get("path")); path != "" {
			for _, root := range h.allowedProjectRoots() {
				if root == path {
					return path, nil
				}
			}
			return "", fmt.Errorf("path %q is not a project registered with this server", path)
		}
	}
	return "", nil
}

// HandleListManageableSkills lists the discoverable, non-Kaizen skills for a
// project together with their current effective enabled state.
//
// Kaizen (per-model tuned) skills are excluded on purpose: they are admitted
// only for a matching model+stack and are not user-toggleable. Disabled skills
// are INCLUDED (with enabled:false) — the sidebar shows them dimmed below the
// enabled ones so the user can switch them back on.
func (h *Handler) HandleListManageableSkills(w http.ResponseWriter, r *http.Request) {
	// The scope selects the LAYER whose state is reported; the root selects the
	// PROJECT the skills are discovered in. A global scope reports the global
	// layer alone (an empty root means "no project layer" to the resolver), so
	// the Settings form shows the machine-wide truth rather than the union with
	// whatever project the server last used.
	scope, global, err := skillScope(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	root, err := h.skillPrefsRoot(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stateRoot := root
	if global {
		stateRoot = ""
	}
	disabled := skill.DisabledForRoot(stateRoot)
	all := skill.LoadSkillsForRoot(root)

	out := make([]skillManageEntry, 0, len(all))
	for _, s := range all {
		if s.TunedFor != "" {
			continue // model-specific tuning skill: not user-toggleable
		}
		out = append(out, skillManageEntry{
			Name:        s.Name,
			Description: s.Description,
			WhenToUse:   s.WhenToUse,
			Source:      s.Source,
			Plugin:      s.Plugin,
			Enabled:     !disabled[s.Name],
		})
	}
	// Enabled first, then case-insensitive name. Mirrors the sidebar's intended
	// presentation (enabled on top, disabled dimmed below) so the client does
	// not have to re-sort, and keeps the API deterministic.
	//
	// Deliberately UNPAGINATED: the sidebar partitions the whole set into the
	// enabled group and the disabled group, so a page boundary would split a
	// group and make the toggle list look like skills vanished. A workspace has
	// tens of skills (not thousands), and the response carries no bodies — only
	// names and short descriptions — so the full list is cheap. If this ever
	// needs paging, page on the raw (pre-partition) order and have the client
	// merge, rather than capping a group.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Enabled != out[j].Enabled {
			return out[i].Enabled
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"project_root": stateRoot,
		"scope":        scope,
		"skills":       out,
	})
}

// HandleSetSkillEnabled flips one skill on/off and applies it to the live
// session.
//
// Persisting changes the effective set immediately; the caller's agent then has
// to be REBUILT because the catalog/corpus/digest are baked into the system
// prompt at construction. That rebuild is skipped while a turn is running
// (mirroring the MCP toggle) and instead lands on the next turn, which
// reconcileProfileAgent triggers by comparing the session's snapshotted
// disabled set against the current one.
//
// Only the REQUESTING session is rebuilt eagerly. Every other live session on the
// project (or every session, for a global toggle) keeps its old catalog only
// until its next turn: the catalog is consumed by the model, which can act only
// inside a turn, and each turn starts with reconcileProfileAgent comparing the
// session's snapshotted disabled set with the current one and rebuilding on a
// difference (TestReconcileProfileAgentRebuildsOnSkillToggle). The skill tool
// itself checks the live preference, so a stale catalog can never LOAD a
// disabled skill.
func (h *Handler) HandleSetSkillEnabled(w http.ResponseWriter, r *http.Request, name string, enabled bool) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid skill name %q", name))
		return
	}

	// The scope selects the LAYER the toggle is written to; the root selects
	// the PROJECT the skill is discovered and resolved in. A global scope
	// writes the global layer ("") but still resolves the name against the
	// caller's project.
	scope, global, err := skillScope(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	root, err := h.skillPrefsRoot(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The layer the preference is written to. The global scope maps to the
	// empty root, which config.SaveSkillEnabled writes to the machine-wide
	// ocodeconfig.json.
	saveRoot := root
	if global {
		saveRoot = ""
	}

	// Resolve the skill through the UNFILTERED loader: a currently disabled
	// skill must still be findable so it can be re-enabled. Refusing an unknown
	// name keeps a typo from writing a dead preference entry.
	resolved, err := skill.LoadSkillForRoot(name, root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if resolved == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("skill %s not found", name))
		return
	}
	if resolved.TunedFor != "" {
		writeError(w, http.StatusBadRequest, "model-specific (Kaizen) skills cannot be enabled or disabled")
		return
	}

	if err := config.SaveSkillEnabled(saveRoot, resolved.Name, enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The preference is cached (3s TTL) on the skill-loading path; a toggle must
	// be visible to the very next catalog build, so drop both caches now.
	skill.InvalidateSkillCache()

	sessionID := ""
	if r != nil {
		sessionID = strings.TrimSpace(r.URL.Query().Get("session_id"))
	}
	if sessionID != "" {
		h.rebuildAgentForSkills(sessionID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":         resolved.Name,
		"enabled":      enabled,
		"project_root": saveRoot,
		"scope":        scope,
		"session_id":   sessionID,
	})
}

// boolPtr returns a pointer to b. It exists so a snapshot field can
// distinguish "not recorded" (nil) from "recorded as false".
func boolPtr(b bool) *bool { return &b }

// sameSkillSet reports whether two effective disabled-name sets are equal. Nil
// and empty are equivalent (both mean "nothing disabled").
func sameSkillSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for name := range a {
		if !b[name] {
			return false
		}
	}
	return true
}

// rebuildAgentForSkills rebuilds one session's agent so a just-toggled skill
// takes effect in that chat only.
func (h *Handler) rebuildAgentForSkills(sessionID string) {
	h.rebuildAgentForPreferenceChange(sessionID, "skill")
}

// rebuildAgentForPreferenceChange force-rebuilds a session's agent after a
// preference that is baked into its construction (MCP tool set, skill catalog)
// changed. No-op when there is no live agent; deferred while a turn is running
// or starting so in-flight work keeps its agent (the next turn reconciles — for
// skills via reconcileProfileAgent's disabled-set comparison).
//
// Exclusion: the per-session turn lock is held by a turn job for its whole run
// (persist -> reconcile -> runTurn), so TryLock on it excludes both a running
// turn and one that is about to start; checking IsTurnActive alone left a window
// where a turn began between the check and the swap and its rows were lost from
// the rebuilt transcript. as.mu then guards the transcript snapshot against the
// other writers (auto-compaction splice, rewind). Lock order matches the turn
// path: turn lock -> as.mu -> h.mu.
func (h *Handler) rebuildAgentForPreferenceChange(sessionID, what string) {
	entry := h.sessions.Lookup(sessionID)
	if entry == nil {
		return
	}
	deferred := func(why string) {
		debuglog.Log.Append(debuglog.Entry{
			Kind:      debuglog.KindAgent,
			Message:   what + " toggle deferred: " + why + " for session " + sessionID,
			SessionID: sessionID,
		})
	}

	turnLock := h.sessionTurnLock(sessionID)
	if !turnLock.TryLock() {
		deferred("turn active")
		return
	}
	defer turnLock.Unlock()
	if h.sessions.IsTurnActive(sessionID) {
		deferred("turn active")
		return
	}

	// Looked up only now, under the turn lock, so the session we rebuild is the
	// one a starting turn would also see.
	as := h.lookupAgentSession(sessionID)
	if as == nil {
		return
	}
	if !as.mu.TryLock() {
		deferred("session busy")
		return
	}
	messages := append([]agent.Message(nil), as.messages...)
	newAs, stage, err := h.buildAgentSession(sessionID, as.model, messages, entry.ProjectRoot)
	as.mu.Unlock() // not held across replace: it shuts the old agent down
	if err != nil {
		log.Printf("serve error: rebuild agent for %s after %s toggle (stage %s): %v", sessionID, what, stage, err)
		return
	}
	newAs.agent.SetParentAdvisorInFlight(as.agent.AdvisorGuard())
	h.replaceAgentSession(sessionID, newAs)
}

// rebuildAgentsForFeatureChange rebuilds EVERY resident agent session after a
// feature toggle that is baked into the agent's construction (the
// suggested-responses prompt fragment).
//
// The suggested-responses toggle lives in the cached SYSTEM prompt, so an
// agent built before the toggle keeps the old fragment until it is rebuilt.
// Rebuilding is deferred while a turn is active (rebuildAgentForPreferenceChange),
// so in-flight work keeps its agent and the next turn picks the change up.
//
// h.mu is held only to SNAPSHOT the session ids — never across the rebuild
// itself, which constructs an agent (slow) and must not freeze the whole
// server (see the locking rules in docs/concepts/web-server-locking-and-liveness-rules.md).
func (h *Handler) rebuildAgentsForFeatureChange(what string) {
	h.mu.Lock()
	ids := make([]string, 0, len(h.agents))
	for id := range h.agents {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.rebuildAgentForPreferenceChange(id, what)
	}
}
