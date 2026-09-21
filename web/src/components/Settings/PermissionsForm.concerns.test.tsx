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
  },
}));

const mockGetAuto = vi.mocked(api.getAutoPermissionConfig);
const mockSetAuto = vi.mocked(api.setAutoPermissionConfig);
const mockGetConcerns = vi.mocked(api.getPermissionConcerns);
const mockSetPermissionModel = vi.mocked(api.setPermissionModel);

// Mirrors agent.RelaxableConcerns(): the Go catalog minus "none", with a note on
// the partly-gated categories.
const CATALOG = [
  { key: "outside_allowed_roots", label: "reads, writes, or deletes a path outside the allowed roots", note: "Non-interpreter asks still refuse an out-of-scope target." },
  { key: "destructive", label: "destroys existing data or repository state", note: "Hard-blocked forms never reach the judge." },
  { key: "secrets", label: "exposes a secret or credential value", note: "Reading a credential file locally is already allowed." },
  { key: "banned_prefix", label: "invokes a banned command prefix", note: "A hard-blocked ban always wins." },
  { key: "network", label: "opens outbound network connections", note: "Relaxes outbound hosts and network-capable subprocesses." },
  { key: "subprocess_or_dynamic_code", label: "spawns subprocesses or evaluates dynamic code", note: "Hard-blocked or harmful subprocesses are still refused." },
  { key: "system_or_git_history", label: "modifies system configuration, git history, or force-pushes", note: "Relaxes what reached the judge." },
  { key: "truncated_or_unknown", label: "the source is truncated, unavailable, or the effect cannot be determined", note: "Allows a call even when the judge cannot tell what it does." },
];

const AUTO = {
  enabled: true,
  allow_destructive: false,
  prompt: "",
  max_context_bytes: 0,
  max_context_sources: 0,
  max_context_lines_per_source: 0,
  min_confidence: 0.85,
};

function setup(auto: Record<string, unknown> = AUTO) {
  vi.clearAllMocks();
  vi.mocked(api.getPermissions).mockResolvedValue({
    mode: "sandbox",
    auto_allow: true,
    sandbox_supported: true,
    effective_behavior: "confined",
    rules: [],
    bash_rules: [],
  } as never);
  mockGetAuto.mockResolvedValue({ ...AUTO, ...auto } as never);
  vi.mocked(api.getPermissionModeConfig).mockResolvedValue({ mode: "normal" } as never);
  mockGetConcerns.mockResolvedValue({ concerns: CATALOG } as never);
  mockSetAuto.mockResolvedValue({} as never);
}

const box = (key: string) => screen.getByLabelText(`Enforce ${key}`) as HTMLInputElement;

describe("PermissionsForm judge enforcement categories", () => {
  beforeEach(() => setup());

  it("renders one checkbox per server category, all enforced by default", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Categories the judge must enforce");

    // The section owns exactly the catalog's checkboxes (the form has two
    // unrelated ones — auto-approve and allow-destructive).
    expect(screen.getAllByLabelText(/^Enforce /)).toHaveLength(CATALOG.length);
    for (const c of CATALOG) {
      expect(box(c.key).checked).toBe(true);
    }
    // The partly-gated categories show their caveat so the switch never
    // overstates what it can do.
    expect(screen.getByText(/Reading a credential file locally is already allowed/)).toBeTruthy();
  });

  it("saves the unticked category, not the ticked ones", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Categories the judge must enforce");

    fireEvent.click(box("secrets"));
    expect(box("secrets").checked).toBe(false);

    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(mockSetAuto).toHaveBeenCalledTimes(1));
    const sent = mockSetAuto.mock.calls[0][0] as { relaxed_concerns?: string[] };
    expect(sent.relaxed_concerns).toEqual(["secrets"]);
  });

  it("clears relaxed categories when a config value is re-ticked", async () => {
    setup({ ...AUTO, relaxed_concerns: ["secrets", "network"] });
    render(<PermissionsForm />);
    await screen.findByText("Categories the judge must enforce");

    // A stored opt-out renders as an UNticked box.
    expect(box("secrets").checked).toBe(false);
    expect(box("network").checked).toBe(false);
    expect(box("destructive").checked).toBe(true);

    fireEvent.click(box("secrets"));
    fireEvent.click(box("network"));
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(mockSetAuto).toHaveBeenCalledTimes(1));
    const sent = mockSetAuto.mock.calls[0][0] as { relaxed_concerns?: string[] };
    expect(sent.relaxed_concerns).toEqual([]);
    // The model still travels on its own endpoint — the concern write must not
    // start clobbering it.
    expect(mockSetPermissionModel).not.toHaveBeenCalled();
  });

  it("None then All round-trips through the catalog", async () => {
    render(<PermissionsForm />);
    await screen.findByText("Categories the judge must enforce");

    fireEvent.click(screen.getByText("None"));
    for (const c of CATALOG) {
      expect(box(c.key).checked).toBe(false);
    }
    fireEvent.click(screen.getByText("All"));
    for (const c of CATALOG) {
      expect(box(c.key).checked).toBe(true);
    }

    fireEvent.click(screen.getByText("Save"));
    await waitFor(() => expect(mockSetAuto).toHaveBeenCalledTimes(1));
    const sent = mockSetAuto.mock.calls[0][0] as { relaxed_concerns?: string[] };
    expect(sent.relaxed_concerns).toEqual([]);
  });
});
