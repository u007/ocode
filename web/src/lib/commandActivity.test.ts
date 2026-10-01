import { beforeEach, describe, expect, it } from "vitest";
import {
  __resetSessionActivityForTests,
  clearCommandActivity,
  clearSessionActivity,
  getSessionActivity,
  rekeySessionActivity,
  setCommandActivity,
  setSkillActivity,
} from "./commandActivity";

beforeEach(() => {
  __resetSessionActivityForTests();
});

describe("setCommandActivity / clearCommandActivity", () => {
  it("records a running command and clears it again", () => {
    const entry = setCommandActivity("s1", "/recap");
    expect(getSessionActivity("s1")).toEqual(entry);
    expect(entry.kind).toBe("command");
    if (entry.kind === "command") expect(entry.label).toBe("/recap");

    clearCommandActivity("s1", entry);
    expect(getSessionActivity("s1")).toBeUndefined();
  });

  it("keeps sessions isolated", () => {
    setCommandActivity("s1", "/recap");
    setCommandActivity("s2", "/share");

    const entry = getSessionActivity("s1");
    expect(entry).toBeDefined();
    clearCommandActivity("s1", entry!);

    // Clearing one session must not disturb another tab's bar.
    expect(getSessionActivity("s2")).toBeDefined();
    expect(getSessionActivity("s1")).toBeUndefined();
  });

  // The real race: /recap can burn a minute, and by the time it resolves the
  // user has sent a new message and the model has loaded a skill. The stale
  // command's cleanup must not erase that newer live indicator.
  it("does not erase a newer activity that replaced it", () => {
    const stale = setCommandActivity("s1", "/recap");
    setSkillActivity("s1", "git-commit-push");

    clearCommandActivity("s1", stale);

    const current = getSessionActivity("s1");
    expect(current?.kind).toBe("skill");
  });

  it("ignores a clear for a session with no activity", () => {
    const stale = setCommandActivity("s1", "/recap");
    clearSessionActivity("s1");
    expect(() => clearCommandActivity("s1", stale)).not.toThrow();
    expect(getSessionActivity("s1")).toBeUndefined();
  });
});

describe("setSkillActivity", () => {
  it("records the skill for the turn", () => {
    setSkillActivity("s1", "git-commit-push");
    const activity = getSessionActivity("s1");
    expect(activity?.kind).toBe("skill");
    if (activity?.kind === "skill") expect(activity.name).toBe("git-commit-push");
  });

  it("is replaced by a later skill in the same turn", () => {
    setSkillActivity("s1", "brainstorming");
    setSkillActivity("s1", "writing-plans");
    const activity = getSessionActivity("s1");
    if (activity?.kind === "skill") expect(activity.name).toBe("writing-plans");
  });
});

describe("clearSessionActivity", () => {
  // Abort emits neither turn_done nor turn_error, so this is the path that
  // stops a bar hanging for the rest of the session.
  it("drops a skill bar on abort", () => {
    setSkillActivity("s1", "git-commit-push");
    clearSessionActivity("s1");
    expect(getSessionActivity("s1")).toBeUndefined();
  });

  it("is a no-op when nothing is running", () => {
    expect(() => clearSessionActivity("s1")).not.toThrow();
  });

  it("clears only the session it is given", () => {
    setSkillActivity("s1", "a");
    setSkillActivity("s2", "b");
    clearSessionActivity("s1");
    expect(getSessionActivity("s2")).toBeDefined();
  });
});

describe("rekeySessionActivity", () => {
  // CLAUDE.md: any session-keyed map must move with /reset-id or it is stranded
  // under the deleted id.
  it("moves a bar to the new session id", () => {
    setSkillActivity("old", "git-commit-push");
    rekeySessionActivity("old", "new");

    expect(getSessionActivity("old")).toBeUndefined();
    const moved = getSessionActivity("new");
    expect(moved?.kind).toBe("skill");
    if (moved?.kind === "skill") expect(moved.name).toBe("git-commit-push");
  });

  it("is a no-op when the old session has no activity", () => {
    expect(() => rekeySessionActivity("old", "new")).not.toThrow();
    expect(getSessionActivity("new")).toBeUndefined();
  });
});

describe("getSessionActivity", () => {
  it("tolerates a missing session id", () => {
    expect(getSessionActivity(null)).toBeUndefined();
    expect(getSessionActivity(undefined)).toBeUndefined();
  });
});