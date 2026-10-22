package skill

import (
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/config"
)

// Disabled-skill gate.
//
// A skill the user switched off is NOT DISCOVERABLE: every advertisement path
// (base-prompt catalog, discovery corpus, Kaizen digest, slash-command list,
// context-budget breakdown) drops it, and LoadEnabledSkillForRoot refuses to
// resolve it for the agent's skill tool.
//
// Deliberately NOT filtered: LoadSkillsForRoot / LoadSkillForRoot stay raw.
// internal/agent's loadAutoInjectBody uses the raw resolver to fetch the body of
// a skill that was ALREADY selected for an in-flight turn — disabling must not
// retroactively break that. Filtering happens where a skill is OFFERED or where
// a NEW explicit load is requested, never where an existing attachment is
// materialized.
//
// The preference file is user-edited JSON, so the resolved set is cached for a
// short TTL to keep it off the per-turn catalog path; InvalidateSkillCache (the
// same hook skill install/remove already uses) clears it immediately after a
// toggle.

const disabledCacheTTL = 3 * time.Second

type disabledCacheEntry struct {
	disabled map[string]bool
	ts       time.Time
}

var (
	disabledCacheMu sync.Mutex
	disabledCache   = map[string]disabledCacheEntry{}
)

// DisabledForRoot returns the effective disabled-name set for a project root,
// layering the global config over the project's .ocode/settings.json. A blank
// root means "no project layer" — the global lists only — which is exactly what
// config.SaveSkillEnabled does for a blank root, so a read and a write always
// address the same scope.
//
// The returned map is SHARED cache state — callers must not mutate it.
//
// A read error degrades to whatever layer could be read (never to "disable
// everything"): this sits on the skill loading path, and failing closed here
// would silently empty every catalog. A DEGRADED result is never cached as if it
// were the truth: a transient read of a file mid-edit would otherwise re-advertise
// every disabled skill for the whole TTL, and flip the catalog (and so rebuild the
// agent) twice. While reads fail, the last good set stays in force; with none yet
// it is returned uncached so the next call retries. The skill package has no
// logger, so the error itself is surfaced to the caller that cares (the sidebar)
// by config.EffectiveDisabledSkills.
func DisabledForRoot(root string) map[string]bool {
	key := prefsRoot(root)
	disabledCacheMu.Lock()
	if e, ok := disabledCache[key]; ok && time.Since(e.ts) < disabledCacheTTL {
		out := e.disabled
		disabledCacheMu.Unlock()
		return out
	}
	disabledCacheMu.Unlock()

	disabled, err := config.EffectiveDisabledSkills(key)
	if disabled == nil {
		disabled = map[string]bool{}
	}

	disabledCacheMu.Lock()
	defer disabledCacheMu.Unlock()
	if err != nil {
		if prev, ok := disabledCache[key]; ok {
			// Keep the last good set, and re-stamp it so a persistently broken
			// file is retried once per TTL rather than on every call.
			prev.ts = time.Now()
			disabledCache[key] = prev
			return prev.disabled
		}
		return disabled
	}
	disabledCache[key] = disabledCacheEntry{disabled: disabled, ts: time.Now()}
	return disabled
}

// IsDisabledForRoot reports whether one skill is switched off for root.
func IsDisabledForRoot(root, name string) bool {
	if name == "" {
		return false
	}
	return DisabledForRoot(root)[name]
}

// ExcludeDisabledForRoot drops disabled skills from a listing. The input slice is
// never mutated (callers share cached slices).
func ExcludeDisabledForRoot(root string, in []Skill) []Skill {
	disabled := DisabledForRoot(root)
	if len(disabled) == 0 || len(in) == 0 {
		return in
	}
	out := make([]Skill, 0, len(in))
	for _, s := range in {
		if disabled[s.Name] {
			continue
		}
		out = append(out, s)
	}
	return out
}

// LoadEnabledSkillForRoot resolves a skill by name like LoadSkillForRoot, but
// refuses a disabled skill.
//
// The refusal returns (nil, nil) — the SAME result LoadSkillForRoot uses for
// "no such skill" — so the agent's tool cannot distinguish a disabled skill from
// a missing one. That is the intent: a disabled skill is fully off, and a
// distinct "this skill is disabled" reply would both leak its existence and give
// the model a reason to retry.
func LoadEnabledSkillForRoot(name, root string) (*Skill, error) {
	s, err := LoadSkillForRoot(name, root)
	if err != nil || s == nil {
		return s, err
	}
	if IsDisabledForRoot(root, s.Name) || IsDisabledForRoot(root, name) {
		return nil, nil
	}
	return s, nil
}

// InvalidateDisabledSkillCache clears the resolved-preference cache. Called from
// InvalidateSkillCache so one hook covers both a skills-directory change and a
// preferences change.
func InvalidateDisabledSkillCache() {
	disabledCacheMu.Lock()
	disabledCache = map[string]disabledCacheEntry{}
	disabledCacheMu.Unlock()
}

// prefsRoot normalizes a root for preference resolution. A blank root means "no
// project layer" (the global lists only), which is exactly what
// config.SaveSkillEnabled does for a blank root — read and write must agree or a
// toggle would read one scope and write the other.
//
// Deliberately NOT resolved against the process cwd, unlike LoadSkillsForRoot:
// a web/desktop server boots with cwd "/", and the callers that want a project
// layer (the TUI, the server handler) always pass their real project root.
func prefsRoot(root string) string {
	return strings.TrimSpace(root)
}
