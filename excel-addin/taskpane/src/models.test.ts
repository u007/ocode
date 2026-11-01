import { describe, expect, it } from "vitest";
import { connectedModels, filterModels, MODEL_ROWS, type ModelInfo } from "./models";

const m = (provider: string, model: string, display_name = model): ModelInfo => ({ name: `${provider}/${model}`, model, provider, display_name });

describe("connected model list", () => {
  const all = [m("openai", "gpt-z"), m("anthropic", "claude-b", "Claude B"), m("opencode-go", "beam-free", "Beam Free"), m("openai", "gpt-a"), m("deepseek", "chat")];

  it("keeps only providers that are connected, sorted by id", () => {
    expect(connectedModels(all, ["opencode-go", "openai"]).map((x) => x.name)).toEqual(["openai/gpt-a", "openai/gpt-z", "opencode-go/beam-free"]);
  });
  it("leaves out decision-only providers even when they are connected", () => {
    const withTypesafe = [...all, m("typesafe", "jev-latest")];
    expect(connectedModels(withTypesafe, ["typesafe", "deepseek"]).map((x) => x.name)).toEqual(["deepseek/chat"]);
  });
  it("is empty when no provider is connected", () => {
    expect(connectedModels(all, [])).toEqual([]);
  });
  it("does not mutate the catalog it was given", () => {
    const before = all.map((x) => x.name);
    connectedModels(all, ["openai"]);
    expect(all.map((x) => x.name)).toEqual(before);
  });
});

describe("model text filter", () => {
  const list = [m("openai", "gpt-a", "GPT A"), m("opencode-go", "beam-free", "Beam Free")];

  it("matches the id or the display name, ignoring case and surrounding spaces", () => {
    expect(filterModels(list, " BEAM ").rows.map((x) => x.name)).toEqual(["opencode-go/beam-free"]);
    expect(filterModels(list, "gpt a").rows.map((x) => x.name)).toEqual(["openai/gpt-a"]);
    expect(filterModels(list, "nothing").rows).toEqual([]);
  });
  it("returns everything for an empty filter", () => {
    expect(filterModels(list, "  ")).toEqual({ rows: list, total: 2 });
  });
  it("caps the rows and reports the full match count", () => {
    const many = Array.from({ length: MODEL_ROWS + 50 }, (_, i) => m("p", `m${i}`));
    const out = filterModels(many, "");
    expect(out.rows).toHaveLength(MODEL_ROWS);
    expect(out.total).toBe(MODEL_ROWS + 50);
  });
});
