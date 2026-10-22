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
4. **Slash-command list** — `GET /api/skills` and the web's `loadDynamicCommands` (project-scoped when the caller names a project — see API).
5. **Context-budget breakdown** — `internal/contextbudget`.

Disabling a skill does NOT pre-remove it from the system prompt; it removes it from every *offer* path above. The single gate is `skill.DisabledForRoot(root)` (the offer surfaces resolve `root` as the session's project root; the slash list does too when the request names a project, and is global-layer only when it does not — see API).

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
- `config.EffectiveDisabledSkills(root)` returns `(map[string]bool, error)`. The two layers load **independently** and the map is never nil: an unreadable project file degrades to the global layer, an unreadable global config degrades to the project layer, and every layer error comes back (joined) rather than emptying every catalog or hiding a readable layer.
- Project `.ocode/settings.json` is edited by **splicing only the `skills` member** into the file's own bytes (`spliceTopLevelKey`, `internal/config/json_splice.go`). Key order, indentation, other values and `//` / `/* */` comments survive a toggle, so a git-tracked file diffs by the skills lines only. Comments are tolerated on read (blanked, not removed, so byte offsets stay valid). A file that is not a JSON(C) object — including one with a trailing comma — is refused with an error and never overwritten.

## Caching and rebuild

- `skill.DisabledForRoot(root)` caches the resolved set for **3 seconds**; `skill.InvalidateSkillCache()` clears both the skills cache and this one. The toggle handler calls it. A **degraded** read (any layer errored) is never cached as the truth: the last good set stays in force (re-stamped so a broken file is retried once per TTL), and with none yet the degraded set is returned uncached. Otherwise a file caught mid-edit would re-advertise every disabled skill for the TTL and flip the catalog (rebuilding the agent) twice.
- The catalog/corpus/digest are baked into the system prompt **at agent construction**, so a toggle must rebuild that chat's agent: `Handler.rebuildAgentForPreferenceChange(sessionID, "skill")` (shared with the MCP toggle). It takes the per-session turn lock with `TryLock` (a turn job holds it for its whole run, so this excludes a turn that is running *or about to start*), then `as.mu` for the transcript snapshot, and defers when either is busy. Only the requesting session is rebuilt eagerly.
- The deferral still lands: `agentSession.disabledSkills` snapshots the set at build time, and `Handler.reconcileProfileAgent` compares it with the current set on the next turn — a difference forces a rebuild. Applies to every live session in that project, which is why the other sessions need no eager rebuild: the catalog only matters inside a turn, and every turn starts with that comparison. The `skill` tool reads the live preference, so a stale catalog can never *load* a disabled skill.

## API

- `GET /api/skills/manage?path=&session_id=` → `{project_root, skills:[{name, description, when_to_use, source, plugin, enabled}]}` — enabled first, then case-insensitive name. **Non-Kaizen skills only.** Disabled skills are included so they can be restored.
- `PUT /api/skills/{name}/enable` and `PUT /api/skills/{name}/disable?path=&session_id=`.
- Root resolution (`Handler.skillPrefsRoot`): `session_id` (canonical, already home-expanded project root from the session registry) → `path` (must be an exact member of `allowedProjectRoots()`) → the server's `workDir` → `""` (global layer). Deliberately **no `os.Getwd()` fallback** — a Finder/Dock-launched desktop app boots with cwd `/`.
- Unknown skill name → **404** and no preference entry written. Kaizen (per-model tuned) skills → **400** (not user-toggleable).
- `GET /api/skills` (installer/status + slash list) is **project-scoped when the caller names a project** (`?path=` — must be a registered root, else 400 — or `?session_id=`, resolved by `Handler.scopedSkillRoot`, which is `skillPrefsRoot` minus the `workDir` fallback) and then honours that project's full effective disabled set. **Unscoped**, it is the shared project-agnostic list: the GLOBAL layer only (`skill.DisabledForRoot("")`), so one project's disable cannot hide a skill from another. Its `GetSkillStatus()`-failure fallback lists `skill.LoadSkillsForRoot(root)` when scoped and `skill.LoadGlobalSkills()` (user-global + bundled, **never the process cwd**) when not.
- Web client: `api.getManageableSkills(host?, projectPath?)`, `api.setSkillEnabled(name, enabled, host?, projectPath?, sessionId?)` and `api.listSkills(host?, projectPath?)`.

## Web UI

- CoworkSidebar **Skills** section (`DEFAULT_SECTIONS.skills = false`, collapsed by default). Enabled rows first, disabled rows dimmed below; count badge `enabled/total`.
- The slash list follows the sidebar's active project: the skills effect calls `setDynamicCommandsScope({host, projectPath})` (a no-op when unchanged), and after a successful toggle the client refetches the list and calls `reloadDynamicCommands({host, projectPath})` (`web/src/components/Chat/commands.ts`) so the `/`-autocomplete drops/restores the name without a page reload. `reloadDynamicCommands` removes only the names this module appended (never built-ins), re-fetches, notifies mounted `useCommands` consumers, and a generation counter drops additions from a superseded load.

## Global Skills settings form (new, Settings panel)

A new **Skills** group in the web/desktop Settings panel (`web/src/components/Settings/SkillsForm.tsx`, registered at `SettingsPanel.tsx:74` as `{id:"skills",label:"Skills"}` and rendered at line 201-202 via `<SkillsForm />`) lets the user toggle discoverable skills globally, independent of any single chat's sidebar. It appears between the existing "Plugins & Local Models" (`line 117-118`) and "Theme" (`line 119`) groups.

Scope difference — the Settings form writes the **GLOBAL layer only**: `SkillsForm.tsx:7-10` notes it sends no project path (`api.getManageableSkills()` at `line 23` has no `host` or `projectPath` arguments), so the server resolves the preference against `ocodeconfig.json` (`config.SkillsConfig`, `internal/config/ocodeconfig.go:793`). By contrast, the CoworkSidebar toggle writes the **PROJECT layer** (`.ocode/settings.json`, or `ocodeconfig.json` when the session's root is blank — exactly the same `key-preserving merge` documented under "Storage and layering" above). The layering algebra is unchanged: `effectiveDisabled = (global.Disabled ∪ project.Disabled) − (global.Enabled ∪ project.Enabled)` (`resolveDisabledSet`, `internal/config/skill_prefs.go:239`); a skill switched off in Settings is disabled in every NEW chat on that machine, while the sidebar's per-project entry can rescue (or suppress) it for that repo only.

The form does NOT reach into live sessions: `SkillsForm.tsx:11` states "Already-running chats keep their current catalog until restarted"; `SkillsForm.tsx:81` repeats "Running chats are not changed mid-turn". This differs from the sidebar path, which triggers `Handler.rebuildAgentForPreferenceChange` (shared with the MCP toggle) and is skipped only while a turn is active. The Settings toggle is pure preference persistence (global layer); no agent rebuild happens.

Presentation (`SkillsForm.tsx:61-89`): `orderedSkills` sorts by `enabled` first (`line 61-64`: `a.enabled ? -1 : 1`) then alphabetical by `name`; a count label `enabledCount/skills.length` (`line 65`, `line 88`) shows `N/M enabled`; disabled rows are dimmed (`line 106`, `line 113`: `text-muted-foreground`) with a dimmed `●` / `○` indicator; enabled rows are `text-foreground` with an emerald `●`. Each row is a `<label>` (`line 99`) wrapping a checkbox (`line 129-136`) plus a truncated `font-mono` name and optional description (`line 111-126`). The form loads with a `Loader2` spinner (`line 68`), shows an error banner (`line 85`), applies optimistic flip (`line 36-44`: `next = !current.enabled` written to local state before the API call) with rollback (`line 52-55`) on `api.setSkillEnabled` failure (`line 46`), then reconciles with a fresh `getManageableSkills` fetch (`line 47`).

Kaizen (per-model tuned) skills are excluded from the listing (`GET /api/skills/manage` returns non-Kaizen only — `internal/server/handler_skills.go:84`; the PUT rejects a Kaizen name with 400 at `internal/server/handler_skills.go:172`), as the page above documents — they are admitted only for a matching model+stack and are not user-toggleable from either surface. The Settings form shares the same `ManageableSkill` shape (`Name`, `Description`, `WhenToUse`, `Source`, `Plugin`, `Enabled`) as the sidebar.

Regression test: `web/src/components/Settings/SkillsForm.test.tsx` (new). It stubs `api.getManageableSkills` (`line 23`) and `api.setSkillEnabled` (`line 46`) with `vi.fn()`, seeds three skills (`SKILLS`: `alpha` enabled, `beta` disabled, `gamma` enabled), and verifies list rendering, optimistic toggle, rollback on failure, and that disabled skills appear dimmed.

## Tests

All mutation-verified:

- `internal/config/skill_prefs_test.go`
- `internal/skill/enabled_test.go`
- `internal/tool/skill_tool_test.go` (`TestSkillToolRefusesDisabledSkill`)
- `internal/server/handler_skills_test.go` (`TestReconcileProfileAgentRebuildsOnSkillToggle` covers the deferred path; `TestHandleListSkillsScoping` pins that a scoped `/api/skills` honours the project's disables, the unscoped list does not leak them, and a global one hides everywhere; `TestRebuildAgentForPreferenceChangeRespectsTurnLock` pins the rebuild exclusion; `TestLoadGlobalSkillsIgnoresProcessCwd`)
- `internal/config/json_splice_test.go` (byte-preserving edits, comments, malformed refusal)
- `internal/server/handler_skills_routes_test.go` (real-mux route test — a handler-only test cannot catch a missing or shadowed route)
- `web/src/components/Layout/CoworkSidebar.skills.test.tsx`
- `web/src/components/Chat/commands.dynamic.test.tsx`

## Known limitation

The slash list is one module-global registry (`COMMANDS`), not per tab: it is scoped to the **sidebar's active project** and reloads when that changes. A window showing several projects at once shows the active one's list. Making the registry per-tab (`useCommands(scope)`) remains a recorded TODO (`TODO.md`, "skill enable/disable follow-ups"). Enforcement never depends on it: the `skill`/`load_skill` tool refuses a disabled skill against the session's own project root.

## Design note (deliberate split)

`LoadSkills()` / `LoadSkillsForModel()` are filtered; `LoadSkillsForRoot()` stays raw. `skill.LoadSkillForRoot` must NOT be gated.
