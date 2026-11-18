import { describe, expect, it } from "vitest";
import { matchesFilter, type UmlFilter } from "./umlFilter";
import type { UmlSymbol } from "../../api/types";

const sym = (over: Partial<UmlSymbol>): UmlSymbol => ({
  id: "x", name: "Name", kind: "struct", lang: "Go", pkg: "internal/agent",
  file: "internal/agent/agent.go", line: 10, end_line: 20,
  ...over,
});
const filter = (over: Partial<UmlFilter>): UmlFilter => ({
  query: "", langs: new Set(["Go"]), kinds: new Set(["struct", "interface"]), pkg: "all",
  ...over,
});

describe("matchesFilter", () => {
  it("matches nothing when the language is off", () => {
    expect(matchesFilter(sym({}), filter({ langs: new Set(["TS"]) }))).toBe(false);
  });
  it("matches nothing when the kind is off", () => {
    expect(matchesFilter(sym({ kind: "func" }), filter({}))).toBe(false);
  });
  it("scopes to the selected package", () => {
    expect(matchesFilter(sym({ pkg: "internal/server" }), filter({ pkg: "internal/agent" }))).toBe(false);
    expect(matchesFilter(sym({}), filter({ pkg: "internal/agent" }))).toBe(true);
  });
  it("requires every query token", () => {
    const s = sym({ name: "AgentSession", doc: "per-session runtime", sig: "" });
    expect(matchesFilter(s, filter({ query: "agent session" }))).toBe(true);
    expect(matchesFilter(s, filter({ query: "agent bogus" }))).toBe(false);
  });
  it("searches members, receiver and file path", () => {
    const s = sym({ members: [{ kind: "method", name: "Recap", sig: "" }] });
    expect(matchesFilter(s, filter({ query: "recap" }))).toBe(true);
    expect(matchesFilter(s, filter({ query: "agent.go" }))).toBe(true);
    expect(matchesFilter(sym({ receiver: "AgentSession" }), filter({ query: "agentsession" }))).toBe(true);
  });
});
