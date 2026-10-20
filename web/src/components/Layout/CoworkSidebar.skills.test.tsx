import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import { fireEvent, render, cleanup, screen, waitFor } from "@testing-library/react";
import CoworkSidebar from "./CoworkSidebar";
import { ChatProvider } from "../../stores/chatStore";
import { api } from "../../api/client";
import { reloadDynamicCommands } from "../Chat/commands";
import type { ManageableSkill } from "../../api/types";

// The Skills section lists every discoverable skill for the active project with
// its effective enabled state: enabled first, disabled dimmed below. "Enabled"
// means discoverable — the toggle writes the PROJECT layer (.ocode/settings.json)
// and the server rebuilds this chat's agent so the catalog takes effect.
vi.mock("../Chat/commands", () => ({
  reloadDynamicCommands: vi.fn(() => Promise.resolve()),
}));

vi.mock("../../stores/projectStore", () => ({
  findProjectPathForTab: () => undefined,
  findTabForSession: () => undefined,
  useProjectState: () => ({
    activeTabId: "session-1",
    state: { activeProject: null },
    dispatch: vi.fn(),
  }),
}));

const SKILLS: ManageableSkill[] = [
  // Deliberately unordered: the component must put enabled ones on top.
  { name: "alpha", description: "disabled one", enabled: false },
  { name: "gamma", description: "enabled one", enabled: true },
  { name: "beta", description: "enabled one", enabled: true },
];

vi.mock("../../api/client", () => ({
  api: {
    listAgents: vi.fn(() => Promise.resolve([])),
    getConfigModel: vi.fn(() => Promise.resolve({ model: "test-model" })),
    getThinkingBudget: vi.fn(() => Promise.resolve({ budget: 0 })),
    getPermissionModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getYolo: vi.fn(() => Promise.resolve({ yolo: false })),
    getRecapConfig: vi.fn(() => Promise.resolve({})),
    getAdvisor: vi.fn(() => Promise.resolve({ model: "" })),
    getAdvisorEnabled: vi.fn(() => Promise.resolve({ enabled: false })),
    getSmallModelWithEnabled: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getExplorerModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getContextModel: vi.fn(() => Promise.resolve({ model: "", enabled: false })),
    getAutoContinue: vi.fn(() => Promise.resolve({ enabled: false, model: "" })),
    setAutoContinue: vi.fn(() => Promise.resolve({ enabled: false, model: "" })),
    getDiscoveryConfig: vi.fn(() => Promise.resolve(null)),
    getPathsConfig: vi.fn(() => Promise.resolve({ extra_allowed_paths: [], upload_dir: "" })),
    setDiscoveryConfig: vi.fn(),
    getCompactConfig: vi.fn(() =>
      Promise.resolve({
        enabled: false,
        threshold_pct: 0,
        summary_model: "",
        max_summary_input_tokens: 0,
        summary_timeout_seconds: 0,
        summary_first_token_timeout_seconds: 0,
        summary_max_tokens: 0,
        keep_recent_messages: 0,
      }),
    ),
    setCompactConfig: vi.fn(() => Promise.resolve({})),
    getSpeechSummaryConfig: vi.fn(() =>
      Promise.resolve({ enabled: false, model: "" }),
    ),
    setSpeechSummaryConfig: vi.fn(() => Promise.resolve({})),
    getSessionStatus: vi.fn(() => Promise.resolve({})),
    getMCP: vi.fn(() => Promise.resolve([])),
    setMCPEnabled: vi.fn(() => Promise.resolve({ name: "", status: "" })),
    getManageableSkills: vi.fn(() => Promise.resolve({ project_root: "", skills: SKILLS })),
    setSkillEnabled: vi.fn(() =>
      Promise.resolve({ name: "alpha", enabled: true, project_root: "" }),
    ),
  },
  apiPath: (p: string) => p,
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
  authHeaders: () => ({}),
  // eventBus's SSE loop calls this on a failed stream; without it the mock
  // throws "No reportAuthFailure export" noise into every test.
  reportAuthFailure: vi.fn(),
}));

const fetchMock = vi.fn(() => Promise.resolve({ json: () => Promise.resolve({}) } as Response));
vi.stubGlobal("fetch", fetchMock);

afterEach(() => {
  cleanup();
});

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  vi.mocked(api.getManageableSkills).mockResolvedValue({
    project_root: "",
    skills: SKILLS,
  });
  vi.mocked(api.setSkillEnabled).mockResolvedValue({
    name: "alpha",
    enabled: true,
    project_root: "",
  });
});

function renderSidebar() {
  return render(
    <ChatProvider>
      <CoworkSidebar isOpen onClose={() => {}} activeAgent="build" />
    </ChatProvider>,
  );
}

// The section is collapsed by default (DEFAULT_SECTIONS.skills = false).
async function openSkillsSection() {
  if (screen.queryByLabelText("Skill alpha")) return;
  fireEvent.click(screen.getByText("Skills"));
  await waitFor(() => {
    expect(screen.getByLabelText("Skill alpha")).toBeTruthy();
  });
}

function skillLabelsInOrder(): string[] {
  return screen
    .getAllByLabelText(/^Skill /)
    .map((el) => el.getAttribute("aria-label") || "");
}

describe("CoworkSidebar Skills toggle", () => {
  it("lists enabled skills first and keeps disabled ones below", async () => {
    renderSidebar();
    await openSkillsSection();

    // Document order is the render order: the two enabled skills (alphabetical),
    // then the disabled one — dimmed, but still present so it can be restored.
    expect(skillLabelsInOrder()).toEqual(["Skill beta", "Skill gamma", "Skill alpha"]);

    const alpha = screen.getByLabelText("Skill alpha") as HTMLInputElement;
    const beta = screen.getByLabelText("Skill beta") as HTMLInputElement;
    expect(alpha.checked).toBe(false);
    expect(beta.checked).toBe(true);
    // Disabled rows are visually de-emphasised (dimmed), not hidden.
    expect(alpha.closest("label")?.className).toContain("opacity-60");
    expect(beta.closest("label")?.className).not.toContain("opacity-60");
  });

  it("queries the list for the active project", async () => {
    renderSidebar();
    await openSkillsSection();
    // activeProject is null in the stub, so only the host (undefined) is sent.
    expect(api.getManageableSkills).toHaveBeenCalledWith(undefined, undefined);
  });

  it("enables a skill, refetches, and reloads the slash commands", async () => {
    renderSidebar();
    await openSkillsSection();

    fireEvent.click(screen.getByLabelText("Skill alpha"));

    await waitFor(() => {
      // name, next enabled state, host, project path, session id
      expect(api.setSkillEnabled).toHaveBeenCalledWith(
        "alpha",
        true,
        undefined,
        undefined,
        "session-1",
      );
    });
    // The list is re-read so the switch reflects the server's authoritative
    // ordering/state.
    await waitFor(() => {
      expect(vi.mocked(api.getManageableSkills).mock.calls.length).toBeGreaterThanOrEqual(2);
    });
    // The slash list is a discovery surface: a disabled skill must disappear
    // from it (and a re-enabled one reappear) without a page reload.
    await waitFor(() => {
      expect(reloadDynamicCommands).toHaveBeenCalled();
    });
  });

  it("rolls the switch back when the toggle request fails", async () => {
    vi.mocked(api.setSkillEnabled).mockRejectedValueOnce(new Error("nope"));
    renderSidebar();
    await openSkillsSection();

    fireEvent.click(screen.getByLabelText("Skill alpha"));

    await waitFor(() => {
      const after = screen.getByLabelText("Skill alpha") as HTMLInputElement;
      expect(after.checked).toBe(false);
    });
    // A failed toggle must not reload the slash list into a wrong state.
    expect(reloadDynamicCommands).not.toHaveBeenCalled();
  });
});
