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
