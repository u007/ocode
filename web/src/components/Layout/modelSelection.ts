import type { ModelInfo } from "../../api/types";

/** Build the advisor config payload from the canonical model registry fields. */
export function advisorSelectionPayload(
  model: Pick<ModelInfo, "model" | "provider">,
): { model: string; provider: string } {
  return {
    model: model.model,
    provider: model.provider,
  };
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
