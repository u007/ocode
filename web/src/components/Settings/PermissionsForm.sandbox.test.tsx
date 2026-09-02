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
    setAutoPermissionConfig: vi.fn(),
    setPermissionModel: vi.fn(),
    setYolo: vi.fn(),
  },
}));

const mockGetPermissions = vi.mocked(api.getPermissions);
const mockGetAuto = vi.mocked(api.getAutoPermissionConfig);
const mockSetPermissionMode = vi.mocked(api.setPermissionMode);
const mockSetAuto = vi.mocked(api.setAutoPermissionConfig);
const mockSetPermissionModel = vi.mocked(api.setPermissionModel);

const EMPTY_AUTO = {
  enabled: false,
  allow_destructive: false,
  prompt: "",
  max_context_bytes: 0,
  max_context_sources: 0,
  max_context_lines_per_source: 0,
  min_confidence: 0,
};

beforeEach(() => {
  vi.clearAllMocks();
  mockGetPermissions.mockResolvedValue({
    mode: "sandbox",
    auto_allow: false,
    sandbox_supported: true,
    effective_behavior: "confined",
    rules: [],
    bash_rules: [],
  } as never);
  mockGetAuto.mockResolvedValue(EMPTY_AUTO as never);
});

describe("PermissionsForm sandbox preservation", () => {
  it("does not revert sandbox to normal when saved without touching mode", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Permissions");

    const save = screen.getByRole("button", { name: /save/i });
    fireEvent.click(save);

    await waitFor(() => expect(mockSetAuto).toHaveBeenCalled());
    // The mode was sandbox and the user did not change it → setPermissionMode
    // must NOT be called, so the session-scoped sandbox toggle is preserved.
    expect(mockSetPermissionMode).not.toHaveBeenCalled();
  });

  it("calls setPermissionMode('yolo') when the user enables YOLO and saves", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Permissions");

    const yolo = screen.getByLabelText(/Yolo mode/i);
    fireEvent.click(yolo); // toggle to checked (yolo)
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(mockSetPermissionMode).toHaveBeenCalledWith("yolo"));
    expect(mockSetPermissionModel).not.toHaveBeenCalled(); // no model change
  });
});
