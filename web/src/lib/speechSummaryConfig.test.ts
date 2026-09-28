import { afterEach, describe, expect, it, vi } from "vitest";
import { api, type SpeechSummaryConfig } from "../api/client";
import {
  DEFAULT_SPEECH_SUMMARY_CONFIG,
  EMPTY_SPEECH_SUMMARY_CONFIG,
  setSpeechSummaryConfig,
  speechSummaryDisplay,
  speechSummaryEnabledPatch,
  speechSummaryModelPatch,
} from "./speechSummaryConfig";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("speechSummaryModelPatch", () => {
  it("stores the picker's canonical provider/model id verbatim", () => {
    // Not split into provider/model halves: the id already names its provider
    // and the server parses "provider/model" itself (speechSummaryClient).
    expect(speechSummaryModelPatch("anthropic/claude-haiku-4-5")).toEqual({
      model: "anthropic/claude-haiku-4-5",
    });
    expect(speechSummaryModelPatch("openai/gpt-4o-mini")).toEqual({ model: "openai/gpt-4o-mini" });
  });

  it("carries no provider field, so a pick can never disagree with the model", () => {
    // compactConfig has a companion `summary_provider` that the server gives
    // priority over the model id. This feature deliberately has none: a stale
    // hand-edited provider would keep summarising on the wrong backend after
    // the user picked a different model.
    expect(Object.keys(speechSummaryModelPatch("x/y"))).toEqual(["model"]);
  });

  it("sends an explicit empty string to CLEAR the model back to auto", () => {
    // `undefined` would be dropped by JSON.stringify and leave the stored model
    // in place; the server treats a present empty string as "clear it".
    expect(speechSummaryModelPatch("")).toEqual({ model: "" });
  });
});

describe("speechSummaryEnabledPatch", () => {
  it("always carries the boolean explicitly, including false", () => {
    expect(speechSummaryEnabledPatch(true)).toEqual({ enabled: true });
    expect(speechSummaryEnabledPatch(false)).toEqual({ enabled: false });
  });

  it("does not smuggle the model along", () => {
    // A toggle write must not be able to clear a model the user picked.
    expect(Object.keys(speechSummaryEnabledPatch(false))).toEqual(["enabled"]);
  });
});

describe("speechSummaryDisplay", () => {
  it("shows the configured model", () => {
    expect(speechSummaryDisplay({ model: "anthropic/claude-haiku-4-5" })).toBe("anthropic/claude-haiku-4-5");
  });

  it("spells out the auto chain when no model is configured", () => {
    // The row must not render an empty cell, and must not invent a model that
    // is not actually in use.
    expect(speechSummaryDisplay({ model: "" })).toBe("(auto: small model, then main)");
    expect(speechSummaryDisplay({ model: "   " })).toBe("(auto: small model, then main)");
    expect(speechSummaryDisplay(null)).toBe("(auto: small model, then main)");
    expect(speechSummaryDisplay(undefined)).toBe("(auto: small model, then main)");
  });
});

describe("defaults", () => {
  it("default ON with no model, matching the server's defaultOcodeConfig", () => {
    // The two must agree or the sidebar would show "off" until the first
    // reload. Pinned here so a change to one without the other is caught.
    expect(DEFAULT_SPEECH_SUMMARY_CONFIG).toEqual({ model: "", enabled: true });
    expect(EMPTY_SPEECH_SUMMARY_CONFIG).toEqual({ model: "", enabled: true });
  });
});

describe("setSpeechSummaryConfig", () => {
  it("PUTs only the patched keys and returns the merged block", async () => {
    const saved: SpeechSummaryConfig = { model: "anthropic/claude-haiku-4-5", enabled: true };
    const spy = vi.spyOn(api, "setSpeechSummaryConfig").mockResolvedValue(saved);

    await expect(setSpeechSummaryConfig({ enabled: false })).resolves.toEqual(saved);

    expect(spy).toHaveBeenCalledExactlyOnceWith({ enabled: false }, undefined);
  });

  it("threads the host so a remote project's block is the one written", async () => {
    // A remote session summarises on the HOST; writing to the local server's
    // block would leave the remote session using the wrong model.
    const spy = vi.spyOn(api, "setSpeechSummaryConfig").mockResolvedValue({
      model: "",
      enabled: false,
    });

    await setSpeechSummaryConfig({ model: "openai/gpt-4o-mini" }, "james@217.216.72.49");

    expect(spy).toHaveBeenCalledExactlyOnceWith(
      { model: "openai/gpt-4o-mini" },
      "james@217.216.72.49",
    );
  });
});
