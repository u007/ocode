import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import PermissionsForm from "./PermissionsForm";
import { api } from "../../api/client";

vi.mock("../../stores/chatStore", () => ({
  ChatStoreProvider: ({ children }: { children: React.ReactNode }) => children,
  useChatState: () => ({}),
  useChatDispatch: () => vi.fn(),
  useChatSelector: (_sel: unknown) => undefined,
}));

vi.mock("../../api/client", () => ({
  api: {
    getPermissions: vi.fn(),
    getAutoPermissionConfig: vi.fn(),
    setPermissionMode: vi.fn(),
    getPermissionModeConfig: vi.fn(),
    setPermissionModeConfig: vi.fn(),
    setAutoPermissionConfig: vi.fn(),
    setPermissionModel: vi.fn(),
    setBashRules: vi.fn(),
    setYolo: vi.fn(),
    getPermissionConcerns: vi.fn(),
  },
}));

const mockGetPermissions = vi.mocked(api.getPermissions);
const mockGetAuto = vi.mocked(api.getAutoPermissionConfig);
const mockGetPermissionModeConfig = vi.mocked(api.getPermissionModeConfig);
const mockGetConcerns = vi.mocked(api.getPermissionConcerns);
const mockSetBashRules = vi.mocked(api.setBashRules);

const EMPTY_AUTO = {
  enabled: false,
  allow_destructive: false,
  prompt: "",
  max_context_bytes: 0,
  max_context_sources: 0,
  max_context_lines_per_source: 0,
  min_confidence: 0,
  relaxed_concerns: [],
};

function seed(bashRules: { tool: string; level: string }[] = [], mode = "normal") {
  mockGetPermissions.mockResolvedValue({
    mode,
    auto_allow: false,
    sandbox_supported: true,
    effective_behavior: "confined",
    rules: [],
    bash_rules: bashRules,
  } as never);
  mockGetAuto.mockResolvedValue(EMPTY_AUTO as never);
  mockGetPermissionModeConfig.mockResolvedValue({ mode } as never);
  mockGetConcerns.mockResolvedValue({ concerns: [] } as never);
  mockSetBashRules.mockResolvedValue({ bash_rules: [] } as never);
}

const save = () => fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

beforeEach(() => {
  vi.clearAllMocks();
  seed();
});

describe("PermissionsForm bash command rules", () => {
  it("lists the server's rules with their level", async () => {
    seed([
      { tool: "git push", level: "deny" },
      { tool: "git status", level: "allow" },
    ]);
    render(<PermissionsForm />);

    expect(await screen.findByLabelText("Rule prefix git push")).toHaveValue("git push");
    expect(screen.getByLabelText("Level for git push")).toHaveValue("deny");
    expect(screen.getByLabelText("Level for git status")).toHaveValue("allow");
  });

  it("does not call the API when nothing was staged", async () => {
    seed([{ tool: "git push", level: "deny" }]);
    render(<PermissionsForm />);
    await screen.findByLabelText("Rule prefix git push");

    save();

    await waitFor(() => expect(api.setAutoPermissionConfig).toHaveBeenCalled());
    expect(mockSetBashRules).not.toHaveBeenCalled();
  });

  it("stages edits and applies them with one delta on Save", async () => {
    seed([
      { tool: "git push", level: "deny" },
      { tool: "sed", level: "ask" },
    ]);
    render(<PermissionsForm />);
    await screen.findByLabelText("Rule prefix git push");

    // Change one level, remove one row, add one row.
    fireEvent.change(screen.getByLabelText("Level for git push"), { target: { value: "ask" } });
    fireEvent.click(screen.getByRole("button", { name: "Remove rule sed" }));
    fireEvent.change(screen.getByLabelText("New rule prefix"), { target: { value: "git push --dry-run" } });
    fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

    expect(screen.getByTestId("bash-rules-pending")).toHaveTextContent("3 unsaved changes");
    expect(mockSetBashRules).not.toHaveBeenCalled();

    save();

    await waitFor(() =>
      expect(mockSetBashRules).toHaveBeenCalledWith({
        set: { "git push": "ask", "git push --dry-run": "deny" },
        remove: ["sed"],
      }),
    );
  });

  it("sends nothing once an edit is reverted", async () => {
    seed([{ tool: "git push", level: "deny" }]);
    render(<PermissionsForm />);
    await screen.findByLabelText("Rule prefix git push");

    fireEvent.change(screen.getByLabelText("Level for git push"), { target: { value: "ask" } });
    expect(screen.getByTestId("bash-rules-pending")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Level for git push"), { target: { value: "deny" } });

    expect(screen.queryByTestId("bash-rules-pending")).not.toBeInTheDocument();
    save();
    await waitFor(() => expect(api.setAutoPermissionConfig).toHaveBeenCalled());
    expect(mockSetBashRules).not.toHaveBeenCalled();
  });

  it("keeps the staged rows and surfaces the error when the save fails", async () => {
    seed([{ tool: "git push", level: "deny" }]);
    mockSetBashRules.mockRejectedValue(new Error("rule store is read-only"));
    render(<PermissionsForm />);
    await screen.findByLabelText("Rule prefix git push");

    fireEvent.change(screen.getByLabelText("Level for git push"), { target: { value: "ask" } });
    save();

    expect(await screen.findByText("rule store is read-only")).toBeInTheDocument();
    // Still staged — the user can fix the problem and press Save again.
    expect(screen.getByTestId("bash-rules-pending")).toBeInTheDocument();
    expect(screen.getByLabelText("Level for git push")).toHaveValue("ask");
  });

  it("adopts the server's rule list from the save response", async () => {
    seed([{ tool: "git push", level: "deny" }]);
    mockSetBashRules.mockResolvedValue({
      bash_rules: [{ tool: "git push", level: "ask" }],
    } as never);
    render(<PermissionsForm />);
    await screen.findByLabelText("Rule prefix git push");

    fireEvent.change(screen.getByLabelText("Level for git push"), { target: { value: "ask" } });
    save();

    await waitFor(() => expect(mockSetBashRules).toHaveBeenCalled());
    // The pending badge clears and the row shows what the server stored, not
    // what was typed — a second Save must then send nothing.
    await waitFor(() =>
      expect(screen.queryByTestId("bash-rules-pending")).not.toBeInTheDocument(),
    );
    save();
    await waitFor(() => expect(api.setAutoPermissionConfig).toHaveBeenCalled());
    expect(mockSetBashRules).toHaveBeenCalledTimes(1);
  });

  it("refuses a blanket git allow before it reaches the API", async () => {
    render(<PermissionsForm />);
    await screen.findByText(/Bash command rules/);

    fireEvent.change(screen.getByLabelText("New rule prefix"), { target: { value: "git" } });
    fireEvent.change(screen.getByLabelText("New rule level"), { target: { value: "allow" } });
    fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/cannot be always-allowed/);
    expect(mockSetBashRules).not.toHaveBeenCalled();
  });

  it("adds a rule with the Enter key in the prefix field", async () => {
    render(<PermissionsForm />);
    await screen.findByText(/Bash command rules/);

    const field = screen.getByLabelText("New rule prefix");
    fireEvent.change(field, { target: { value: "curl" } });
    fireEvent.keyDown(field, { key: "Enter" });

    expect(await screen.findByLabelText("Rule prefix curl")).toBeInTheDocument();
    expect(field).toHaveValue("");
  });

  it("warns that yolo bypasses these rules when it is the startup default", async () => {
    seed([], "yolo");
    render(<PermissionsForm />);

    expect(await screen.findByTestId("bash-rules-yolo-warning")).toBeInTheDocument();
  });

  it("shows no yolo warning for the normal default", async () => {
    render(<PermissionsForm />);
    await screen.findByText(/Bash command rules/);

    expect(screen.queryByTestId("bash-rules-yolo-warning")).not.toBeInTheDocument();
  });
});

// Keying a row by its prefix would remount the input on the first keystroke
// (the prefix IS the editable value), which silently throws away focus
// mid-typing. This drives real focus so the regression is observable.
describe("PermissionsForm bash rule prefix editing keeps focus", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    seed([{ tool: "git push", level: "deny" }]);
  });

  it("keeps the focused prefix input focused across edits", async () => {
    render(<PermissionsForm />);
    const field = (await screen.findByLabelText("Rule prefix git push")) as HTMLInputElement;
    field.focus();
    expect(document.activeElement).toBe(field);

    fireEvent.change(field, { target: { value: "git pushes" } });
    expect(document.activeElement).not.toBe(document.body);
    // The same DOM node is still the one being edited.
    expect(document.activeElement).toBe(field);
    expect((document.activeElement as HTMLInputElement).value).toBe("git pushes");
  });

  it("surfaces the rename as one remove plus one set on Save", async () => {
    render(<PermissionsForm />);
    const field = await screen.findByLabelText("Rule prefix git push");
    fireEvent.change(field, { target: { value: "git push origin" } });

    save();

    await waitFor(() =>
      expect(mockSetBashRules).toHaveBeenCalledWith({
        set: { "git push origin": "deny" },
        remove: ["git push"],
      }),
    );
  });
});

describe("PermissionsForm bash rule prefix typing", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // The input is edited with fireEvent.change per keystroke, so a per-keystroke
  // trim() means the trailing space is gone by the time the next character is
  // appended. "git push" became "gitpush": a rule that matches nothing.
  it("keeps the space typed inside a prefix", async () => {
    seed([{ tool: "npm", level: "ask" }]);
    render(<PermissionsForm />);
    const field = (await screen.findByLabelText("Rule prefix npm")) as HTMLInputElement;

    for (const value of ["g", "gi", "git", "git ", "git p", "git pu", "git push"]) {
      fireEvent.change(field, { target: { value } });
    }

    expect(field).toHaveValue("git push");
    save();

    // The set key carries the space. With a per-keystroke trim this was
    // "gitpush", a prefix that matches no command at all.
    await waitFor(() =>
      expect(mockSetBashRules).toHaveBeenCalledWith({
        set: { "git push": "ask" },
        remove: ["npm"],
      }),
    );
  });

  // Typing a trailing space is only legal because the raw value is kept; the
  // normalized key is still what reaches the API.
  it("normalizes the key it sends while keeping what the user typed", async () => {
    seed([{ tool: "npm", level: "ask" }]);
    render(<PermissionsForm />);
    const field = (await screen.findByLabelText("Rule prefix npm")) as HTMLInputElement;

    fireEvent.change(field, { target: { value: "  git   push  " } });

    expect(field).toHaveValue("  git   push  ");
    save();

    await waitFor(() =>
      expect(mockSetBashRules).toHaveBeenCalledWith({
        set: { "git push": "ask" },
        remove: ["npm"],
      }),
    );
  });

  // Backspacing "git push" down to "git" walks through a prefix another row
  // already holds. Collapsing them mid-typing dropped the untouched row from the
  // staged list, and Save then emitted remove:["git"] — deleting a saved rule
  // the user never edited.
  it("never deletes a colliding row while a rename passes through its prefix", async () => {
    seed([
      { tool: "git", level: "deny" },
      { tool: "git push", level: "allow" },
    ]);
    render(<PermissionsForm />);
    const field = await screen.findByLabelText("Rule prefix git push");

    for (const value of ["git pus", "git pu", "git p", "git ", "git"]) {
      fireEvent.change(field, { target: { value } });
    }

    // Both rows are still on screen — the untouched "git" row with its level.
    // Both now share the prefix, so the labels match two elements.
    expect(screen.getAllByLabelText("Rule prefix git")).toHaveLength(2);
    // The row that was never touched kept its level. Under the old per-keystroke
    // dedupe this row was replaced by the edited one and vanished from the list.
    expect(screen.getAllByLabelText("Level for git").map((el) => (el as HTMLSelectElement).value)).toEqual([
      "deny",
      "allow",
    ]);

    // ...and the collision is reported rather than resolved behind the user's back.
    await waitFor(() =>
      expect(screen.getAllByText(/another row has this prefix/i).length).toBeGreaterThan(0),
    );

    save();

    await waitFor(() =>
      expect(mockSetBashRules).toHaveBeenCalledWith({
        set: { "git": "allow" },
        remove: ["git push"],
      }),
    );
    // The load-bearing assertion: the unrelated "git" rule is never removed.
    const delta = mockSetBashRules.mock.calls[0][0];
    expect(delta.remove).not.toContain("git");
  });

  it("clears the duplicate warning once the collision is resolved", async () => {
    seed([
      { tool: "git", level: "deny" },
      { tool: "git push", level: "allow" },
    ]);
    render(<PermissionsForm />);
    const field = await screen.findByLabelText("Rule prefix git push");

    fireEvent.change(field, { target: { value: "git" } });
    await waitFor(() =>
      expect(screen.getAllByText(/another row has this prefix/i).length).toBeGreaterThan(0),
    );

    fireEvent.change(field, { target: { value: "git pusher" } });
    await waitFor(() =>
      expect(screen.queryByText(/another row has this prefix/i)).toBeNull(),
    );
  });
});
