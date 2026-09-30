import { describe, expect, it } from "vitest";
import { isRecentUserInput, RECENT_INPUTS_MAX, recentUserInputs } from "./recentInputs";

const user = (content: string) => ({ role: "user" as const, content });
const assistant = (content: string) => ({ role: "assistant" as const, content });

describe("recentUserInputs", () => {
  it("returns at most 2 inputs, newest last, so the newest sits nearest the composer", () => {
    const messages = [user("first ask"), user("second ask"), user("third ask")];
    expect(recentUserInputs(messages)).toEqual(["second ask", "third ask"]);
    expect(recentUserInputs(messages)).toHaveLength(RECENT_INPUTS_MAX);
  });

  it("returns fewer than 2 when the session has fewer inputs", () => {
    expect(recentUserInputs([user("only ask")])).toEqual(["only ask"]);
    expect(recentUserInputs([])).toEqual([]);
  });

  it("excludes slash-command echoes (composes the shared countable predicate)", () => {
    // `/help` output is role "user" on the wire but is chrome, not a prompt.
    expect(recentUserInputs([user("real ask"), user("/help")])).toEqual(["real ask"]);
  });

  it("excludes system-injected user-role messages the agent writes itself", () => {
    // These ARE persisted with role "user" (verified across 400 transcripts:
    // 113 "[advisor plan checkpoint]", 82 "[advisor completion checkpoint]",
    // 7 "[ocode:event]"), so a raw role check would fill the strip with
    // advisor noise instead of what the user typed.
    const messages = [
      user("the actual question"),
      user("[advisor plan checkpoint] An advisor reviewed the changes you just made:"),
      assistant("..."),
      user("[advisor completion checkpoint] Before finishing, an advisor reviewed your final report:"),
      user("[ocode:event] out-of-band completion notice, not a user instruction"),
    ];
    expect(recentUserInputs(messages)).toEqual(["the actual question"]);
  });

  it("excludes any [ocode:*] injected tail, including markers added later", () => {
    // The denylist is a prefix rule on "[ocode:" rather than an enumeration, so
    // a new injected tail (todo/lsp/notes/discovery/...) is covered without a
    // second edit here.
    for (const marker of ["[ocode:todo]", "[ocode:lsp]", "[ocode:notes]", "[ocode:brand-new]"]) {
      expect(recentUserInputs([user("typed"), user(`${marker} injected`)])).toEqual(["typed"]);
    }
  });

  it("ignores empty / whitespace-only user messages", () => {
    expect(recentUserInputs([user("kept"), user("   \n  ")])).toEqual(["kept"]);
  });

  it("trims surrounding whitespace so the rendered line is not blank-looking", () => {
    expect(recentUserInputs([user("\n  padded ask  \n")])).toEqual(["padded ask"]);
  });

  it("does not mutate or alias the source messages", () => {
    const messages = [user("one"), user("two")];
    const frozen = Object.freeze(messages.slice());
    recentUserInputs(frozen);
    expect(messages).toHaveLength(2);
  });

  it("honours an explicit limit", () => {
    const messages = [user("a"), user("b"), user("c"), user("d")];
    expect(recentUserInputs(messages, 3)).toEqual(["b", "c", "d"]);
    expect(recentUserInputs(messages, 0)).toEqual([]);
  });
});

describe("isRecentUserInput", () => {
  it("accepts a plain typed prompt", () => {
    expect(isRecentUserInput(user("fix the bug"))).toBe(true);
  });

  it("rejects non-user roles", () => {
    expect(isRecentUserInput(assistant("I fixed it"))).toBe(false);
    expect(isRecentUserInput({ role: "tool", content: "output" })).toBe(false);
    expect(isRecentUserInput({ role: "system", content: "be helpful" })).toBe(false);
  });
});
