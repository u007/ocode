import { describe, expect, it } from "vitest";
import { advisorSelectionPayload, partitionModelSections } from "./modelSelection";
import type { ModelInfo } from "../../api/types";

const model = (
  name: string,
  extra: Partial<ModelInfo> = {},
): ModelInfo => ({
  name,
  model: name.split("/")[1] ?? name,
  provider: name.split("/")[0] ?? "",
  active: false,
  ...extra,
});

const names = (ms: ModelInfo[]) => ms.map((m) => m.name);

describe("partitionModelSections", () => {
  it("surfaces Recently Used first, in saved (backend list) order", () => {
    const models = [
      model("anthropic/claude-a", { recent: true }),
      model("openai/gpt-b", { recent: true }),
      model("groq/compound"),
    ];
    const s = partitionModelSections(models);
    expect(names(s.recents)).toEqual(["anthropic/claude-a", "openai/gpt-b"]);
  });

  it("shows a model that is both favorite and recent only under Recently Used", () => {
    const models = [
      model("anthropic/claude-a", { recent: true, favorite: true }),
      model("openai/gpt-b", { favorite: true }),
    ];
    const s = partitionModelSections(models);
    expect(names(s.recents)).toEqual(["anthropic/claude-a"]);
    expect(names(s.favorites)).toEqual(["openai/gpt-b"]);
    // Not duplicated in the provider groups.
    const all = Object.values(s.providers).flat();
    expect(names(all)).toEqual([]);
    // Placement favors "recent", but the favorite flag must survive on the
    // recent row so the star renders lit and un-favoriting works from there.
    expect(s.recents[0].favorite).toBe(true);
  });

  it("excludes favorites from provider groups but keeps the rest grouped", () => {
    const models = [
      model("anthropic/claude-a", { favorite: true }),
      model("openai/gpt-b"),
      model("openai/gpt-c"),
      model("groq/compound"),
    ];
    const s = partitionModelSections(models);
    expect(names(s.recents)).toEqual([]);
    expect(names(s.favorites)).toEqual(["anthropic/claude-a"]);
    expect(s.providers).toEqual({
      openai: [models[1], models[2]],
      groq: [models[3]],
    });
  });

  it("groups providerless models under Other and handles an empty list", () => {
    const s = partitionModelSections([]);
    expect(s).toEqual({ recents: [], favorites: [], providers: {} });

    const orphan = { ...model("bare-id"), provider: "" };
    const s2 = partitionModelSections([orphan]);
    expect(s2.providers.Other).toEqual([orphan]);
  });
});

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
