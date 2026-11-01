/** One entry of ocode's model catalog (GET /api/models). `name` is "provider/model", the id ocode's setModel takes. */
export interface ModelInfo { name: string; model: string; provider: string; display_name: string }

/** The picker shows at most this many rows; the rest are reached by typing in the filter. */
export const MODEL_ROWS = 200;

/**
 * Providers that answer decisions only (typesafe's Chat fails with ErrTypesafeDecisionOnly in ocode), so a chat cannot
 * use their models even when they are connected.
 */
const DECISION_ONLY_PROVIDERS = new Set(["typesafe"]);

/**
 * Only models from providers ocode reports as connected, minus decision-only providers. The catalog has ~9000 entries,
 * most of them unusable here.
 */
export function connectedModels(all: readonly ModelInfo[], providers: readonly string[]): ModelInfo[] {
  const ids = new Set(providers);
  return all.filter((m) => ids.has(m.provider) && !DECISION_ONLY_PROVIDERS.has(m.provider)).sort((a, b) => a.name.localeCompare(b.name));
}

/** Case-insensitive text match on the id or the display name, capped at MODEL_ROWS. `total` counts every match. */
export function filterModels(list: readonly ModelInfo[], query: string): { rows: ModelInfo[]; total: number } {
  const q = query.trim().toLowerCase();
  const matches = q ? list.filter((m) => m.name.toLowerCase().includes(q) || m.display_name.toLowerCase().includes(q)) : list;
  return { rows: matches.slice(0, MODEL_ROWS), total: matches.length };
}
