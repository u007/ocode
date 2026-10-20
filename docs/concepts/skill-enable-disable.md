---
type: Concept
title: Per-Project Skill Enable/Disable
description: 'How a skill is disabled or re-enabled per project from the web/desktop chat sidebar: the discoverability gate, global + project layering algebra, caching/rebuild contract, and API/UI surface.'
tags:
  - skills
  - sidebar
  - discovery
  - config
  - web
timestamp: 2026-10-20T05:56:00Z
---
# Per-Project Skill Enable/Disable

Feature: a **Skills** section in the web/desktop chat sidebar (CoworkSidebar) lets the user enable or disable a skill per project. This page documents the semantics, storage/layering, caching/rebuild contract, API, and UI.

## Semantics: "enabled" means discoverable, not pre-included

An *enabled* skill is one that is **advertised and loadable**. It is surfaced in four places plus the context-budget breakdown:

1. **Base-prompt skill catalog** — `skill.BuildCatalogForModel` (via `LoadSkillsForModel`).
2. **Discovery corpus** — `Agent.discoveryDocs` (`internal/agent/discovery_glue.go`).
3. **Kaizen digest block** — `KaizenDigestBlock` / `KaizenDigestAdmittedForModels` (via `KaizenSkillsForModel`).
4. **Slash-command list** — `GET /api/skills` and the web's `loadDynamicCommands`.
5. **Context-budget breakdown** — `internal/contextbudget`.

Disabling a skill does NOT pre-remove it from the system prompt; it removes it from every *offer* path above. The single gate is `skill.DisabledForRoot(root)` (the four offer surfaces resolve `root` as the session's project root, EXCEPT the slash list, which is global-layer only — see API).

### Disabling also makes the `skill` / `load_skill` tool refuse

The agent-facing `skill` / `load_skill` tool consults `skill.LoadEnabledSkillForRoot` (`internal/tool/misc.go`). For a disabled skill it returns `(nil, nil)` — **identical to "no such skill"**. This is deliberate:

- the model has nothing to retry, and
- the existence of a disabled skill does not leak into the transcript.

### Already-attached bodies are never retracted

`LoadSkillsForRoot` and `LoadSkillForRoot` stay **RAW / unfiltered** on purpose. `internal/agent/discovery_autoinject.go` (`loadAutoInjectBody`) resolves a skill that was already selected for the in-flight turn through the raw loader. Only the **offer paths** and **new explicit loads** are gated.

Rule to preserve: *"those already included just let it be."* Do NOT gate `LoadSkillForRoot`, and keep `LoadSkills()`/`LoadSkillsForModel()` filtered while `LoadSkillsForRoot()` stays raw.

## Storage and layering

Two layers, keyed by the same `{"skills":{"disabled":[…],"enabled":[…]}}` shape:

- **Global layer** — `ocodeconfig.json` → `config.OcodeConfig.Skills` (`config.SkillsConfig`).
- **Project layer** — `<project>/.ocode/settings.json`, same shape. Writes are a **key-preserving merge**; an empty block is deleted; writes are atomic (temp + rename) under a cross-process lock file `settings.json.lock`.

### Effective-set algebra

Single implementation: `config.resolveDisabledSet`.

```
effectiveDisabled = (global.Disabled ∪ project.Disabled) − (global.Enabled ∪ project.Enabled)
```

- An `enabled` entry at **either** scope overrides a `disabled` entry at **either** scope.
- Enable/disable **move** the name between the project's two lists (not just delete), so a project can rescue a globally disabled skill.
- Names are exact / case-sensitive identifiers (plugin skills are `plugin:name`). Lists are trimmed, deduped and sorted on load and before every save.
- `config.EffectiveDisabledSkills(root)` returns `(map[string]bool, error)`; an unreadable project file degrades to the global layer and returns the error rather than emptying every catalog.

## Caching and rebuild

- `skill.DisabledForRoot(root)` caches the resolved set for **3 seconds**; `skill.InvalidateSkillCache()` clears both the skills cache and this one. The toggle handler calls it.
- The catalog/corpus/digest are baked into the system prompt **at agent construction**, so a toggle must rebuild that chat's agent: `Handler.rebuildAgentForPreferenceChange(sessionID, "skill")` (shared with the MCP toggle). It is skipped while a turn is active.
- The deferral still lands: `agentSession.disabledSkills` snapshots the set at build time, and `Handler.reconcileProfileAgent` compares it with the current set on the next turn — a difference forces a rebuild. Applies to every live session in that project.

## API

- `GET /api/skills/manage?path=&session_id=` → `{project_root, skills:[{name, description, when_to_use, source, plugin, enabled}]}` — enabled first, then case-insensitive name. **Non-Kaizen skills only.** Disabled skills are included so they can be restored.
- `PUT /api/skills/{name}/enable` and `PUT /api/skills/{name}/disable?path=&session_id=`.
- Root resolution (`Handler.skillPrefsRoot`): `session_id` (canonical, already home-expanded project root from the session registry) → `path` (must be an exact member of `allowedProjectRoots()`) → the server's `workDir` → `""` (global layer). Deliberately **no `os.Getwd()` fallback** — a Finder/Dock-launched desktop app boots with cwd `/`.
- Unknown skill name → **404** and no preference entry written. Kaizen (per-model tuned) skills → **400** (not user-toggleable).
- `GET /api/skills` (installer/status + slash list) filters by the **GLOBAL layer only** (`skill.DisabledForRoot("")`) — it does NOT resolve the request's project. Rationale: the SPA fetches this endpoint ONCE for the whole page with no project scope, so filtering by any single project (the server default or a cached active tab) would let one project hide a skill from every other project — an over-filter, which is worse than the alternative. Consequence: a project-scoped disable may still appear in `/`-autocomplete, but the `skill`/`load_skill` tool refuses it (it resolves against the session's own project root) and the sidebar list shows it disabled. Its `GetSkillStatus()`-failure fallback uses the RAW loader (`LoadSkillsForRoot("")`) plus the same global filter — deliberately NOT the cwd's project layer.
- Web client: `api.getManageableSkills(host?, projectPath?)` and `api.setSkillEnabled(name, enabled, host?, projectPath?, sessionId?)`.

## Web UI

- CoworkSidebar **Skills** section (`DEFAULT_SECTIONS.skills = false`, collapsed by default). Enabled rows first, disabled rows dimmed below; count badge `enabled/total`.
- After a successful toggle the client refetches the list and calls `reloadDynamicCommands()` (`web/src/components/Chat/commands.ts`) so the `/`-autocomplete drops/restores the name without a page reload. `reloadDynamicCommands` removes only the names this module appended (never built-ins), re-fetches, notifies mounted `useCommands` consumers, and a generation counter drops additions from a superseded load.

## Tests

All mutation-verified:

- `internal/config/skill_prefs_test.go`
- `internal/skill/enabled_test.go`
- `internal/tool/skill_tool_test.go` (`TestSkillToolRefusesDisabledSkill`)
- `internal/server/handler_skills_test.go` (`TestReconcileProfileAgentRebuildsOnSkillToggle` covers the deferred path; `TestHandleListSkillsUsesGlobalLayerOnly` pins that a project-scoped disable does NOT leak into `/api/skills`, while a global one does)
- `internal/server/handler_skills_routes_test.go` (real-mux route test — a handler-only test cannot catch a missing or shadowed route)
- `web/src/components/Layout/CoworkSidebar.skills.test.tsx`
- `web/src/components/Chat/commands.dynamic.test.tsx`

## Known limitation

The web slash list is filtered by the GLOBAL layer only, so a project-scoped disable still autocompletes (the `skill`/`load_skill` tool enforces it, and the sidebar shows it off).

Full per-tab scoping is a recorded TODO (`TODO.md`, "skill enable/disable follow-ups"): make the command registry per-tab (`useCommands(scope)` from `resolveSessionHost` + `activeTabId`, with `api.listSkills(host, path)` forwarding the scope), or drop skill names from the static registry and let autocomplete query `GET /api/skills/manage` for the active session.

## Design note (deliberate split)

`LoadSkills()` / `LoadSkillsForModel()` are filtered; `LoadSkillsForRoot()` stays raw. `skill.LoadSkillForRoot` must NOT be gated.
