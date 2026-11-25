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
    setYolo: vi.fn(),
    getPermissionConcerns: vi.fn(),
    getAllowedHosts: vi.fn(),
    setAllowedHosts: vi.fn(),
    listAutoGrants: vi.fn(),
    revokeAutoGrant: vi.fn(),
  },
}));

const AUTO = {
  enabled: false,
  allow_destructive: false,
  prompt: "",
  max_context_bytes: 0,
  max_context_sources: 0,
  max_context_lines_per_source: 0,
  min_confidence: 0.85,
};

const GRANTS = [
  {
    index: 0,
    kind: "interpreter",
    language: "python",
    source_mode: "exact",
    entrypoint_path: "/home/user/scripts/run.py",
    entrypoint_sha256: "abc123def456",
    normalized_command: "python /home/user/scripts/run.py",
    cwd: "/home/user",
    destructive: false,
  },
  {
    index: 1,
    kind: "interpreter",
    language: "bash",
    source_mode: "exact",
    entrypoint_path: "/tmp/scratchpad.sh",
    entrypoint_sha256: "deadbeefcafe",
    normalized_command: "bash /tmp/scratchpad.sh",
    cwd: "/tmp",
    destructive: true,
  },
];

function setup(grants: typeof GRANTS = GRANTS) {
  vi.clearAllMocks();
  vi.mocked(api.getPermissions).mockResolvedValue({
    mode: "normal",
    auto_allow: false,
    sandbox_supported: true,
    effective_behavior: "confined",
    rules: [],
    bash_rules: [],
  } as never);
  vi.mocked(api.getAutoPermissionConfig).mockResolvedValue(AUTO as never);
  vi.mocked(api.getPermissionModeConfig).mockResolvedValue({ mode: "normal" } as never);
  vi.mocked(api.getPermissionConcerns).mockResolvedValue({ concerns: [] } as never);
  vi.mocked(api.getAllowedHosts).mockResolvedValue({ hosts: [] } as never);
  vi.mocked(api.listAutoGrants).mockResolvedValue({ grants } as never);
  vi.mocked(api.revokeAutoGrant).mockResolvedValue({ revoked: true, index: 0 } as never);
  vi.mocked(api.setAutoPermissionConfig).mockResolvedValue({} as never);
}

describe("PermissionsForm whitelisted scripts", () => {
  beforeEach(() => setup());

  it("renders grants with language, path, sha prefix, cwd, destructive", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Whitelisted scripts");

    expect(screen.getByText("python")).toBeTruthy();
    expect(screen.getByText("/home/user/scripts/run.py")).toBeTruthy();
    expect(screen.getByText(/sha256 abc123def456/)).toBeTruthy();
    expect(screen.getByText(/cwd \/home\/user/)).toBeTruthy();

    expect(screen.getByText("bash")).toBeTruthy();
    expect(screen.getByText("/tmp/scratchpad.sh")).toBeTruthy();
    expect(screen.getByText("destructive")).toBeTruthy();
  });

  it("shows a /tmp warning for paths under /tmp", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Whitelisted scripts");

    const warnings = screen.getAllByTestId("tmp-warning");
    expect(warnings.length).toBe(1);
  });

  it("revokes a grant and refreshes the list", async () => {
    const mockList = vi.mocked(api.listAutoGrants);
    mockList
      .mockResolvedValueOnce({ grants: GRANTS } as never)
      .mockResolvedValueOnce({ grants: [GRANTS[0]] } as never);

    render(<PermissionsForm />);
    await screen.findByText("Whitelisted scripts");

    const btn = screen.getByLabelText("Revoke grant 1");
    fireEvent.click(btn);

    await waitFor(() => expect(vi.mocked(api.revokeAutoGrant)).toHaveBeenCalledWith(1));
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByText("/tmp/scratchpad.sh")).toBeNull());
  });

  it("shows loading state", async () => {
    vi.mocked(api.listAutoGrants).mockImplementation(
      () => new Promise(() => {}) as never,
    );
    render(<PermissionsForm />);
    await screen.findByText("Whitelisted scripts");
    expect(screen.getByText("Loading…")).toBeTruthy();
  });

  it("shows error state on fetch failure", async () => {
    vi.mocked(api.listAutoGrants).mockRejectedValue(new Error("boom"));
    render(<PermissionsForm />);
    await screen.findByText("Whitelisted scripts");
    await waitFor(() => expect(screen.getByText("boom")).toBeTruthy());
  });
});
