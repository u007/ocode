import type { ModelInfo } from "../../api/types";

/**
 * Provider label for the client-appended local/LM Studio models. Shared with
 * ModelDialog's `withLocalModels` so the group can be exempted from the render
 * cap by the exact same key it is created under.
 */
export const LOCAL_MODELS_PROVIDER = "Local Models";

/** The provider groups that must never be trimmed or counted against the
 *  render cap (see capProviderGroups). */
export const LOCAL_MODELS_UNCAPPED: ReadonlySet<string> = new Set([LOCAL_MODELS_PROVIDER]);


/** Build the advisor config payload from the canonical model registry fields. */
export function advisorSelectionPayload(
  model: Pick<ModelInfo, "model" | "provider">,
): { model: string; provider: string } {
  return {
    model: model.model,
    provider: model.provider,
  };
}

/**
 * Sentinel provider the server interprets as the Claude Code CLI backend.
 * `PUT /api/advisor` derives `claude_code` from `provider === "claude-code"`
 * (internal/server/handler_config.go), and the agent's AdvisorTool then shells
 * out to `claude -p` with the configured model instead of calling a provider
 * API (internal/agent/advisor_tool.go). The provider half of a claude-code
 * selection is therefore never used to build an LLM client.
 */
export const CLAUDE_CODE_PROVIDER = "claude-code";

/** Header for the Claude Code CLI group in the advisor picker. Mirrors the
 *  TUI's "› Claude Code (Read-Only CLI)" section title (internal/tui/picker.go). */
export const CLAUDE_CODE_SECTION_TITLE = "Claude Code (Read-Only CLI)";

/**
 * Claude model aliases offered under the Claude Code CLI section. MUST stay in
 * sync with the `claudeCodeModels` list in `prependClaudeCodeSection`
 * (internal/tui/picker.go) — both name the aliases the `claude` CLI accepts.
 */
export const CLAUDE_CODE_ADVISOR_MODELS: readonly string[] = [
  "claude-sonnet-4-6",
  "claude-sonnet-5",
  "claude-opus-4-8",
  "claude-opus-4-7",
  "claude-opus-5",
  "claude-haiku-4-5",
  "claude-fable-5",
];

/**
 * Picker rows for the advisor's Claude Code CLI section. The models.dev
 * registry has no `claude-code` provider, so these are synthetic ModelInfo
 * entries; giving them the normal shape lets them flow through the same
 * search/filter/group/render path as registry models. `name` carries the full
 * "claude-code/<model>" id the TUI displays, `model` the bare alias the server
 * persists (see advisorSelectionPayload).
 */
export function claudeCodeAdvisorModelInfos(): ModelInfo[] {
  return CLAUDE_CODE_ADVISOR_MODELS.map((model) => ({
    name: `${CLAUDE_CODE_PROVIDER}/${model}`,
    model,
    provider: CLAUDE_CODE_PROVIDER,
    active: false,
  }));
}

/** The model list split into the picker's display sections. */
export interface ModelSections {
  /** Recently used models, in saved order (first section in the picker). */
  recents: ModelInfo[];
  /** Favorites not already shown under Recently Used (TUI-style dedupe). */
  favorites: ModelInfo[];
  /** Remaining models grouped by provider, backend ordering preserved. */
  providers: Record<string, ModelInfo[]>;
}

/**
 * Split the GET /api/models list into picker sections mirroring the TUI model
 * picker (openModelPicker in internal/tui/picker.go): Recently Used first,
 * then ★ Favorites (models already shown in Recently Used are deduped out),
 * then the remaining models grouped by provider with both excluded. The
 * backend already returns favorites/recents first and preserves saved order,
 * so this relies on list order, not on re-sorting.
 */
export function partitionModelSections(models: ModelInfo[]): ModelSections {
  const recents = models.filter((m) => m.recent);
  const inRecents = new Set(recents.map((m) => m.name));
  const favorites = models.filter((m) => m.favorite && !inRecents.has(m.name));
  const shown = new Set<string>(inRecents);
  for (const f of favorites) shown.add(f.name);
  const providers: Record<string, ModelInfo[]> = {};
  for (const m of models) {
    if (shown.has(m.name)) continue;
    const provider = m.provider || "Other";
    (providers[provider] ??= []).push(m);
  }
  return { recents, favorites, providers };
}

/** Default-view render cap for provider-grouped models. The full models.dev
 *  registry is thousands of entries across hundreds of providers; mounting
 *  every row makes the dialog's first paint slow (measured ~1.5s for 8k rows),
 *  so the unfiltered view renders at most this many provider rows and points
 *  the user at the search box for the rest. Recents/favorites are rendered
 *  separately and are never capped. */
export const MAX_VISIBLE_PROVIDER_MODELS = 500;

export interface CappedProviderGroups {
  groups: Record<string, ModelInfo[]>;
  /** Number of provider rows omitted by the cap. */
  hidden: number;
}

/**
 * Cap provider-grouped models to `limit` rows in total, preserving provider
 * order and each provider's model order (a provider may be split if the cap
 * lands mid-group). Search still filters the FULL model list before this runs,
 * so refining the query always reveals the hidden entries.
 *
 * `uncapped` names provider groups that must never be trimmed or counted
 * against the budget — the client-appended "Local Models" group for the
 * permission/mask/autocontinue pickers. Those rows exist precisely so the
 * typically-local judge models are selectable, so a large configured registry
 * must not push them out.
 */
export function capProviderGroups(
  groups: Record<string, ModelInfo[]>,
  limit = MAX_VISIBLE_PROVIDER_MODELS,
  uncapped: ReadonlySet<string> = new Set(),
): CappedProviderGroups {
  const out: Record<string, ModelInfo[]> = {};
  let budget = limit;
  let hidden = 0;
  for (const [provider, models] of Object.entries(groups)) {
    if (uncapped.has(provider)) {
      out[provider] = models;
      continue;
    }
    if (budget <= 0) {
      hidden += models.length;
      continue;
    }
    if (models.length <= budget) {
      out[provider] = models;
      budget -= models.length;
    } else {
      out[provider] = models.slice(0, budget);
      hidden += models.length - budget;
      budget = 0;
    }
  }
  return { groups: out, hidden };
}
