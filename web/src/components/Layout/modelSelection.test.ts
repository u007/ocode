import { describe, expect, it } from "vitest";
import { advisorSelectionPayload } from "./modelSelection";

describe("advisorSelectionPayload", () => {
  it("keeps the provider and strips it from the persisted model value", () => {
    expect(
      advisorSelectionPayload({
        provider: "anthropic",
        model: "claude-sonnet-4-6",
      }),
    ).toEqual({
      provider: "anthropic",
      model: "claude-sonnet-4-6",
    });
  });
});
