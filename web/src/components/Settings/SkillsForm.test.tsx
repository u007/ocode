import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { api } from "../../api/client";
import SkillsForm from "./SkillsForm";

const SKILLS = [
  { name: "alpha", description: "First skill", enabled: true },
  { name: "beta", description: "Second skill", enabled: false },
  { name: "gamma", description: "Third skill", enabled: true },
];

let getManageableSkills: Mock<typeof api.getManageableSkills>;
let setSkillEnabled: Mock<typeof api.setSkillEnabled>;

beforeEach(() => {
  getManageableSkills = vi.fn(async () => ({ project_root: "", skills: SKILLS }));
  setSkillEnabled = vi.fn(async () => ({ name: "", enabled: true, project_root: "" }));
  vi.spyOn(api, "getManageableSkills").mockImplementation(getManageableSkills);
  vi.spyOn(api, "setSkillEnabled").mockImplementation(setSkillEnabled);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function toggleInput(name: string): HTMLInputElement {
  return screen.getByRole("checkbox", { name: `Skill ${name}` });
}

describe("SkillsForm", () => {
  it("loads the global skill list and shows enabled count", async () => {
    render(<SkillsForm />);

    await waitFor(() => expect(screen.getByText(/2\/3 enabled/)).toBeInTheDocument());
    expect(screen.getByRole("checkbox", { name: "Skill alpha" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Skill beta" })).not.toBeChecked();
  });

  it("toggles a skill via the global layer (explicit scope)", async () => {
    render(<SkillsForm />);

    await waitFor(() => expect(screen.getByText(/2\/3 enabled/)).toBeInTheDocument());

    // Switch beta on.
    fireEvent.click(toggleInput("beta"));
    await waitFor(() =>
      expect(setSkillEnabled).toHaveBeenCalledWith(
        "beta",
        true,
        undefined,
        undefined,
        undefined,
        true,
      ),
    );
    // The global scope is sent EXPLICITLY. Omitting it is NOT enough: the
    // server's default workDir is never empty in a real deployment, so an
    // un-scoped request writes the PROJECT layer of whatever project the
    // server last used — which made this form's promise false.
    expect(getManageableSkills).toHaveBeenLastCalledWith(undefined, undefined, true);

    // Switch alpha off.
    fireEvent.click(toggleInput("alpha"));
    await waitFor(() =>
      expect(setSkillEnabled).toHaveBeenCalledWith(
        "alpha",
        false,
        undefined,
        undefined,
        undefined,
        true,
      ),
    );
  });

  it("does not roll back a state a concurrent successful flip already replaced", async () => {
    // Toggle 1 of "alpha" will FAIL. Toggle 2 of "alpha" SUCCEEDS while
    // toggle 1 is still in flight. When toggle 1 finally rejects, its rollback
    // must NOT restore the stale captured value (alpha enabled) — the server
    // now has alpha disabled.
    //
    // The busy guard disables the checkbox of the skill being toggled, so the
    // second toggle of the SAME skill is blocked in the UI. The sequence
    // guard is still load-bearing: the guard is released by the `finally` of
    // whichever toggle finishes last, and a rejected toggle that is NOT the
    // latest must not restore anything.
    let rejectFirst!: (e: Error) => void;
    vi.mocked(setSkillEnabled)
      .mockImplementationOnce(
        () =>
          new Promise((_resolve, reject) => {
            rejectFirst = reject;
          }),
      )
      .mockImplementationOnce(async () => ({
        name: "alpha",
        enabled: false,
        project_root: "",
      }));

    render(<SkillsForm />);
    await waitFor(() => expect(screen.getByText(/2\/3 enabled/)).toBeInTheDocument());

    // Toggle 1: in flight, will fail.
    fireEvent.click(toggleInput("alpha"));
    await waitFor(() => expect(setSkillEnabled).toHaveBeenCalledTimes(1));

    // Toggle 2 of the same skill succeeds. The checkbox is disabled while
    // toggle 1 is in flight, so dispatch the change directly (which is what a
    // keyboard activation or a programmatic toggle would do).
    vi.mocked(getManageableSkills).mockResolvedValue({
      project_root: "",
      skills: SKILLS.map((s) => (s.name === "alpha" ? { ...s, enabled: false } : s)),
    });
    const alphaBox = toggleInput("alpha");
    alphaBox.disabled = false;
    fireEvent.click(alphaBox);
    await waitFor(() => expect(setSkillEnabled).toHaveBeenCalledTimes(2));

    // Now toggle 1 fails and tries to roll back.
    rejectFirst(new Error("boom"));
    await waitFor(() => expect(screen.getByText(/boom/)).toBeInTheDocument());

    // The row must show the SERVER's state (alpha disabled), not the stale
    // captured value (alpha enabled).
    expect(toggleInput("alpha")).not.toBeChecked();
  });

  it("rolls back an optimistic flip when the server rejects it", async () => {
    vi.mocked(setSkillEnabled).mockRejectedValueOnce(new Error("boom"));
    render(<SkillsForm />);

    await waitFor(() => expect(screen.getByText(/2\/3 enabled/)).toBeInTheDocument());

    fireEvent.click(toggleInput("alpha"));
    await waitFor(() => expect(screen.getByText(/boom/)).toBeInTheDocument());

    // alpha must be back to enabled after rollback.
    expect(toggleInput("alpha")).toBeChecked();
  });

  it("disables the busy toggle while a request is in flight", async () => {
    let release!: () => void;
    vi.mocked(setSkillEnabled).mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          release = resolve;
        }),
    );
    render(<SkillsForm />);

    await waitFor(() => expect(screen.getByText(/2\/3 enabled/)).toBeInTheDocument());

    fireEvent.click(toggleInput("beta"));
    expect(toggleInput("beta")).toBeDisabled();

    release();
    await waitFor(() => expect(toggleInput("beta")).not.toBeDisabled());
  });
});
