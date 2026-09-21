import { describe, expect, it } from "vitest";
import { advisorSelectionPayload, capProviderGroups, claudeCodeAdvisorModelInfos, CLAUDE_CODE_ADVISOR_MODELS, CLAUDE_CODE_PROVIDER, LOCAL_MODELS_PROVIDER, LOCAL_MODELS_UNCAPPED, partitionModelSections } from "./modelSelection";
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

describe("claudeCodeAdvisorModelInfos", () => {
  it("builds claude-code rows carrying the full id and the bare model alias", () => {
    const rows = claudeCodeAdvisorModelInfos();
    expect(rows.map((r) => r.name)).toEqual(
      CLAUDE_CODE_ADVISOR_MODELS.map((m) => `${CLAUDE_CODE_PROVIDER}/${m}`),
    );
    expect(rows.every((r) => r.provider === CLAUDE_CODE_PROVIDER)).toBe(true);
    expect(rows.every((r) => r.active === false)).toBe(true);
    // The payload the dialog PUTs must persist the bare alias — the server
    // passes it to `claude -p --model`, which rejects a "provider/model" id.
    expect(advisorSelectionPayload(rows[0])).toEqual({
      model: "claude-sonnet-4-6",
      provider: "claude-code",
    });
  });

  it("offers the Claude Code CLI aliases the TUI picker lists", () => {
    // Sync guard for internal/tui/picker.go prependClaudeCodeSection.
    for (const alias of [
      "claude-sonnet-4-6",
      "claude-sonnet-5",
      "claude-opus-4-8",
      "claude-opus-4-7",
      "claude-opus-5",
      "claude-haiku-4-5",
      "claude-fable-5",
    ]) {
      expect(CLAUDE_CODE_ADVISOR_MODELS).toContain(alias);
    }
  });
});

describe("capProviderGroups", () => {
  it("keeps every row when the total is under the limit", () => {
    const groups = {
      a: [model("a/1"), model("a/2")],
      b: [model("b/1")],
    };
    const cap = capProviderGroups(groups, 10);
    expect(names(Object.values(cap.groups).flat())).toEqual(["a/1", "a/2", "b/1"]);
    expect(cap.hidden).toBe(0);
  });

  it("splits the last visible provider at the limit and counts the rest as hidden", () => {
    const groups = {
      a: [model("a/1"), model("a/2")],
      b: [model("b/1"), model("b/2"), model("b/3")],
      c: [model("c/1")],
    };
    const cap = capProviderGroups(groups, 4);
    // a (2) fits, b is truncated to 2, c is dropped entirely.
    expect(Object.keys(cap.groups)).toEqual(["a", "b"]);
    expect(names(cap.groups.a)).toEqual(["a/1", "a/2"]);
    expect(names(cap.groups.b)).toEqual(["b/1", "b/2"]);
    expect(cap.hidden).toBe(2); // b/3 + c/1
  });

  it("preserves provider and per-provider model order", () => {
    const groups = {
      z: [model("z/1")],
      a: [model("a/1"), model("a/2")],
    };
    const cap = capProviderGroups(groups, 2);
    expect(Object.keys(cap.groups)).toEqual(["z", "a"]);
    expect(names(cap.groups.a)).toEqual(["a/1"]);
    expect(cap.hidden).toBe(1);
  });

  it("never trims or counts uncapped groups against the budget", () => {
    const groups = {
      a: Array.from({ length: 5 }, (_, i) => model(`a/${i}`)),
      [LOCAL_MODELS_PROVIDER]: [model("local-1"), model("local-2")],
    };
    // Budget smaller than provider a alone: without the exemption the local
    // group would be sliced to zero.
    const cap = capProviderGroups(groups, 3, LOCAL_MODELS_UNCAPPED);
    expect(names(cap.groups[LOCAL_MODELS_PROVIDER])).toEqual(["local-1", "local-2"]);
    expect(names(cap.groups.a)).toEqual(["a/0", "a/1", "a/2"]);
    expect(cap.hidden).toBe(2); // a/3 + a/4 only; locals are not counted
  });

  it("trims the local group when it is not exempted (regression guard)", () => {
    const groups = {
      a: Array.from({ length: 5 }, (_, i) => model(`a/${i}`)),
      [LOCAL_MODELS_PROVIDER]: [model("local-1"), model("local-2")],
    };
    const cap = capProviderGroups(groups, 3);
    expect(cap.groups[LOCAL_MODELS_PROVIDER]).toBeUndefined();
    expect(cap.hidden).toBe(4); // a/3 + a/4 + both locals
  });
});
