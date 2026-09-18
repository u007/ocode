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
  },
}));

const mockGetPermissions = vi.mocked(api.getPermissions);
const mockGetAuto = vi.mocked(api.getAutoPermissionConfig);
const mockSetPermissionMode = vi.mocked(api.setPermissionMode);
const mockGetPermissionModeConfig = vi.mocked(api.getPermissionModeConfig);
const mockSetPermissionModeConfig = vi.mocked(api.setPermissionModeConfig);
const mockSetAuto = vi.mocked(api.setAutoPermissionConfig);

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
  mockGetPermissionModeConfig.mockResolvedValue({ mode: "normal" } as never);
});

describe("PermissionsForm is process-wide settings only", () => {
  it("never writes a session-less live permission mode on save", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Permissions");

    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(mockSetAuto).toHaveBeenCalled());
    // The live mode is per chat session and is edited from the sidebar/commands,
    // never from this process-wide form (a session-less write would leak).
    expect(mockSetPermissionMode).not.toHaveBeenCalled();
  });

  it("does not offer a live YOLO checkbox anymore", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Permissions");
    expect(screen.queryByLabelText(/Yolo mode/i)).toBeNull();
  });

  it("does not persist the default mode when the user did not change it", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Permissions");

    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(mockSetAuto).toHaveBeenCalled());
    expect(mockSetPermissionModeConfig).not.toHaveBeenCalled();
  });

  it("calls setPermissionModeConfig('sandbox') when the user picks Sandbox as the default and saves", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Permissions");

    fireEvent.click(screen.getByLabelText(/^Sandbox$/i));
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(mockSetPermissionModeConfig).toHaveBeenCalledWith("sandbox"));
  });
});
