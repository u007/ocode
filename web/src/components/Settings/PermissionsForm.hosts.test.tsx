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
  },
}));

const mockGetHosts = vi.mocked(api.getAllowedHosts);
const mockSetHosts = vi.mocked(api.setAllowedHosts);

const AUTO = {
  enabled: false,
  allow_destructive: false,
  prompt: "",
  max_context_bytes: 0,
  max_context_sources: 0,
  max_context_lines_per_source: 0,
  min_confidence: 0.85,
};

function setup(hosts: string[]) {
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
  mockGetHosts.mockResolvedValue({ hosts } as never);
  mockSetHosts.mockImplementation(async (next: string[]) => ({ hosts: next }) as never);
  vi.mocked(api.setAutoPermissionConfig).mockResolvedValue({} as never);
}

describe("PermissionsForm allowed hosts", () => {
  beforeEach(() => setup(["api.example.com", "*.docs.example.org"]));

  it("renders the loaded hosts as editable rows", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Allowed hosts");

    expect(screen.getByLabelText("Allowed host api.example.com")).toBeTruthy();
    expect(screen.getByLabelText("Allowed host *.docs.example.org")).toBeTruthy();
    expect(screen.getByText(/the outbound-request judge still sees them/)).toBeTruthy();
  });

  it("adding a host and clicking Save sends the new list", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Allowed hosts");

    const input = screen.getByLabelText("New allowed host");
    fireEvent.change(input, { target: { value: "  staging.example.net " } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByLabelText("Allowed host staging.example.net")).toBeTruthy();

    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(mockSetHosts).toHaveBeenCalledTimes(1));
    expect(mockSetHosts).toHaveBeenCalledWith([
      "api.example.com",
      "*.docs.example.org",
      "staging.example.net",
    ]);
  });

  it("saves a host still typed in the add row when Save is clicked without Enter", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Allowed hosts");

    const input = screen.getByLabelText("New allowed host");
    fireEvent.change(input, { target: { value: "  staging.example.net " } });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(mockSetHosts).toHaveBeenCalledTimes(1));
    expect(mockSetHosts).toHaveBeenCalledWith([
      "api.example.com",
      "*.docs.example.org",
      "staging.example.net",
    ]);
    await waitFor(() => expect(input).toHaveValue(""));
  });

  it("does not repeat a typed host that is already in the list", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Allowed hosts");

    fireEvent.change(screen.getByLabelText("New allowed host"), {
      target: { value: "api.example.com " },
    });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(api.setAutoPermissionConfig).toHaveBeenCalledTimes(1));
    expect(mockSetHosts).not.toHaveBeenCalled();
  });

  it("removing a host and clicking Save sends the shorter list", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Allowed hosts");

    fireEvent.click(screen.getByLabelText("Remove host api.example.com"));
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(mockSetHosts).toHaveBeenCalledTimes(1));
    expect(mockSetHosts).toHaveBeenCalledWith(["*.docs.example.org"]);
  });

  it("does not call setAllowedHosts when nothing changed", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Allowed hosts");

    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(api.setAutoPermissionConfig).toHaveBeenCalledTimes(1));
    expect(mockSetHosts).not.toHaveBeenCalled();
  });
});
