import { api, type SpeechSummaryConfig } from "../api/client";

/**
 * A complete zero-valued speech-summary config.
 *
 * Used where a WHOLE config must be present (defaults before the first load
 * resolves). Writes never send this: `PUT /api/config/ocode/speech-summary`
 * takes a PARTIAL patch, so a write carries only the keys the user actually
 * changed and cannot clobber the other one.
 */
export const EMPTY_SPEECH_SUMMARY_CONFIG: SpeechSummaryConfig = { model: "", enabled: true };

/**
 * A fresh install turns summarising ON with no model configured, which
 * resolves at call time to speech-summary → small → main. The model key is
 * therefore deliberately blank rather than seeded with a guess: showing a model
 * here that is not actually in use would be a lie.
 */
export const DEFAULT_SPEECH_SUMMARY_CONFIG: SpeechSummaryConfig = { model: "", enabled: true };

/**
 * The patch a model pick writes.
 *
 * `model` is the canonical `"provider/model"` id that every model picker hands
 * back, stored verbatim. There is deliberately NO companion provider field: the
 * server resolves the provider from the id itself (internal/agent/
 * `speechSummaryClient`), so a separate provider could only ever disagree with
 * the model the user actually picked.
 */
export function speechSummaryModelPatch(modelId: string): Pick<SpeechSummaryConfig, "model"> {
  return { model: modelId };
}

/** The patch the on/off toggle writes. `enabled` is always explicit. */
export function speechSummaryEnabledPatch(enabled: boolean): Pick<SpeechSummaryConfig, "enabled"> {
  return { enabled };
}

/**
 * Write ONLY the fields that changed.
 *
 * The endpoint merges the keys the body carries onto the block the server
 * reads fresh from disk, so a gate-only write cannot clear a chosen model and
 * a model-only write cannot re-enable something the user turned off. Do NOT
 * re-add a client-side read-modify-write: that reintroduces the race the
 * server-side merge exists to remove, at the cost of an extra round trip.
 * Resolves to the merged block as saved, so callers refresh from it.
 *
 * @param host SSH/WSL host of the session's project. The block lives on the
 *   server that would run the summariser, not the local one.
 */
export function setSpeechSummaryConfig(
  patch: Partial<SpeechSummaryConfig>,
  host?: string,
): Promise<SpeechSummaryConfig> {
  return api.setSpeechSummaryConfig(patch, host);
}

/**
 * Sidebar / dialog label for the speech-summary model. Mirrors the resolution
 * order in `speechSummaryClient`: with nothing configured the summary runs on
 * the small model when that gate is on, otherwise on the main model.
 */
export function speechSummaryDisplay(
  cfg: { model?: string | null } | null | undefined,
): string {
  const model = cfg?.model?.trim();
  return model ? model : "(auto: small model, then main)";
}
