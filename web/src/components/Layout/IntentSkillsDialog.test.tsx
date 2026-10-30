import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import IntentSkillsDialog from "./IntentSkillsDialog";
import { api } from "../../api/client";
import { reloadDynamicCommands } from "../Chat/commands";

vi.mock("../../api/client", () => ({
  api: {
    listIntentSkills: vi.fn(),
    installIntentSkills: vi.fn(),
  },
}));

vi.mock("../Chat/commands", () => ({
  reloadDynamicCommands: vi.fn(() => Promise.resolve()),
}));

const mockList = vi.mocked(api.listIntentSkills);
const mockInstall = vi.mocked(api.installIntentSkills);

beforeEach(() => {
  vi.clearAllMocks();
  mockList.mockResolvedValue({
    version: "0.5.6",
    skills: [
      { name: "domain-discovery", description: "Use when discovering domains." },
      { name: "generate-skill", description: "Use when generating skills." },
      { name: "tree-generator", description: "Use when building a tree." },
    ],
  });
});

function skillBox(name: string): HTMLElement {
  return screen.getByText(name).closest("button") as HTMLElement;
}

describe("IntentSkillsDialog", () => {
  it("lists the fetched skills and installs only the checked ones, sorted", async () => {
    mockInstall.mockResolvedValue({ report: "installed generate-skill in ~/.claude/skills" });
    render(<IntentSkillsDialog open onOpenChange={() => {}} />);

    await waitFor(() => expect(screen.getByText("generate-skill")).toBeTruthy());
    expect(mockList).toHaveBeenCalledTimes(1);

    fireEvent.click(skillBox("tree-generator"));
    fireEvent.click(skillBox("domain-discovery"));
    expect(skillBox("tree-generator").getAttribute("aria-checked")).toBe("true");

    fireEvent.click(screen.getByRole("button", { name: /Install selected/ }));
    await waitFor(() =>
      expect(mockInstall).toHaveBeenCalledWith(["domain-discovery", "tree-generator"]),
    );
    expect(await screen.findByText(/installed generate-skill/)).toBeTruthy();
    expect(reloadDynamicCommands).toHaveBeenCalledTimes(1);
  });

  it("keeps Install disabled until a skill is checked", async () => {
    render(<IntentSkillsDialog open onOpenChange={() => {}} />);
    await waitFor(() => expect(screen.getByText("generate-skill")).toBeTruthy());

    const install = screen.getByRole("button", { name: /Install selected/ }) as HTMLButtonElement;
    expect(install.disabled).toBe(true);
    fireEvent.click(skillBox("generate-skill"));
    expect(install.disabled).toBe(false);
  });

  it("shows the fetch error instead of an empty list", async () => {
    mockList.mockRejectedValue(new Error("registry unreachable"));
    render(<IntentSkillsDialog open onOpenChange={() => {}} />);
    expect(await screen.findByText(/registry unreachable/)).toBeTruthy();
    expect(mockInstall).not.toHaveBeenCalled();
  });
});
