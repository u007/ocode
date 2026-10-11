import { describe, it, expect } from "vitest";
import {
  dedupeRows,
  diffBashRules,
  isEmptyDelta,
  normalizeLevel,
  normalizePrefix,
  rowsFromResponse,
  validateRule,
  type BashRuleRow,
} from "./bashRulesDiff";

const rows = (...pairs: [string, string][]): BashRuleRow[] =>
  pairs.map(([prefix, level]) => ({ prefix, level: level as BashRuleRow["level"] }));

describe("bashRulesDiff", () => {
  it("sends nothing when the staged rows equal the baseline", () => {
    const base = rows(["git push", "deny"], ["git status", "allow"]);
    expect(diffBashRules(base, [...base])).toEqual({ set: {}, remove: [] });
    expect(isEmptyDelta(diffBashRules(base, [...base]))).toBe(true);
  });

  it("emits a set for a changed level and for a new row", () => {
    const base = rows(["git push", "deny"]);
    const staged = rows(["git push", "ask"], ["sed", "deny"]);
    expect(diffBashRules(base, staged)).toEqual({
      set: { "git push": "ask", sed: "deny" },
      remove: [],
    });
  });

  it("emits a remove for a dropped row and a set for one the editor added", () => {
    // The editor removed "git push" and added "curl" itself, so BOTH halves of
    // the delta must name them. (A rule another surface added is a different
    // case: it is absent from BOTH lists here, so it is never mentioned and the
    // server keeps it — covered end to end by
    // TestSetBashRulesDeltaPreservesConcurrentExternalWrite.)
    const base = rows(["git push", "deny"]);
    const staged = rows(["curl", "deny"]);
    expect(diffBashRules(base, staged)).toEqual({ set: { curl: "deny" }, remove: ["git push"] });
  });

  it("never mentions a rule that is in neither the baseline nor the staged list", () => {
    // "curl" landed on the server from another surface after the load. The diff
    // must be silent about it, which is what stops a full save from deleting it.
    const base = rows(["git push", "deny"]);
    const staged = rows(["git push", "ask"]);
    expect(diffBashRules(base, staged)).toEqual({ set: { "git push": "ask" }, remove: [] });
  });

  it("expresses a rename as a remove of the old key plus a set of the new one", () => {
    const base = rows(["git push", "deny"]);
    const staged = rows(["git push origin", "deny"]);
    expect(diffBashRules(base, staged)).toEqual({
      set: { "git push origin": "deny" },
      remove: ["git push"],
    });
  });

  it("re-adds a removed prefix at a new level as a set, not a remove", () => {
    const base = rows(["sed", "deny"]);
    const staged = rows(["sed", "allow"]);
    const delta = diffBashRules(base, staged);
    expect(delta).toEqual({ set: { sed: "allow" }, remove: [] });
    expect(delta.remove).not.toContain("sed");
  });

  it("normalizes a typed prefix before comparing, and skips blank rows", () => {
    const base = rows(["git push", "deny"]);
    const staged = rows(["  git   push  ", "deny"], ["   ", "ask"]);
    expect(diffBashRules(base, staged)).toEqual({ set: {}, remove: [] });
  });

  it("sorts both the set keys and the removes so the payload is deterministic", () => {
    const base = rows(["z", "deny"], ["a", "deny"], ["m", "deny"]);
    const staged: BashRuleRow[] = [];
    const delta = diffBashRules(base, staged);
    expect(delta.remove).toEqual(["a", "m", "z"]);
    expect(Object.keys(delta.set ?? {})).toEqual([]);
  });

  it("counts a rename as two pending changes", () => {
    const base = rows(["git push", "deny"]);
    const delta = diffBashRules(base, rows(["git push --dry-run", "deny"]));
    expect(isEmptyDelta(delta)).toBe(false);
  });
});

describe("validateRule", () => {
  it("rejects a blanket git allow, mirroring the server", () => {
    expect(validateRule("git", "allow")).toMatch(/cannot be always-allowed/);
    expect(validateRule("git", "deny")).toBeNull();
    expect(validateRule("git push", "allow")).toBeNull();
  });

  it("rejects a blank prefix and the reserved internal key", () => {
    expect(validateRule("", "deny")).toMatch(/required/);
    expect(validateRule("__inroot__:cat:/tmp", "allow")).toMatch(/reserved/);
  });
});

describe("helpers", () => {
  it("normalizes prefixes and levels", () => {
    expect(normalizePrefix("  git   push ")).toBe("git push");
    expect(normalizeLevel("DENY")).toBe("ask");
    expect(normalizeLevel("deny")).toBe("deny");
  });

  it("collapses duplicate prefixes, last row wins", () => {
    // Map.set keeps the FIRST insertion position but the LAST value, so the
    // surviving row stays where the user first put it.
    const deduped = dedupeRows(rows(["git push", "deny"], ["sed", "ask"], ["git push", "allow"]));
    expect(deduped).toEqual(rows(["git push", "allow"], ["sed", "ask"]));
  });

  it("builds sorted rows from a server response and tolerates junk", () => {
    expect(
      rowsFromResponse([
        { tool: "sed", level: "deny" },
        { tool: "git status", level: "allow" },
      ]),
    ).toEqual(rows(["git status", "allow"], ["sed", "deny"]));
    expect(rowsFromResponse(undefined)).toEqual([]);
  });
});
