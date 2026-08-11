# Configuration UI Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a **Settings** top-level tab to the web/desktop React app (shared UI surface) that groups every `OcodeConfig`/`opencode.json` setting into an **ocode** and an **opencode** nav section, per `docs/superpowers/specs/2026-08-11-configuration-ui-design.md`. This is Plan 2 of 2 — it consumes the REST endpoints added in `docs/superpowers/plans/2026-08-11-configuration-api-backend.md` (Plan 1), which must be implemented first.

**Architecture:** `TopTabs.tsx` gets a sixth entry (`settings`) in its `mainTabs` array; `App.tsx`'s `activeView` union type and `TabsContent` switch get a matching `"settings"` case rendering a new `SettingsPanel.tsx`. `SettingsPanel` is a left-nav/right-pane shell (two headed groups: ocode, opencode); each nav item renders one small, focused form component under `web/src/components/Settings/`, each with its own load-on-mount + save pattern copied from the existing `PluginsPanel.tsx` precedent (arrow-function API calls on the `api` object, inline `error`/`loading`/`saving` state — **not** a toast library, this codebase doesn't use one). Settings-like sections (`models`, `theme`, `tools`, `paths`) are removed from `CoworkSidebar.tsx`, with the live main-model picker trigger relocated to a compact single-row control so `ModelDialog.tsx` (out of scope, unchanged) stays reachable.

**Tech Stack:** React 18, TypeScript, Tailwind, shadcn/ui (`Dialog`/`Button`/`Input`/`Select` primitives already in `web/src/components/ui/`), no new dependencies. Wails v3 for the desktop native menu (Go).

## Global Constraints

- No toast/snackbar library exists in this codebase — every form uses the `PluginsPanel.tsx` pattern: `useState` for `loading`/`saving`/`error`, an inline `{error && <div className="... text-red-400 ...">{error}</div>}` block, and `Loader2` spinner icons on buttons during async work.
- Every API call goes through a new arrow function added to the single exported `api` object in `web/src/api/client.ts`, following the existing `get<Thing>`/`set<Thing>` naming convention and calling the shared `fetchJSON<T>(path, init?)` helper — never call `fetch` directly from a component.
- No automated frontend test suite exists in `web/` (confirmed in the design spec, section 6) — skip writing new test files. Each task instead ends with `bun run typecheck` (this repo's rule: **always use `tsgo` via `bun run typecheck`, never raw `tsc`**) and a manual verification step using the `run` skill.
- Match existing Tailwind/zinc-dark visual style already used in `CoworkSidebar.tsx`/`PluginsPanel.tsx` (`bg-zinc-900`, `border-zinc-700`, `text-zinc-300`/`text-zinc-500`, `hover:bg-zinc-800`) — the Settings pane is a new surface, not a redesign.
- Every new/extended field name in a form's request/response type must exactly match the JSON key names defined in Plan 1 (backend) — cross-check against `docs/superpowers/specs/2026-08-11-configuration-ui-design.md` section 3 before writing a form's types.
- Files: one form = one file, under `web/src/components/Settings/`. Do not grow `SettingsPanel.tsx` itself into a monolith — it only holds nav state and dispatches to form components.

---

### Task 1: Settings tab wiring and SettingsPanel shell

**Files:**
- Modify: `web/src/components/Layout/TopTabs.tsx:1-18` (imports, `mainTabs` array)
- Modify: `web/src/App.tsx:108-110` (activeView union type), `App.tsx:341-382` (TabsContent switch — insert alongside `git`/`cron`/`assets`)
- Create: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Produces: `<SettingsPanel />` component (no props) rendered when `activeView === "settings"`; a `SettingsGroupId` string union type (`"model-defaults" | "commit-msg" | "compact" | "advisor" | "permissions" | "security" | "terminal" | "ocr" | "discovery" | "tui" | "editor" | "imagegen" | "paths" | "limits" | "features" | "plugins" | "theme" | "opencode-mcp" | "opencode-plugins" | "opencode-model-state"`) that later tasks' form components key off of.

- [x] **Step 1: Add the Settings entry to `TopTabs.tsx`**

```tsx
// web/src/components/Layout/TopTabs.tsx — add Settings to the icon imports
import { FolderGit2, GitBranch, Paperclip, CalendarClock, MessageSquare, MoreHorizontal, Settings } from "lucide-react";
```

```tsx
const mainTabs = [
  { id: "sessions", label: "Sessions", icon: MessageSquare },
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "cron", label: "Cron", icon: CalendarClock },
  { id: "assets", label: "Assets", icon: Paperclip },
  { id: "settings", label: "Settings", icon: Settings },
];
```

- [x] **Step 2: Add `"settings"` to `App.tsx`'s `activeView` union**

```tsx
  // web/src/App.tsx:108-110
  const [activeView, setActiveView] = useState<
    "files" | "git" | "cron" | "assets" | "sessions" | "settings"
  >("sessions");
```

- [x] **Step 3: Create the `SettingsPanel` shell**

```tsx
// web/src/components/Settings/SettingsPanel.tsx
import { useState } from "react";

export type SettingsGroupId =
  | "model-defaults"
  | "commit-msg"
  | "compact"
  | "advisor"
  | "permissions"
  | "security"
  | "terminal"
  | "ocr"
  | "discovery"
  | "tui"
  | "editor"
  | "imagegen"
  | "paths"
  | "limits"
  | "features"
  | "plugins"
  | "theme"
  | "opencode-mcp"
  | "opencode-plugins"
  | "opencode-model-state";

interface GroupDef {
  id: SettingsGroupId;
  label: string;
}

const OCODE_GROUPS: GroupDef[] = [
  { id: "model-defaults", label: "Model Defaults & Recap" },
  { id: "commit-msg", label: "Commit Message" },
  { id: "compact", label: "Compact" },
  { id: "advisor", label: "Advisor" },
  { id: "permissions", label: "Permissions" },
  { id: "security", label: "Security & Redaction" },
  { id: "terminal", label: "Terminal" },
  { id: "ocr", label: "OCR" },
  { id: "discovery", label: "Discovery" },
  { id: "tui", label: "TUI" },
  { id: "editor", label: "Editor Mode" },
  { id: "imagegen", label: "Image Generation" },
  { id: "paths", label: "Paths & Uploads" },
  { id: "limits", label: "Limits" },
  { id: "features", label: "Features" },
  { id: "plugins", label: "Plugins & Local Models" },
  { id: "theme", label: "Theme" },
];

const OPENCODE_GROUPS: GroupDef[] = [
  { id: "opencode-mcp", label: "MCP Servers" },
  { id: "opencode-plugins", label: "Legacy Plugins Key" },
  { id: "opencode-model-state", label: "Model Selection State" },
];

// Placeholder content renderer — later tasks replace each case with the
// group's real form component. Kept as a single switch (not a lookup map)
// so each task's diff is a localized one-case addition.
function renderGroup(id: SettingsGroupId) {
  switch (id) {
    default:
      return (
        <div className="text-sm text-zinc-500 p-6">
          {OCODE_GROUPS.concat(OPENCODE_GROUPS).find((g) => g.id === id)?.label} — coming soon.
        </div>
      );
  }
}

function NavSection({
  title,
  groups,
  active,
  onSelect,
}: {
  title: string;
  groups: GroupDef[];
  active: SettingsGroupId;
  onSelect: (id: SettingsGroupId) => void;
}) {
  return (
    <div className="mb-4">
      <div className="px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-zinc-500">
        {title}
      </div>
      {groups.map((g) => (
        <button
          key={g.id}
          type="button"
          onClick={() => onSelect(g.id)}
          className={`w-full text-left px-3 py-1.5 text-sm rounded-md transition-colors ${
            active === g.id
              ? "bg-zinc-700 text-white"
              : "text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
          }`}
        >
          {g.label}
        </button>
      ))}
    </div>
  );
}

export default function SettingsPanel() {
  const [active, setActive] = useState<SettingsGroupId>("model-defaults");

  return (
    <div className="flex flex-1 overflow-hidden bg-zinc-900">
      <nav className="w-64 shrink-0 overflow-y-auto border-r border-zinc-700 py-3">
        <NavSection title="ocode" groups={OCODE_GROUPS} active={active} onSelect={setActive} />
        <NavSection title="opencode" groups={OPENCODE_GROUPS} active={active} onSelect={setActive} />
      </nav>
      <div className="flex-1 overflow-y-auto">{renderGroup(active)}</div>
    </div>
  );
}
```

- [x] **Step 4: Wire `SettingsPanel` into `App.tsx`'s tab switch**

```tsx
// web/src/App.tsx — add import near the other Layout/panel imports
import SettingsPanel from "./components/Settings/SettingsPanel";
```

```tsx
              {/* Inserted alongside the existing git/cron/assets TabsContent blocks, App.tsx:341-382 */}
              <TabsContent value="settings" forceMount className="flex-1 overflow-hidden m-0">
                <SettingsPanel />
              </TabsContent>
```

- [x] **Step 5: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [x] **Step 6: Manual verification**

Use the `run` skill to launch the web dev server. Click the new "Settings" tab in the top bar; confirm the left nav shows both "ocode" and "opencode" headed groups with all 17/3 items, clicking each shows its "coming soon" placeholder, and no other tab's behavior changed.

- [x] **Step 7: Commit**

```bash
git add web/src/components/Layout/TopTabs.tsx web/src/App.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add Settings tab shell with ocode/opencode nav groups"
```

---

### Task 2: `api/client.ts` — wrapper functions for every new/extended endpoint

**Files:**
- Modify: `web/src/api/client.ts` (add to the exported `api` object, grouped near the existing config functions)

**Interfaces:**
- Produces: one `get`/`set` pair per Plan 1 endpoint (exact names below), each returning a `Promise` of the endpoint's JSON shape. Every later form task consumes these.

- [x] **Step 1: Add the wrapper functions**

```ts
  // --- New/extended OcodeConfig endpoints (Plan 1: configuration-api-backend) ---

  getRecapConfig: () =>
    fetchJSON<{ recap_model: string; recap_model_enabled: boolean; recap_timeout_seconds: number }>(
      "/api/config/ocode/recap",
    ),
  setRecapConfig: (recap_model: string, recap_model_enabled: boolean, recap_timeout_seconds: number) =>
    fetchJSON<{ recap_model: string; recap_model_enabled: boolean; recap_timeout_seconds: number }>(
      "/api/config/ocode/recap",
      { method: "PUT", body: JSON.stringify({ recap_model, recap_model_enabled, recap_timeout_seconds }) },
    ),

  getCommitMsgConfig: () =>
    fetchJSON<{ commit_msg_model: string; commit_msg_prompt: string }>("/api/config/ocode/commit-msg"),
  setCommitMsgConfig: (commit_msg_model: string, commit_msg_prompt: string) =>
    fetchJSON<{ commit_msg_model: string; commit_msg_prompt: string }>("/api/config/ocode/commit-msg", {
      method: "PUT",
      body: JSON.stringify({ commit_msg_model, commit_msg_prompt }),
    }),

  getCompactConfig: () => fetchJSON<CompactConfig>("/api/config/ocode/compact"),
  setCompactConfig: (cfg: CompactConfig) =>
    fetchJSON<CompactConfig>("/api/config/ocode/compact", { method: "PUT", body: JSON.stringify(cfg) }),

  getAdvisorFull: () =>
    fetchJSON<{ model: string; provider: string; claude_code: boolean; checkpoints: string[] }>(
      "/api/config/advisor",
    ),
  setAdvisorFull: (fields: Partial<{ model: string; provider: string; claude_code: boolean; checkpoints: string[] }>) =>
    fetchJSON<{ model: string; provider: string; claude_code: boolean; checkpoints: string[] }>(
      "/api/config/advisor",
      { method: "PUT", body: JSON.stringify(fields) },
    ),

  getAutoPermissionConfig: () => fetchJSON<AutoPermissionConfig>("/api/config/ocode/permissions-auto"),
  setAutoPermissionConfig: (cfg: AutoPermissionConfig) =>
    fetchJSON<AutoPermissionConfig>("/api/config/ocode/permissions-auto", {
      method: "PUT",
      body: JSON.stringify(cfg),
    }),

  getMaskAdvanced: () =>
    fetchJSON<{
      enabled: boolean; mode: string; model: string; base_url: string; fail_mode: string;
      allow_remote_tier2: boolean; custom_words: string[];
    }>("/api/config/mask"),
  setMaskAdvanced: (fields: { base_url: string; fail_mode: string; allow_remote_tier2: boolean; custom_words: string[] }) =>
    fetchJSON<typeof fields>("/api/config/mask/advanced", { method: "PUT", body: JSON.stringify(fields) }),

  getDiscoveryConfig: () => fetchJSON<DiscoveryConfig>("/api/config/ocode/discovery"),
  setDiscoveryConfig: (cfg: DiscoveryConfig) =>
    fetchJSON<DiscoveryConfig>("/api/config/ocode/discovery", { method: "PUT", body: JSON.stringify(cfg) }),

  getTUISettings: () => fetchJSON<TUISettings>("/api/config/ocode/tui"),
  setTUISettings: (cfg: TUISettings) =>
    fetchJSON<TUISettings>("/api/config/ocode/tui", { method: "PUT", body: JSON.stringify(cfg) }),

  getEditorConfig: () =>
    fetchJSON<{ editor: string; editor_mode: string; ide_mode: string }>("/api/config/ocode/editor"),
  setEditorConfig: (editor: string, editor_mode: string, ide_mode: string) =>
    fetchJSON<{ editor: string; editor_mode: string; ide_mode: string }>("/api/config/ocode/editor", {
      method: "PUT",
      body: JSON.stringify({ editor, editor_mode, ide_mode }),
    }),

  getImageGenConfig: () => fetchJSON<ImageGenConfig>("/api/config/ocode/imagegen"),
  setImageGenConfig: (cfg: ImageGenConfig) =>
    fetchJSON<ImageGenConfig>("/api/config/ocode/imagegen", { method: "PUT", body: JSON.stringify(cfg) }),

  getPathsConfig: () =>
    fetchJSON<{ extra_allowed_paths: string[]; upload_dir: string }>("/api/config/ocode/paths"),
  setPathsConfig: (extra_allowed_paths: string[], upload_dir: string) =>
    fetchJSON<{ extra_allowed_paths: string[]; upload_dir: string }>("/api/config/ocode/paths", {
      method: "PUT",
      body: JSON.stringify({ extra_allowed_paths, upload_dir }),
    }),

  getLimitsConfig: () =>
    fetchJSON<{ max_steps: number; max_image_dim: number; max_concurrent_agents: number; undo_max_age_delta: number }>(
      "/api/config/ocode/limits",
    ),
  setLimitsConfig: (fields: { max_steps: number; max_image_dim: number; max_concurrent_agents: number; undo_max_age_delta: number }) =>
    fetchJSON<typeof fields>("/api/config/ocode/limits", { method: "PUT", body: JSON.stringify(fields) }),

  getFeaturesConfig: () =>
    fetchJSON<{ memory_enabled: boolean; doc_prompt_enabled: boolean }>("/api/config/ocode/features"),
  setFeaturesConfig: (memory_enabled: boolean, doc_prompt_enabled: boolean) =>
    fetchJSON<{ memory_enabled: boolean; doc_prompt_enabled: boolean }>("/api/config/ocode/features", {
      method: "PUT",
      body: JSON.stringify({ memory_enabled, doc_prompt_enabled }),
    }),

  getPluginsEnabledConfig: () => fetchJSON<{ ast: boolean }>("/api/config/ocode/plugins-enabled"),
  setPluginsEnabledConfig: (ast: boolean) =>
    fetchJSON<{ ast: boolean }>("/api/config/ocode/plugins-enabled", { method: "PUT", body: JSON.stringify({ ast }) }),

  getLocalModelsConfig: () =>
    fetchJSON<Record<string, { enabled: boolean; max_parallel: number }>>("/api/config/ocode/local-models"),
  setLocalModelsConfig: (models: Record<string, { enabled: boolean; max_parallel: number }>) =>
    fetchJSON<Record<string, { enabled: boolean; max_parallel: number }>>("/api/config/ocode/local-models", {
      method: "PUT",
      body: JSON.stringify(models),
    }),
```

Add the supporting type aliases near the top of `client.ts` (alongside any existing inline `import("./types")` usages):

```ts
export interface CompactConfig {
  enabled: boolean;
  summary_provider: string;
  summary_model: string;
  token_threshold: number;
  keep_recent_turns: number;
  keep_recent_tokens: number;
  min_messages: number;
  summary_timeout_seconds: number;
  summary_max_retries: number;
  max_summary_input_tokens: number;
}

export interface AutoPermissionConfig {
  enabled?: boolean;
  allow_destructive?: boolean;
  prompt?: string;
  max_context_bytes?: number;
  max_context_sources?: number;
  max_context_lines_per_source?: number;
  min_confidence?: number;
  grants?: unknown[];
}

export interface DiscoveryConfig {
  enabled: boolean;
  embedding_model: string;
  embedding_backend: string;
  local_model_status: string;
  local_server_url: string;
  pinned_skills: string[];
  ignore_paths: string[];
}

export interface TUISettings {
  theme: string;
  mouse: boolean | null;
  scroll_speed: number;
  keybinds: Record<string, string>;
  leader_timeout: number;
  branchless: boolean;
}

export interface ImageGenConfig {
  enabled: boolean;
  provider: string;
  model: string;
  output_path?: string;
  timeout?: number;
}
```

- [x] **Step 2: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors (these are unused-but-exported functions/types at this point — `noUnusedLocals`-style errors would only fire on unused *local* variables, not exported module members, so this should pass cleanly).

- [x] **Step 3: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat(web): add API client wrappers for new configuration endpoints"
```

---

### Task 3: Model Defaults & Recap + Commit Message forms

**Files:**
- Create: `web/src/components/Settings/ModelDefaultsForm.tsx`
- Create: `web/src/components/Settings/CommitMsgForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx` (wire the two new cases into `renderGroup`)

**Interfaces:**
- Consumes: `api.getSmallModel`, `api.setSmallModel`, `api.setSmallModelEnabled` (existing), `api.getRecapConfig`/`setRecapConfig`, `api.getCommitMsgConfig`/`setCommitMsgConfig` (Task 2).

- [x] **Step 1: `ModelDefaultsForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function ModelDefaultsForm() {
  const [smallModel, setSmallModel] = useState("");
  const [smallModelEnabled, setSmallModelEnabled] = useState(false);
  const [recapModel, setRecapModel] = useState("");
  const [recapEnabled, setRecapEnabled] = useState(false);
  const [recapTimeout, setRecapTimeout] = useState(120);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [sm, recap] = await Promise.all([api.getSmallModel(), api.getRecapConfig()]);
      setSmallModel(sm.model);
      setRecapModel(recap.recap_model);
      setRecapEnabled(recap.recap_model_enabled);
      setRecapTimeout(recap.recap_timeout_seconds);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.setSmallModel(smallModel);
      await api.setSmallModelEnabled(smallModelEnabled);
      await api.setRecapConfig(recapModel, recapEnabled, recapTimeout);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Model Defaults & Recap</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}

      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Small model</label>
        <Input value={smallModel} onChange={(e) => setSmallModel(e.target.value)} className="h-8 text-xs" />
      </div>
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={smallModelEnabled} onChange={(e) => setSmallModelEnabled(e.target.checked)} />
        Small model enabled
      </label>

      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Recap model</label>
        <Input value={recapModel} onChange={(e) => setRecapModel(e.target.value)} className="h-8 text-xs" />
      </div>
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={recapEnabled} onChange={(e) => setRecapEnabled(e.target.checked)} />
        Recap model enabled
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Recap timeout (seconds)</label>
        <Input
          type="number"
          value={recapTimeout}
          onChange={(e) => setRecapTimeout(Number(e.target.value))}
          className="h-8 text-xs w-32"
        />
      </div>

      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 2: `CommitMsgForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function CommitMsgForm() {
  const [model, setModel] = useState("");
  const [prompt, setPrompt] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getCommitMsgConfig();
      setModel(cfg.commit_msg_model);
      setPrompt(cfg.commit_msg_prompt);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.setCommitMsgConfig(model, prompt);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Commit Message</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Model</label>
        <Input value={model} onChange={(e) => setModel(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Prompt</label>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          rows={4}
          className="w-full rounded-md border border-zinc-700 bg-zinc-800 px-2 py-1.5 text-xs text-zinc-200"
        />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 3: Wire into `SettingsPanel.tsx`**

```tsx
import ModelDefaultsForm from "./ModelDefaultsForm";
import CommitMsgForm from "./CommitMsgForm";
```

```tsx
function renderGroup(id: SettingsGroupId) {
  switch (id) {
    case "model-defaults":
      return <ModelDefaultsForm />;
    case "commit-msg":
      return <CommitMsgForm />;
    default:
      return ( /* unchanged */
        <div className="text-sm text-zinc-500 p-6">
          {OCODE_GROUPS.concat(OPENCODE_GROUPS).find((g) => g.id === id)?.label} — coming soon.
        </div>
      );
  }
}
```

- [x] **Step 4: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [x] **Step 5: Manual verification**

Launch the dev server via the `run` skill. Open Settings → "Model Defaults & Recap": confirm fields populate from the API, edit and Save, reload the page, confirm the edited values persisted. Repeat for "Commit Message".

- [x] **Step 6: Commit**

```bash
git add web/src/components/Settings/ModelDefaultsForm.tsx web/src/components/Settings/CommitMsgForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add Model Defaults/Recap and Commit Message settings forms"
```

---

### Task 4: Compact form

**Files:**
- Create: `web/src/components/Settings/CompactForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: `api.getCompactConfig`/`setCompactConfig` (Task 2), `CompactConfig` type.

- [x] **Step 1: `CompactForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api, type CompactConfig } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

const EMPTY: CompactConfig = {
  enabled: false, summary_provider: "", summary_model: "", token_threshold: 0,
  keep_recent_turns: 0, keep_recent_tokens: 0, min_messages: 0,
  summary_timeout_seconds: 0, summary_max_retries: 0, max_summary_input_tokens: 0,
};

const FIELDS: { key: keyof CompactConfig; label: string; type: "text" | "number" | "checkbox" }[] = [
  { key: "enabled", label: "Enabled", type: "checkbox" },
  { key: "summary_provider", label: "Summary provider", type: "text" },
  { key: "summary_model", label: "Summary model", type: "text" },
  { key: "token_threshold", label: "Token threshold (0-1)", type: "number" },
  { key: "keep_recent_turns", label: "Keep recent turns", type: "number" },
  { key: "keep_recent_tokens", label: "Keep recent tokens", type: "number" },
  { key: "min_messages", label: "Min messages", type: "number" },
  { key: "summary_timeout_seconds", label: "Summary timeout (s)", type: "number" },
  { key: "summary_max_retries", label: "Summary max retries", type: "number" },
  { key: "max_summary_input_tokens", label: "Max summary input tokens", type: "number" },
];

export default function CompactForm() {
  const [cfg, setCfg] = useState<CompactConfig>(EMPTY);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setCfg(await api.getCompactConfig());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      setCfg(await api.setCompactConfig(cfg));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Compact</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      {FIELDS.map((f) =>
        f.type === "checkbox" ? (
          <label key={f.key} className="flex items-center gap-2 text-xs text-zinc-400">
            <input
              type="checkbox"
              checked={Boolean(cfg[f.key])}
              onChange={(e) => setCfg({ ...cfg, [f.key]: e.target.checked })}
            />
            {f.label}
          </label>
        ) : (
          <div key={f.key} className="space-y-1.5">
            <label className="text-xs text-zinc-500">{f.label}</label>
            <Input
              type={f.type}
              value={String(cfg[f.key])}
              onChange={(e) =>
                setCfg({ ...cfg, [f.key]: f.type === "number" ? Number(e.target.value) : e.target.value })
              }
              className="h-8 text-xs"
            />
          </div>
        ),
      )}
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 2: Wire into `SettingsPanel.tsx`** (add `case "compact": return <CompactForm />;` following Task 3's pattern)

- [x] **Step 3: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [x] **Step 4: Manual verification**

Via the `run` skill: open Settings → "Compact", toggle `Enabled`, change `Keep recent turns`, Save, reload, confirm persisted.

- [x] **Step 5: Commit**

```bash
git add web/src/components/Settings/CompactForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add Compact settings form"
```

---

### Task 5: Advisor form (extended)

**Files:**
- Create: `web/src/components/Settings/AdvisorForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: `api.getAdvisorFull`/`setAdvisorFull` (Task 2), `api.getAdvisorEnabled`/`setAdvisorEnabled` (existing, runtime-only per spec section 1's out-of-scope callout).

- [x] **Step 1: `AdvisorForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function AdvisorForm() {
  const [model, setModel] = useState("");
  const [provider, setProvider] = useState("");
  const [claudeCode, setClaudeCode] = useState(false);
  const [checkpoints, setCheckpoints] = useState<string[]>([]);
  const [runtimeEnabled, setRuntimeEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [full, enabled] = await Promise.all([api.getAdvisorFull(), api.getAdvisorEnabled()]);
      setModel(full.model);
      setProvider(full.provider);
      setClaudeCode(full.claude_code);
      setCheckpoints(full.checkpoints ?? []);
      setRuntimeEnabled(enabled.enabled);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.setAdvisorFull({ model, provider, claude_code: claudeCode, checkpoints });
      await api.setAdvisorEnabled(runtimeEnabled);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const toggleCheckpoint = (name: string) => {
    setCheckpoints((prev) => (prev.includes(name) ? prev.filter((c) => c !== name) : [...prev, name]));
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Advisor</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}

      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={runtimeEnabled} onChange={(e) => setRuntimeEnabled(e.target.checked)} />
        Enabled for this session (not persisted)
      </label>

      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Provider</label>
        <Input value={provider} onChange={(e) => setProvider(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Model</label>
        <Input value={model} onChange={(e) => setModel(e.target.value)} className="h-8 text-xs" />
      </div>
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={claudeCode} onChange={(e) => setClaudeCode(e.target.checked)} />
        Use Claude Code CLI as advisor backend
      </label>

      <div className="space-y-1.5">
        <div className="text-xs text-zinc-500">Checkpoints</div>
        {["done", "plan"].map((cp) => (
          <label key={cp} className="flex items-center gap-2 text-xs text-zinc-400">
            <input type="checkbox" checked={checkpoints.includes(cp)} onChange={() => toggleCheckpoint(cp)} />
            {cp}
          </label>
        ))}
      </div>

      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 2: Wire into `SettingsPanel.tsx`** (`case "advisor": return <AdvisorForm />;`)

- [x] **Step 3: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors. If `api.getAdvisorEnabled`/`setAdvisorEnabled` don't exist under those exact names, run `grep -n "advisor.enabled\|AdvisorEnabled" web/src/api/client.ts` to find the actual names and use those instead.

- [x] **Step 4: Manual verification**

Via the `run` skill: open Settings → "Advisor", toggle Claude Code CLI and a checkpoint, Save, reload, confirm persisted; confirm the runtime Enabled toggle still behaves like the existing sidebar advisor toggle did (session-only).

- [x] **Step 5: Commit**

```bash
git add web/src/components/Settings/AdvisorForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add Advisor settings form"
```

---

### Task 6: Permissions form

**Files:**
- Create: `web/src/components/Settings/PermissionsForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: `api.getAutoPermissionConfig`/`setAutoPermissionConfig` (Task 2), existing yolo-mode endpoints (`grep -n "yolo" web/src/api/client.ts` to find exact existing function names before writing).

- [x] **Step 1: Discover the existing yolo/mode API functions**

Run: `grep -n "yolo\|Permission" web/src/api/client.ts` and read the matches. Use whatever `get`/`set` function names already exist for permission mode (likely `getYolo`/`setYolo` or similarly named) in place of the placeholder names below if they differ.

- [x] **Step 2: `PermissionsForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api, type AutoPermissionConfig } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

const EMPTY_AUTO: AutoPermissionConfig = {
  enabled: false, allow_destructive: false, prompt: "",
  max_context_bytes: 0, max_context_sources: 0, max_context_lines_per_source: 0, min_confidence: 0,
};

export default function PermissionsForm() {
  const [mode, setMode] = useState(false); // yolo on/off — see api.getYolo/setYolo, confirm exact shape in Step 1 of this task
  const [auto, setAuto] = useState<AutoPermissionConfig>(EMPTY_AUTO);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [yolo, autoCfg] = await Promise.all([api.getYolo(), api.getAutoPermissionConfig()]);
      setMode(yolo.enabled);
      setAuto({ ...EMPTY_AUTO, ...autoCfg });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.setYolo(mode);
      await api.setAutoPermissionConfig(auto);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Permissions</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}

      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={mode} onChange={(e) => setMode(e.target.checked)} />
        Yolo mode (auto-approve all tool calls)
      </label>

      <div className="border-t border-zinc-700 pt-4 space-y-4">
        <div className="text-xs font-semibold text-zinc-300">Auto-approval (LLM-assisted)</div>
        <label className="flex items-center gap-2 text-xs text-zinc-400">
          <input
            type="checkbox"
            checked={Boolean(auto.enabled)}
            onChange={(e) => setAuto({ ...auto, enabled: e.target.checked })}
          />
          Enabled
        </label>
        <label className="flex items-center gap-2 text-xs text-zinc-400">
          <input
            type="checkbox"
            checked={Boolean(auto.allow_destructive)}
            onChange={(e) => setAuto({ ...auto, allow_destructive: e.target.checked })}
          />
          Allow destructive actions
        </label>
        <div className="space-y-1.5">
          <label className="text-xs text-zinc-500">Prompt</label>
          <textarea
            value={auto.prompt ?? ""}
            onChange={(e) => setAuto({ ...auto, prompt: e.target.value })}
            rows={3}
            className="w-full rounded-md border border-zinc-700 bg-zinc-800 px-2 py-1.5 text-xs text-zinc-200"
          />
        </div>
        <div className="grid grid-cols-3 gap-2">
          <div className="space-y-1.5">
            <label className="text-xs text-zinc-500">Max context bytes</label>
            <Input
              type="number"
              value={auto.max_context_bytes ?? 0}
              onChange={(e) => setAuto({ ...auto, max_context_bytes: Number(e.target.value) })}
              className="h-8 text-xs"
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-xs text-zinc-500">Max sources</label>
            <Input
              type="number"
              value={auto.max_context_sources ?? 0}
              onChange={(e) => setAuto({ ...auto, max_context_sources: Number(e.target.value) })}
              className="h-8 text-xs"
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-xs text-zinc-500">Max lines/source</label>
            <Input
              type="number"
              value={auto.max_context_lines_per_source ?? 0}
              onChange={(e) => setAuto({ ...auto, max_context_lines_per_source: Number(e.target.value) })}
              className="h-8 text-xs"
            />
          </div>
        </div>
      </div>

      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 3: Wire into `SettingsPanel.tsx`** (`case "permissions": return <PermissionsForm />;`)

- [x] **Step 4: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors once `api.getYolo`/`api.setYolo` (or the actual names found in Step 1) are correctly referenced.

- [x] **Step 5: Manual verification**

Via the `run` skill: open Settings → "Permissions", toggle Yolo mode and auto-approval Enabled, Save, reload, confirm persisted.

- [x] **Step 6: Commit**

```bash
git add web/src/components/Settings/PermissionsForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add Permissions settings form"
```

---

### Task 7: Security & Redaction form (extended)

**Files:**
- Create: `web/src/components/Settings/SecurityForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: `api.getMaskAdvanced`, `api.setMaskEnabled`/`setMaskMode`/`setMaskModel` (existing), `api.setMaskAdvanced` (Task 2).

- [x] **Step 1: `SecurityForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function SecurityForm() {
  const [enabled, setEnabled] = useState(false);
  const [mode, setMode] = useState("lenient");
  const [model, setModel] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [failMode, setFailMode] = useState("warn");
  const [allowRemoteTier2, setAllowRemoteTier2] = useState(false);
  const [customWords, setCustomWords] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getMaskAdvanced();
      setEnabled(cfg.enabled);
      setMode(cfg.mode);
      setModel(cfg.model);
      setBaseUrl(cfg.base_url);
      setFailMode(cfg.fail_mode);
      setAllowRemoteTier2(cfg.allow_remote_tier2);
      setCustomWords((cfg.custom_words ?? []).join(", "));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.setMaskEnabled(enabled);
      await api.setMaskMode(mode);
      await api.setMaskModel(model);
      await api.setMaskAdvanced({
        base_url: baseUrl,
        fail_mode: failMode,
        allow_remote_tier2: allowRemoteTier2,
        custom_words: customWords.split(",").map((w) => w.trim()).filter(Boolean),
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Security & Redaction</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}

      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        Enabled
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Mode (lenient / full)</label>
        <Input value={mode} onChange={(e) => setMode(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Model</label>
        <Input value={model} onChange={(e) => setModel(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Base URL</label>
        <Input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Fail mode (block / warn)</label>
        <Input value={failMode} onChange={(e) => setFailMode(e.target.value)} className="h-8 text-xs" />
      </div>
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={allowRemoteTier2} onChange={(e) => setAllowRemoteTier2(e.target.checked)} />
        Allow remote tier-2 scanner endpoints
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Custom words (comma-separated)</label>
        <Input value={customWords} onChange={(e) => setCustomWords(e.target.value)} className="h-8 text-xs" />
      </div>

      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 2: Wire into `SettingsPanel.tsx`** (`case "security": return <SecurityForm />;`)

- [x] **Step 3: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [x] **Step 4: Manual verification**

Via the `run` skill: open Settings → "Security & Redaction", edit Base URL and Custom words, Save, reload, confirm persisted and that Enabled/Mode/Model (from the pre-existing endpoints) still round-trip correctly.

- [x] **Step 5: Commit**

```bash
git add web/src/components/Settings/SecurityForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): extend Security/Redaction settings form with advanced fields"
```

---

### Task 8: Terminal + OCR forms

**Files:**
- Create: `web/src/components/Settings/TerminalForm.tsx`
- Create: `web/src/components/Settings/OcrForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: `api.getTerminalConfig`/`setTerminalScrollbackLines` (existing), `api.getOcrConfig`/`setOcrConfig` (existing — confirm exact names via `grep -n "Ocr" web/src/api/client.ts` before writing).

- [x] **Step 1: `TerminalForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function TerminalForm() {
  const [scrollback, setScrollback] = useState(0);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getTerminalConfig();
      setScrollback(cfg.scrollback_lines);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.setTerminalScrollbackLines(scrollback);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Terminal</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Scrollback lines</label>
        <Input
          type="number"
          value={scrollback}
          onChange={(e) => setScrollback(Number(e.target.value))}
          className="h-8 text-xs w-32"
        />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 2: Discover the OCR API shape, then write `OcrForm.tsx`**

Run: `grep -n "Ocr" web/src/api/client.ts` and read the `getOcrConfig`/`setOcrConfig` (or equivalently named) function signatures and the `OcrConfig` type it imports from `./types`.

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import type { OcrConfig } from "../../api/types";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function OcrForm() {
  const [cfg, setCfg] = useState<OcrConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setCfg(await api.getOcrConfig());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    if (!cfg) return;
    setSaving(true);
    setError(null);
    try {
      await api.setOcrConfig(cfg);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading || !cfg) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">OCR</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input
          type="checkbox"
          checked={Boolean(cfg.enabled)}
          onChange={(e) => setCfg({ ...cfg, enabled: e.target.checked })}
        />
        Enabled
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Backend</label>
        <Input
          value={String(cfg.backend ?? "")}
          onChange={(e) => setCfg({ ...cfg, backend: e.target.value })}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Model</label>
        <Input
          value={String(cfg.model ?? "")}
          onChange={(e) => setCfg({ ...cfg, model: e.target.value })}
          className="h-8 text-xs"
        />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

(If Step 2's grep finds `OcrConfig` has additional required fields, e.g. an endpoint URL, add matching `Input` rows following the same `space-y-1.5` block pattern above.)

- [x] **Step 3: Wire both into `SettingsPanel.tsx`** (`case "terminal": return <TerminalForm />;`, `case "ocr": return <OcrForm />;`)

- [x] **Step 4: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [x] **Step 5: Manual verification**

Via the `run` skill: open Settings → "Terminal", change scrollback lines, Save, reload, confirm persisted. Open Settings → "OCR", toggle Enabled, Save, reload, confirm persisted.

- [x] **Step 6: Commit**

```bash
git add web/src/components/Settings/TerminalForm.tsx web/src/components/Settings/OcrForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add Terminal and OCR settings forms"
```

---

### Task 9: Discovery form

**Files:**
- Create: `web/src/components/Settings/DiscoveryForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: `api.getDiscoveryConfig`/`setDiscoveryConfig` (Task 2), `DiscoveryConfig` type.

- [x] **Step 1: `DiscoveryForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api, type DiscoveryConfig } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

const EMPTY: DiscoveryConfig = {
  enabled: false, embedding_model: "", embedding_backend: "http", local_model_status: "none",
  local_server_url: "", pinned_skills: [], ignore_paths: [],
};

export default function DiscoveryForm() {
  const [cfg, setCfg] = useState<DiscoveryConfig>(EMPTY);
  const [pinnedText, setPinnedText] = useState("");
  const [ignoreText, setIgnoreText] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const c = await api.getDiscoveryConfig();
      setCfg(c);
      setPinnedText((c.pinned_skills ?? []).join(", "));
      setIgnoreText((c.ignore_paths ?? []).join(", "));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const next: DiscoveryConfig = {
        ...cfg,
        pinned_skills: pinnedText.split(",").map((s) => s.trim()).filter(Boolean),
        ignore_paths: ignoreText.split(",").map((s) => s.trim()).filter(Boolean),
      };
      setCfg(await api.setDiscoveryConfig(next));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Discovery</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={cfg.enabled} onChange={(e) => setCfg({ ...cfg, enabled: e.target.checked })} />
        Enabled
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Embedding model</label>
        <Input
          value={cfg.embedding_model}
          onChange={(e) => setCfg({ ...cfg, embedding_model: e.target.value })}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Embedding backend (http / local)</label>
        <Input
          value={cfg.embedding_backend}
          onChange={(e) => setCfg({ ...cfg, embedding_backend: e.target.value })}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Local server URL</label>
        <Input
          value={cfg.local_server_url}
          onChange={(e) => setCfg({ ...cfg, local_server_url: e.target.value })}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Pinned skills (comma-separated)</label>
        <Input value={pinnedText} onChange={(e) => setPinnedText(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Ignore paths (comma-separated)</label>
        <Input value={ignoreText} onChange={(e) => setIgnoreText(e.target.value)} className="h-8 text-xs" />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 2: Wire into `SettingsPanel.tsx`** (`case "discovery": return <DiscoveryForm />;`)

- [x] **Step 3: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [x] **Step 4: Manual verification**

Via the `run` skill: open Settings → "Discovery", toggle Enabled, edit Pinned skills, Save, reload, confirm persisted.

- [x] **Step 5: Commit**

```bash
git add web/src/components/Settings/DiscoveryForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add Discovery settings form"
```

---

### Task 10: TUI form

**Files:**
- Create: `web/src/components/Settings/TUIForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: `api.getTUISettings`/`setTUISettings` (Task 2), `TUISettings` type.

- [x] **Step 1: `TUIForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api, type TUISettings } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

const EMPTY: TUISettings = { theme: "", mouse: null, scroll_speed: 0, keybinds: {}, leader_timeout: 0, branchless: false };

export default function TUIForm() {
  const [cfg, setCfg] = useState<TUISettings>(EMPTY);
  const [keybindsText, setKeybindsText] = useState("{}");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const c = await api.getTUISettings();
      setCfg(c);
      setKeybindsText(JSON.stringify(c.keybinds ?? {}, null, 2));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      let keybinds: Record<string, string> = {};
      try {
        keybinds = JSON.parse(keybindsText);
      } catch {
        setError("Keybinds must be valid JSON");
        setSaving(false);
        return;
      }
      setCfg(await api.setTUISettings({ ...cfg, keybinds }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">TUI</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Theme</label>
        <Input value={cfg.theme} onChange={(e) => setCfg({ ...cfg, theme: e.target.value })} className="h-8 text-xs" />
      </div>
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input
          type="checkbox"
          checked={Boolean(cfg.mouse)}
          onChange={(e) => setCfg({ ...cfg, mouse: e.target.checked })}
        />
        Mouse support
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Scroll speed</label>
        <Input
          type="number"
          value={cfg.scroll_speed}
          onChange={(e) => setCfg({ ...cfg, scroll_speed: Number(e.target.value) })}
          className="h-8 text-xs w-32"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Leader key timeout (ms)</label>
        <Input
          type="number"
          value={cfg.leader_timeout}
          onChange={(e) => setCfg({ ...cfg, leader_timeout: Number(e.target.value) })}
          className="h-8 text-xs w-32"
        />
      </div>
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input
          type="checkbox"
          checked={cfg.branchless}
          onChange={(e) => setCfg({ ...cfg, branchless: e.target.checked })}
        />
        Branchless mode
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Keybinds (JSON)</label>
        <textarea
          value={keybindsText}
          onChange={(e) => setKeybindsText(e.target.value)}
          rows={6}
          className="w-full rounded-md border border-zinc-700 bg-zinc-800 px-2 py-1.5 text-xs text-zinc-200 font-mono"
        />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 2: Wire into `SettingsPanel.tsx`** (`case "tui": return <TUIForm />;`)

- [x] **Step 3: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [x] **Step 4: Manual verification**

Via the `run` skill: open Settings → "TUI", change Theme and Scroll speed, edit Keybinds JSON, Save, reload, confirm persisted; confirm an invalid Keybinds JSON shows the inline error and does not save.

- [x] **Step 5: Commit**

```bash
git add web/src/components/Settings/TUIForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add TUI settings form"
```

---

### Task 11: Editor Mode, Paths & Uploads, Features forms

**Files:**
- Create: `web/src/components/Settings/EditorModeForm.tsx`
- Create: `web/src/components/Settings/PathsForm.tsx`
- Create: `web/src/components/Settings/FeaturesForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: `api.getEditorConfig`/`setEditorConfig`, `api.getPathsConfig`/`setPathsConfig`, `api.getFeaturesConfig`/`setFeaturesConfig` (all Task 2).

- [x] **Step 1: `EditorModeForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function EditorModeForm() {
  const [editor, setEditor] = useState("");
  const [editorMode, setEditorMode] = useState("");
  const [ideMode, setIdeMode] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getEditorConfig();
      setEditor(cfg.editor);
      setEditorMode(cfg.editor_mode);
      setIdeMode(cfg.ide_mode);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.setEditorConfig(editor, editorMode, ideMode);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Editor Mode</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Editor</label>
        <Input value={editor} onChange={(e) => setEditor(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Editor mode</label>
        <Input value={editorMode} onChange={(e) => setEditorMode(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">IDE mode</label>
        <Input value={ideMode} onChange={(e) => setIdeMode(e.target.value)} className="h-8 text-xs" />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 2: `PathsForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function PathsForm() {
  const [pathsText, setPathsText] = useState("");
  const [uploadDir, setUploadDir] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getPathsConfig();
      setPathsText((cfg.extra_allowed_paths ?? []).join(", "));
      setUploadDir(cfg.upload_dir);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const paths = pathsText.split(",").map((p) => p.trim()).filter(Boolean);
      await api.setPathsConfig(paths, uploadDir);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Paths & Uploads</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Extra allowed paths (comma-separated)</label>
        <Input value={pathsText} onChange={(e) => setPathsText(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Upload directory</label>
        <Input value={uploadDir} onChange={(e) => setUploadDir(e.target.value)} className="h-8 text-xs" />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 3: `FeaturesForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Loader2 } from "lucide-react";

export default function FeaturesForm() {
  const [memoryEnabled, setMemoryEnabled] = useState(false);
  const [docPromptEnabled, setDocPromptEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getFeaturesConfig();
      setMemoryEnabled(cfg.memory_enabled);
      setDocPromptEnabled(cfg.doc_prompt_enabled);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.setFeaturesConfig(memoryEnabled, docPromptEnabled);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Features</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={memoryEnabled} onChange={(e) => setMemoryEnabled(e.target.checked)} />
        Memory injection enabled
      </label>
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={docPromptEnabled} onChange={(e) => setDocPromptEnabled(e.target.checked)} />
        Documentation-first prompt enabled
      </label>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 4: Wire all three into `SettingsPanel.tsx`** (`case "editor": return <EditorModeForm />;`, `case "paths": return <PathsForm />;`, `case "features": return <FeaturesForm />;`)

- [x] **Step 5: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [x] **Step 6: Manual verification**

Via the `run` skill: exercise each of the three group tabs — edit a field, Save, reload, confirm persisted, for all three.

- [x] **Step 7: Commit**

```bash
git add web/src/components/Settings/EditorModeForm.tsx web/src/components/Settings/PathsForm.tsx web/src/components/Settings/FeaturesForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add Editor Mode, Paths & Uploads, and Features settings forms"
```

---

### Task 12: Limits form

**Files:**
- Create: `web/src/components/Settings/LimitsForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: `api.getLimitsConfig`/`setLimitsConfig` (Task 2).

- [x] **Step 1: `LimitsForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function LimitsForm() {
  const [maxSteps, setMaxSteps] = useState(0);
  const [maxImageDim, setMaxImageDim] = useState(0);
  const [maxConcurrentAgents, setMaxConcurrentAgents] = useState(0);
  const [undoMaxAgeDelta, setUndoMaxAgeDelta] = useState(0);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getLimitsConfig();
      setMaxSteps(cfg.max_steps);
      setMaxImageDim(cfg.max_image_dim);
      setMaxConcurrentAgents(cfg.max_concurrent_agents);
      setUndoMaxAgeDelta(cfg.undo_max_age_delta);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.setLimitsConfig({
        max_steps: maxSteps, max_image_dim: maxImageDim,
        max_concurrent_agents: maxConcurrentAgents, undo_max_age_delta: undoMaxAgeDelta,
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Limits</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Max steps (0 = default cap of 100)</label>
        <Input type="number" value={maxSteps} onChange={(e) => setMaxSteps(Number(e.target.value))} className="h-8 text-xs w-32" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Max image dimension (px, 0 = default 2000)</label>
        <Input type="number" value={maxImageDim} onChange={(e) => setMaxImageDim(Number(e.target.value))} className="h-8 text-xs w-32" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Max concurrent agents (0 = unlimited)</label>
        <Input
          type="number"
          value={maxConcurrentAgents}
          onChange={(e) => setMaxConcurrentAgents(Number(e.target.value))}
          className="h-8 text-xs w-32"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Undo max age delta (default 4)</label>
        <Input
          type="number"
          value={undoMaxAgeDelta}
          onChange={(e) => setUndoMaxAgeDelta(Number(e.target.value))}
          className="h-8 text-xs w-32"
        />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 2: Wire into `SettingsPanel.tsx`** (`case "limits": return <LimitsForm />;`)

- [x] **Step 3: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [x] **Step 4: Manual verification**

Via the `run` skill: open Settings → "Limits", change Max concurrent agents, Save, reload, confirm persisted.

- [x] **Step 5: Commit**

```bash
git add web/src/components/Settings/LimitsForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add Limits settings form"
```

---

### Task 13: Image Generation form

**Files:**
- Create: `web/src/components/Settings/ImageGenForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: `api.getImageGenConfig`/`setImageGenConfig` (Task 2), `ImageGenConfig` type.

- [x] **Step 1: `ImageGenForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api, type ImageGenConfig } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

const EMPTY: ImageGenConfig = { enabled: false, provider: "gemini", model: "", output_path: "", timeout: 0 };

export default function ImageGenForm() {
  const [cfg, setCfg] = useState<ImageGenConfig>(EMPTY);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setCfg(await api.getImageGenConfig());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      setCfg(await api.setImageGenConfig(cfg));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Image Generation</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={cfg.enabled} onChange={(e) => setCfg({ ...cfg, enabled: e.target.checked })} />
        Enabled
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Provider (gemini / openai / novita / deepinfra)</label>
        <Input value={cfg.provider} onChange={(e) => setCfg({ ...cfg, provider: e.target.value })} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Model (blank = provider default)</label>
        <Input value={cfg.model} onChange={(e) => setCfg({ ...cfg, model: e.target.value })} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Output path (blank = working directory)</label>
        <Input value={cfg.output_path ?? ""} onChange={(e) => setCfg({ ...cfg, output_path: e.target.value })} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Timeout (s, 0 = default)</label>
        <Input
          type="number"
          value={cfg.timeout ?? 0}
          onChange={(e) => setCfg({ ...cfg, timeout: Number(e.target.value) })}
          className="h-8 text-xs w-32"
        />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
```

- [x] **Step 2: Wire into `SettingsPanel.tsx`** (`case "imagegen": return <ImageGenForm />;`)

- [x] **Step 3: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [x] **Step 4: Manual verification**

Via the `run` skill: open Settings → "Image Generation", toggle Enabled, change Provider, Save, reload, confirm persisted.

- [x] **Step 5: Commit**

```bash
git add web/src/components/Settings/ImageGenForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add Image Generation settings form"
```

---

### Task 14: Plugins & Local Models form (ocode)

**Files:**
- Create: `web/src/components/Settings/OcodePluginsForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: `api.getPluginsEnabledConfig`/`setPluginsEnabledConfig`, `api.getLocalModelsConfig`/`setLocalModelsConfig` (Task 2), `api.listPlugins`/`setPluginEnabled`/`installPlugin`/`removePlugin` (existing, same functions `PluginsPanel.tsx` already uses for `ExternalPlugins`).

- [x] **Step 1: `OcodePluginsForm.tsx`**

Reuses the install/list/toggle/remove logic already proven in `PluginsPanel.tsx` for the `ExternalPlugins` list, plus new sections for the `Plugins.AST` toggle and `LocalModels` map:

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import type { PluginInfo } from "../../api/types";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2, Trash2, Download } from "lucide-react";

export default function OcodePluginsForm() {
  const [astEnabled, setAstEnabled] = useState(false);
  const [plugins, setPlugins] = useState<PluginInfo[]>([]);
  const [source, setSource] = useState("");
  const [installing, setInstalling] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [localModels, setLocalModels] = useState<Record<string, { enabled: boolean; max_parallel: number }>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [pluginsEnabled, list, models] = await Promise.all([
        api.getPluginsEnabledConfig(),
        api.listPlugins(),
        api.getLocalModelsConfig(),
      ]);
      setAstEnabled(pluginsEnabled.ast);
      setPlugins(list.slice().sort((a, b) => a.name.localeCompare(b.name)));
      setLocalModels(models);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const saveAst = async (next: boolean) => {
    setSaving(true);
    setError(null);
    try {
      await api.setPluginsEnabledConfig(next);
      setAstEnabled(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const toggleExternal = async (p: PluginInfo) => {
    setBusy(p.name);
    setError(null);
    try {
      await api.setPluginEnabled(p.name, !p.enabled);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  const removeExternal = async (p: PluginInfo) => {
    setBusy(p.name);
    setError(null);
    try {
      await api.removePlugin(p.name);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  const install = async () => {
    const src = source.trim();
    if (!src) return;
    setInstalling(true);
    setError(null);
    try {
      await api.installPlugin(src);
      setSource("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setInstalling(false);
    }
  };

  const toggleLocalModel = async (name: string) => {
    const next = { ...localModels, [name]: { ...localModels[name], enabled: !localModels[name].enabled } };
    setLocalModels(next);
    setSaving(true);
    setError(null);
    try {
      await api.setLocalModelsConfig(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-6">
      <div>
        <h2 className="text-sm font-semibold text-zinc-200 mb-2">Plugins & Local Models</h2>
        {error && <div className="text-xs text-red-400 mb-2">{error}</div>}
        <label className="flex items-center gap-2 text-xs text-zinc-400">
          <input
            type="checkbox"
            checked={astEnabled}
            disabled={saving}
            onChange={(e) => saveAst(e.target.checked)}
          />
          AST structural search/rewrite tool (ast_grep) enabled
        </label>
      </div>

      <div className="border-t border-zinc-700 pt-4">
        <div className="text-xs font-semibold text-zinc-300 mb-2">External plugins</div>
        <div className="flex items-center gap-2 mb-3">
          <Input
            value={source}
            onChange={(e) => setSource(e.target.value)}
            placeholder="name, git URL, or owner/repo@ref"
            className="h-8 text-xs"
            onKeyDown={(e) => {
              if (e.key === "Enter") install();
            }}
          />
          <Button size="sm" className="h-8 gap-1.5 text-xs shrink-0" onClick={install} disabled={installing || !source.trim()}>
            {installing ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Download className="w-3.5 h-3.5" />}
            Install
          </Button>
        </div>
        {plugins.length === 0 ? (
          <div className="text-xs text-zinc-500">No plugins installed.</div>
        ) : (
          <div className="space-y-1">
            {plugins.map((p) => (
              <div key={p.name} className="flex items-center justify-between gap-2 py-2 px-3 rounded-md hover:bg-zinc-800">
                <div className="min-w-0 text-sm text-zinc-300 truncate">{p.name}</div>
                <div className="flex items-center gap-1.5 shrink-0">
                  <Button
                    variant={p.enabled ? "default" : "outline"}
                    size="sm"
                    className="h-7 text-xs min-w-[56px]"
                    onClick={() => toggleExternal(p)}
                    disabled={busy === p.name}
                  >
                    {busy === p.name ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : p.enabled ? "On" : "Off"}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 w-7 p-0 text-zinc-500 hover:text-red-400"
                    onClick={() => removeExternal(p)}
                    disabled={busy === p.name}
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="border-t border-zinc-700 pt-4">
        <div className="text-xs font-semibold text-zinc-300 mb-2">Local models</div>
        {Object.keys(localModels).length === 0 ? (
          <div className="text-xs text-zinc-500">No local models registered.</div>
        ) : (
          <div className="space-y-1">
            {Object.entries(localModels).map(([name, m]) => (
              <div key={name} className="flex items-center justify-between gap-2 py-2 px-3 rounded-md hover:bg-zinc-800">
                <div className="min-w-0 text-sm text-zinc-300 truncate font-mono">{name}</div>
                <Button
                  variant={m.enabled ? "default" : "outline"}
                  size="sm"
                  className="h-7 text-xs min-w-[56px]"
                  onClick={() => toggleLocalModel(name)}
                >
                  {m.enabled ? "On" : "Off"}
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
```

- [x] **Step 2: Wire into `SettingsPanel.tsx`** (`case "plugins": return <OcodePluginsForm />;`)

- [x] **Step 3: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [x] **Step 4: Manual verification**

Via the `run` skill: open Settings → "Plugins & Local Models", toggle AST enabled, install/enable/remove a plugin (mirrors `PluginsPanel.tsx` behavior), toggle a local model if any are registered.

- [x] **Step 5: Commit**

```bash
git add web/src/components/Settings/OcodePluginsForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add Plugins & Local Models settings form"
```

---

### Task 15: Theme form (relocated from sidebar)

**Files:**
- Create: `web/src/components/Settings/ThemeForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: `api.getThemes` (existing, used by `CoworkSidebar.tsx`'s current Theme section), plus whichever `api.setTheme`/`applyTheme`-backing call `CoworkSidebar.tsx` currently uses (`grep -n "applyTheme\|getThemes\|setTheme" web/src/components/Layout/CoworkSidebar.tsx web/src/api/client.ts` to find the exact function before writing).

- [x] **Step 1: Discover the existing theme API**

Run: `grep -n "applyTheme\|getThemes\|setTheme\|currentTheme" web/src/components/Layout/CoworkSidebar.tsx web/src/api/client.ts` and read the matches to learn the exact `api` function name(s) and how `currentTheme` is currently derived/persisted (local state, an API call, or `localStorage`).

- [x] **Step 2: `ThemeForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Loader2 } from "lucide-react";

interface ThemeOption {
  name: string;
  label: string;
}

export default function ThemeForm() {
  const [themes, setThemes] = useState<ThemeOption[]>([]);
  const [currentTheme, setCurrentTheme] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const list = await api.getThemes();
      setThemes(list);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const apply = async (name: string) => {
    setError(null);
    try {
      await api.setTheme(name); // confirm exact name in Step 1 of this task; adjust if different
      setCurrentTheme(name);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Theme</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      {themes.length === 0 ? (
        <div className="text-xs text-zinc-500">No themes available.</div>
      ) : (
        <div className="grid grid-cols-3 gap-1.5">
          {themes.map((t) => (
            <button
              key={t.name}
              type="button"
              onClick={() => apply(t.name)}
              className={`text-xs rounded px-2 py-1.5 truncate transition-colors ${
                currentTheme === t.name
                  ? "bg-emerald-600/30 text-emerald-300 border border-emerald-600/50"
                  : "bg-zinc-800 text-zinc-400 hover:bg-zinc-700"
              }`}
              title={t.name}
            >
              {t.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
```

- [x] **Step 3: Wire into `SettingsPanel.tsx`** (`case "theme": return <ThemeForm />;`)

- [x] **Step 4: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors once the exact theme-apply function name from Step 1 is used.

- [x] **Step 5: Manual verification**

Via the `run` skill: open Settings → "Theme", click a different theme, confirm the UI restyles the same way clicking it in the old sidebar section used to.

- [x] **Step 6: Commit**

```bash
git add web/src/components/Settings/ThemeForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add Theme settings form"
```

---

### Task 16: opencode group form (MCP Servers, legacy Plugins, Model Selection State)

**Files:**
- Create: `web/src/components/Settings/OpencodeMcpForm.tsx`
- Create: `web/src/components/Settings/OpencodeReadOnlyForm.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx`

**Interfaces:**
- Consumes: the MCP server list/toggle API currently used by `CoworkSidebar.tsx`'s Tools/MCP section (`mcpServers`, `toggleMcp` — `grep -n "mcpServers\|toggleMcp\|getMcp\|setMcp" web/src/components/Layout/CoworkSidebar.tsx web/src/api/client.ts` to find exact names before writing).

- [x] **Step 1: Discover the existing MCP API**

Run: `grep -n "mcpServers\|toggleMcp\|getMcp\|setMcp\|MCPStatus" web/src/components/Layout/CoworkSidebar.tsx web/src/api/client.ts` and read the matches for the exact loader function and toggle function names/signatures.

- [x] **Step 2: `OpencodeMcpForm.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import type { MCPStatus } from "../../api/types";
import { Loader2 } from "lucide-react";

export default function OpencodeMcpForm() {
  const [servers, setServers] = useState<MCPStatus[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setServers(await api.getMcpServers()); // confirm exact name in Step 1 of this task
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const toggle = async (m: MCPStatus) => {
    setBusy(m.name);
    setError(null);
    try {
      await api.setMcpEnabled(m.name, !m.enabled); // confirm exact name in Step 1 of this task
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">MCP Servers</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      {servers.length === 0 ? (
        <div className="text-xs text-zinc-500">No MCP servers configured.</div>
      ) : (
        <div className="space-y-1">
          {servers.map((m) => (
            <div key={m.name} className="flex items-center justify-between gap-2 py-2 px-3 rounded-md hover:bg-zinc-800">
              <span className="truncate font-mono text-sm text-zinc-300">{m.name}</span>
              <button
                type="button"
                onClick={() => toggle(m)}
                disabled={busy === m.name}
                className="flex items-center gap-2 disabled:opacity-50"
              >
                <span className={`text-xs ${m.enabled ? "text-emerald-400" : "text-zinc-500"}`}>
                  {m.enabled ? "on" : "off"}
                </span>
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
```

- [x] **Step 3: `OpencodeReadOnlyForm.tsx`** — shared component for the two read-only opencode items

```tsx
interface Props {
  title: string;
  note: string;
  data: unknown;
}

export default function OpencodeReadOnlyForm({ title, note, data }: Props) {
  return (
    <div className="p-6 max-w-lg space-y-3">
      <h2 className="text-sm font-semibold text-zinc-200">{title}</h2>
      <div className="text-xs text-zinc-500">{note}</div>
      <pre className="rounded-md border border-zinc-700 bg-zinc-800 p-3 text-xs text-zinc-300 overflow-x-auto">
        {JSON.stringify(data, null, 2)}
      </pre>
    </div>
  );
}
```

Since there is no existing endpoint that surfaces `opencode.json`'s legacy `plugins` key or `opencode/model.json` to the frontend, these two nav items render a static explanatory message instead of live data for now (matches the spec's read-only, non-persisted framing — no backend work was scoped for them in Plan 1):

```tsx
// Inline usage in SettingsPanel.tsx's renderGroup, added in Step 4 below —
// no new file needed for the two read-only cases themselves.
```

- [x] **Step 4: Wire all three into `SettingsPanel.tsx`**

```tsx
import OpencodeMcpForm from "./OpencodeMcpForm";
import OpencodeReadOnlyForm from "./OpencodeReadOnlyForm";
```

```tsx
    case "opencode-mcp":
      return <OpencodeMcpForm />;
    case "opencode-plugins":
      return (
        <OpencodeReadOnlyForm
          title="Legacy Plugins Key"
          note="opencode.json's legacy plugins key is read by ocode for migration only and is never written back — this value is informational."
          data={{ note: "not yet exposed via API — see docs/superpowers/specs/2026-08-11-configuration-ui-design.md section 7" }}
        />
      );
    case "opencode-model-state":
      return (
        <OpencodeReadOnlyForm
          title="Model Selection State"
          note="opencode/model.json (recent/favorite model selections) is owned exclusively by opencode — ocode only reads it as a fallback."
          data={{ note: "not yet exposed via API — see docs/superpowers/specs/2026-08-11-configuration-ui-design.md section 7" }}
        />
      );
```

- [x] **Step 5: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors once the exact MCP API names from Step 1 are used.

- [x] **Step 6: Manual verification**

Via the `run` skill: open Settings → "MCP Servers", toggle a server on/off, confirm it matches the old sidebar Tools/MCP section's behavior. Open "Legacy Plugins Key" and "Model Selection State", confirm they render the explanatory placeholder without erroring.

- [x] **Step 7: Commit**

```bash
git add web/src/components/Settings/OpencodeMcpForm.tsx web/src/components/Settings/OpencodeReadOnlyForm.tsx web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): add opencode MCP Servers form and read-only legacy sections"
```

---

### Task 17: Remove Models/Theme/Tools/Paths from `CoworkSidebar.tsx`, preserve the main-model picker trigger

**Files:**
- Modify: `web/src/components/Layout/CoworkSidebar.tsx`

**Interfaces:**
- Consumes: nothing new — this task only removes JSX/state that has been superseded by Tasks 3–16's Settings forms, while preserving `onModelClick?.("main")` as the trigger for `ModelDialog.tsx` (unchanged, out of scope per the design spec).

- [x] **Step 1: Update `DEFAULT_SECTIONS`**

```tsx
// web/src/components/Layout/CoworkSidebar.tsx — replace the existing DEFAULT_SECTIONS
const DEFAULT_SECTIONS: Record<string, boolean> = {
  agent: true,
  context: true,
  lsp: false,
  files: false,
  todo: false,
  git: true,
};
```

Bump the persistence key so removed section keys don't linger in old users' `localStorage` (harmless either way, but keeps the stored shape clean):

```tsx
const SIDEBAR_SECTIONS_KEY = "ocode.ui.sidebar.v2";
```

- [x] **Step 2: Replace the Models section (`CoworkSidebar.tsx:416-560`) with a slim main-model trigger**

The full expandable Models section (Main/Small/Advisor buttons, small-model/advisor/OCR toggles, Permission Model display) is superseded by the Model Defaults, Advisor, OCR, and Permissions Settings forms. Only the main-model picker trigger has no other home (per the design spec, `ModelDialog.tsx` stays as-is and must remain reachable) — replace the whole section with:

```tsx
        {/* Compact main-model trigger — Small/Advisor/OCR/Permissions moved to
            the Settings tab (see docs/superpowers/specs/2026-08-11-configuration-ui-design.md).
            This is the only remaining sidebar entry point for ModelDialog,
            which stays out of scope for this feature. */}
        <div className="border-b border-zinc-700 px-4 py-2.5">
          <button
            type="button"
            onClick={() => onModelClick?.("main")}
            className="w-full rounded px-1 py-1 text-left text-xs transition-colors hover:bg-zinc-800 disabled:cursor-default disabled:hover:bg-transparent"
            disabled={!onModelClick}
          >
            <div className="text-zinc-500 mb-1">Model</div>
            <div className="text-zinc-300 font-mono truncate">
              {model || config.model || "Not set"}
            </div>
          </button>
        </div>
```

Remove the now-unused `toggleSmallModel`, `toggleAdvisor`, `toggleOcr` handlers and their backing state (`smallModel`, `smallModelEnabled`, `advisorModel`, `advisorEnabled`, `ocrModel`, `ocrEnabled`, `ocrBackend`) **only if** nothing else in the file references them — run `grep -n "toggleSmallModel\|toggleAdvisor\|toggleOcr\|smallModelEnabled\|advisorEnabled\|ocrEnabled" web/src/components/Layout/CoworkSidebar.tsx` after this edit to confirm no other JSX in the file still uses them before deleting their declarations.

- [x] **Step 3: Remove the Extra Allowed Paths section (`CoworkSidebar.tsx:616-649`)**

Delete the whole `{/* Extra Allowed Paths Section */}` block. Run `grep -n "extraPaths" web/src/components/Layout/CoworkSidebar.tsx` afterward; if `extraPaths` is now unused, remove its declaration too.

- [x] **Step 4: Remove the Tools/MCP section (`CoworkSidebar.tsx:769-829`)**

Delete the whole `{/* Tools / MCP Section */}` block. Run `grep -n "mcpServers\|mcpBusy\|toggleMcp" web/src/components/Layout/CoworkSidebar.tsx` afterward; if these are now unused outside this block, remove their declarations too. Leave the Plugins section (opens `PluginsPanel.tsx`) immediately below it untouched — it is not in the removal list for this task (`ExternalPlugins` management is duplicated between the sidebar's quick-access `PluginsPanel` dialog and the new Settings "Plugins & Local Models" form by design, matching how `ModelDialog` also has more than one entry point elsewhere in the app).

- [x] **Step 5: Remove the Theme section (`CoworkSidebar.tsx:885-924`)**

Delete the whole `{/* Theme Section */}` block. Run `grep -n "themes\b\|currentTheme\|applyTheme" web/src/components/Layout/CoworkSidebar.tsx` afterward; if unused outside this block, remove the declarations too.

- [x] **Step 6: Typecheck**

Run: `cd web && bun run typecheck`
Expected: no errors, and specifically no "declared but never used" errors — confirming Steps 2–5's cleanup grep checks were thorough.

- [x] **Step 7: Manual verification**

Via the `run` skill: open the sidebar, confirm Models/Theme/Tools/Paths sections are gone, confirm the compact "Model" trigger still opens `ModelDialog`, confirm Agent/Git/Context/LSP/Files/Plugins/Todo sections are unchanged and still function. Cross-check each removed control's equivalent now works from the Settings tab (Tasks 3–16).

- [x] **Step 8: Commit**

```bash
git add web/src/components/Layout/CoworkSidebar.tsx
git commit -m "refactor(web): remove settings sections from sidebar, moved to Settings tab"
```

---

### Task 18: Desktop native "Settings…" menu item

**Files:**
- Modify: `cmd/ocode-desktop/main.go` (`buildAppMenu`, `196-275`)

**Interfaces:**
- Produces: a native "Settings…" menu item (macOS: app menu; Windows/Linux: File menu) that switches the shared webview to the Settings tab.

- [x] **Step 1: Confirm the exact Wails v3 event-emission API before writing code**

This codebase has **zero** existing Go→JS event-bridge code (confirmed: `confirmQuit`'s dialog is entirely native, no `EmitEvent`/`EventsOn` pattern exists anywhere in this repo). Before writing the Go side, run:
`grep -rn "EmitEvent\|Events\." "$(go env GOPATH)/pkg/mod/github.com/wailsapp/wails/v3"* 2>/dev/null | head -30`
(adjust the glob to match the actual installed version directory) to find the exact v3 method for emitting a browser-visible event from Go, and its corresponding JS-side subscription API (likely exposed on `window` under a Wails-injected namespace — inspect the same module's generated JS bindings or its documentation comments for the exact global name). Do not guess the API surface — this step's grep output determines the exact code for Steps 2–3.

- [x] **Step 2: Add the menu item, emitting whatever event name Step 1 confirmed**

```go
	if runtime.GOOS == "darwin" {
		appMenu := menu.AddSubmenu("ocode")
		appMenu.AddRole(application.About)
		appMenu.AddSeparator()
		appMenu.Add("Settings…").
			SetAccelerator("CmdOrCtrl+,").
			OnClick(func(*application.Context) {
				app.EmitEvent("ocode:open-settings", nil) // exact method name per Step 1's findings
			})
		appMenu.AddSeparator()
		appMenu.AddRole(application.ServicesMenu)
		appMenu.AddSeparator()
		appMenu.AddRole(application.Hide)
		appMenu.AddRole(application.HideOthers)
		appMenu.AddRole(application.UnHide)
		appMenu.AddSeparator()
		appMenu.Add("Quit ocode").
			SetAccelerator("CmdOrCtrl+q").
			OnClick(func(*application.Context) {
				confirmQuit(app, window)
			})
	} else {
		fileMenu := menu.AddSubmenu("File")
		fileMenu.Add("Settings…").
			SetAccelerator("CmdOrCtrl+,").
			OnClick(func(*application.Context) {
				app.EmitEvent("ocode:open-settings", nil) // exact method name per Step 1's findings
			})
		fileMenu.AddSeparator()
		fileMenu.Add("Quit ocode").
			SetAccelerator("CmdOrCtrl+q").
			OnClick(func(*application.Context) {
				confirmQuit(app, window)
			})
	}
```

- [x] **Step 3: Add the JS-side listener in `App.tsx`, using the exact subscription API Step 1 confirmed**

```tsx
  // Desktop-only: the native "Settings…" menu item emits this event to
  // switch the shared webview to the Settings tab. No-op in the browser
  // (the Wails-injected global is undefined there).
  useEffect(() => {
    const wailsEvents = (window as unknown as { wails?: { Events?: { On?: (name: string, cb: () => void) => () => void } } }).wails?.Events;
    if (!wailsEvents?.On) return;
    const unsubscribe = wailsEvents.On("ocode:open-settings", () => setActiveView("settings"));
    return () => unsubscribe?.();
  }, []);
```

(Adjust the `window.wails.Events.On` access path to match whatever global Step 1 actually found — this is a best-effort shape based on common Wails v3 binding conventions, not a confirmed API, per the research finding that no working example exists in this codebase to copy.)

- [x] **Step 4: Build and typecheck**

Run: `go build ./...` — expect no errors.
Run: `cd web && bun run typecheck` — expect no errors.

- [x] **Step 5: Manual verification**

Use the `run` skill to launch the desktop app (not just the web dev server — this is desktop-only). Trigger the "Settings…" menu item (App menu on macOS, File menu on Windows/Linux, or the `CmdOrCtrl+,` accelerator) and confirm the app switches to the Settings tab. If the event bridge doesn't fire (API surface guessed incorrectly in Steps 1–3), fall back to verifying the Settings tab is still reachable by clicking it directly in `TopTabs` — desktop parity is satisfied either way since desktop renders the same web UI, so a failed native-menu wire-up is a minor gap, not a blocker for the rest of this plan.

- [x] **Step 6: Commit**

```bash
git add cmd/ocode-desktop/main.go web/src/App.tsx
git commit -m "feat(desktop): add native Settings menu item"
```

---

## Final verification (after all 18 tasks)

- [x] Run `cd web && bun run typecheck` — expect no errors.
- [x] Run `cd web && bun run build` — expect a clean production build.
- [x] Use the `run` skill to launch both the web dev server and the desktop app; click through every nav item under both "ocode" and "opencode" headings in the Settings tab, confirming each loads without error and Save round-trips at least one field per form.
- [x] Confirm `CoworkSidebar.tsx` no longer shows Models/Theme/Tools/Paths sections, and that the compact "Model" trigger still opens `ModelDialog`.
- [x] Cross-check every nav group in `SettingsPanel.tsx`'s `OCODE_GROUPS`/`OPENCODE_GROUPS` against section 1 of `docs/superpowers/specs/2026-08-11-configuration-ui-design.md` — every listed group must have a real form, no `default: "coming soon"` case should still be reachable.
