import { api, type CompactConfig } from "../api/client";

/**
 * A complete zero-valued compact config. PUT /api/config/ocode/compact
 * REPLACES the whole struct server-side (`h.cfg.Ocode.Compact = req`), so any
 * field missing from the body is silently reset to Go's zero value. Use this
 * rather than a partial literal wherever a whole config must be sent.
 */
export const EMPTY_COMPACT_CONFIG: CompactConfig = {
  enabled: false,
  summary_provider: "",
  summary_model: "",
  token_threshold: 0,
  keep_recent_turns: 0,
  keep_recent_tokens: 0,
  min_messages: 0,
  summary_timeout_seconds: 0,
  summary_first_token_timeout_seconds: 0,
  summary_max_retries: 0,
  max_summary_input_tokens: 0,
};

/**
 * The patch a model pick writes into the compact config.
 *
 * `ModelInfo.name` — what every model picker hands back — is the canonical
 * `"provider/model"` id, so it is stored verbatim. `summary_provider` is
 * cleared because the server gives an EXPLICIT provider priority over the
 * model id's own prefix (internal/agent/agent.go, compactSummaryClient): a
 * stale hand-edited `summary_provider` would keep routing summaries to the
 * wrong backend after the user picked a new model. With the provider empty, a
 * qualified id resolves on its own provider and a bare name falls back to the
 * main model's provider, which is exactly the documented behaviour.
 *
 * `summary_provider` is always present (never `undefined`) because
 * JSON.stringify drops undefined keys — an absent field would clear the stored
 * provider on the server anyway, but only by accident rather than by intent.
 */
export function summaryModelPatch(modelId: string): Pick<CompactConfig, "summary_model" | "summary_provider"> {
  return { summary_model: modelId, summary_provider: "" };
}

/**
 * Write ONLY the fields that changed.
 *
 * `PUT /api/config/ocode/compact` merges the keys the body carries onto the
 * block read fresh from disk (`HandleSetCompactConfig` →
 * `config.SaveOcodeCompactConfigPatch`), so a single-field write cannot reset
 * the threshold, timeout and keep-recent settings that another control owns,
 * and a save cannot revert an edit another window made after this one loaded.
 * An explicit zero is written; only an absent key is left alone.
 *
 * Do NOT re-add a client-side read-modify-write here: it reintroduces the race
 * the server-side merge exists to remove, at the cost of an extra round trip.
 * The returned value is the merged block as saved, so callers refresh from it
 * instead of guessing.
 *
 * @param host SSH/WSL host of the session's project; the block lives on the
 *   server that runs the session, not the local one.
 */
export function setCompactConfig(
  patch: Partial<CompactConfig>,
  host?: string,
): Promise<CompactConfig> {
  return api.setCompactConfig(patch, host);
}

/**
 * Row/dialog label for the summary model. Mirrors the resolution order in
 * compactSummaryClient: with nothing configured the summary runs on the small
 * model when that gate is on, otherwise on the main model.
 */
export function compactSummaryDisplay(
  cfg: { summary_model?: string | null } | null | undefined,
): string {
  const model = cfg?.summary_model?.trim();
  return model ? model : "(auto: small model, then main)";
}
