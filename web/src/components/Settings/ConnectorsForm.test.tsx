import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { api, type ConnectProvider } from "../../api/client";
import ConnectorsForm from "./ConnectorsForm";

// The Connectors form is the web/desktop equivalent of the TUI /connect dialog.
// What these tests protect, in order of blast radius:
//  1. Credentials are per machine, so every call carries `host`.
//  2. Remove is destructive AND the mask is the only credential feedback we
//     render — a failed remove that reports success silently re-connects a
//     provider the user believes they disconnected.
//  3. The catalog is ~40 providers, so the filter is load-bearing, not polish.
//  4. `status` from the server is an opaque symbol string; it is rendered, but
//     a provider with a stored key must never be shown as unconnected.

const PROVIDERS = [
  {
    id: "anthropic",
    label: "Anthropic",
    status: "✓",
    statusDetail: "key from auth.json",
    hasCredential: true,
    kind: "api_key",
    masked: "sk-a••••b123",
    methods: [
      { id: "apikey", label: "API key", kind: "apikey" as const },
      { id: "oauth_console", label: "Claude account (console)", kind: "oauth" as const },
    ],
  },
  {
    id: "openai",
    label: "OpenAI",
    status: "✗",
    statusDetail: "no credential",
    hasCredential: false,
    methods: [{ id: "apikey", label: "API key", kind: "apikey" as const }],
  },
];

// Typed as the real api methods (not bare `vi.fn`), so a signature change on
// either side is a compile error here rather than a silently-wrong mock.
let listProviders: Mock<typeof api.listConnectProviders>;
let setCredential: Mock<typeof api.setConnectCredential>;
let removeCredential: Mock<typeof api.removeConnectCredential>;
let testCredential: Mock<typeof api.testConnectCredential>;

beforeEach(() => {
  listProviders = vi.fn(async () => ({ providers: PROVIDERS as ConnectProvider[] }));
  setCredential = vi.fn(async () => ({ ok: true, provider: PROVIDERS[0] as ConnectProvider }));
  removeCredential = vi.fn(async () => ({
    ok: true,
    provider: { ...PROVIDERS[1], hasCredential: false } as ConnectProvider,
  }));
  testCredential = vi.fn(async () => ({ ok: true }));
  vi.spyOn(api, "listConnectProviders").mockImplementation(listProviders);
  vi.spyOn(api, "setConnectCredential").mockImplementation(setCredential);
  vi.spyOn(api, "removeConnectCredential").mockImplementation(removeCredential);
  vi.spyOn(api, "testConnectCredential").mockImplementation(testCredential);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

/** Provider rows are keyed by a stable test id so we never assert on labels. */
function rowFor(label: string): HTMLElement {
  return screen.getByTestId(`connector-${label}`);
}

describe("ConnectorsForm — listing and status", () => {
  it("renders every provider with its status and masked credential", async () => {
    render(<ConnectorsForm host="host-a" />);

    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    const anthropic = rowFor("anthropic");
    expect(within(anthropic).getByText("Anthropic")).toBeTruthy();
    expect(within(anthropic).getByText("sk-a••••b123")).toBeTruthy();

    // A connected provider must never read as unconnected just because the
    // server sent a status symbol we render without interpreting.
    expect(within(anthropic).getByTestId("connector-status-anthropic").textContent).toContain("✓");
    expect(within(rowFor("openai")).getByTestId("connector-status-openai").textContent).toContain("✗");
  });

  it("filters the catalog without dropping the provider being typed for", async () => {
    render(<ConnectorsForm host="host-a" />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());
    expect(screen.getByTestId("connector-openai")).toBeTruthy();

    fireEvent.change(screen.getByPlaceholderText(/filter/i), { target: { value: "anthro" } });

    expect(screen.getByTestId("connector-anthropic")).toBeTruthy();
    expect(screen.queryByTestId("connector-openai")).toBeNull();
  });

  it("surfaces a load failure instead of rendering an empty provider list", async () => {
    listProviders.mockRejectedValueOnce(new Error("boom"));
    render(<ConnectorsForm host="host-a" />);

    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    // The dangerous failure mode is a silent empty list, which reads as "no
    // providers configured" rather than "the request failed".
    expect(screen.queryByTestId("connector-anthropic")).toBeNull();
  });
});

describe("ConnectorsForm — saving an API key", () => {
  it("PUTs the typed key with the host threaded, then refreshes the list", async () => {
    render(<ConnectorsForm host="host-a" />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    fireEvent.click(within(rowFor("openai")).getByRole("button", { name: /connect/i }));
    fireEvent.change(screen.getByLabelText(/api key/i), { target: { value: "sk-test-999" } });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(setCredential).toHaveBeenCalled());
    expect(setCredential).toHaveBeenCalledWith("openai", { apiKey: "sk-test-999" }, "host-a");
    // The masked value is server-derived, so a local optimistic update would
    // show a mask the server never produced.
    await waitFor(() => expect(listProviders.mock.calls.length).toBeGreaterThan(1));
  });

  it("keeps the save error visible and does not close the form", async () => {
    setCredential.mockRejectedValueOnce(new Error("provider rejected key"));
    render(<ConnectorsForm host="host-a" />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    fireEvent.click(within(rowFor("openai")).getByRole("button", { name: /connect/i }));
    fireEvent.change(screen.getByLabelText(/api key/i), { target: { value: "bad" } });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(screen.getByText(/provider rejected key/i)).toBeTruthy());
    // Still expanded: a closed form reads as "saved".
    expect(screen.getByLabelText(/api key/i)).toBeTruthy();
  });

  // A pasted or typed key is a live secret: a plain-text field puts it on
  // screen, in a screenshot, and in a screen-share. The TUI /connect dialog
  // already uses password echo for the same field (internal/tui/connect.go),
  // and every other key field in Settings does too (ProfilesManager,
  // SecurityForm, VaultForm) — this one was the outlier.
  it("masks the API key field and opts it out of autofill", async () => {
    render(<ConnectorsForm host="host-a" />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    fireEvent.click(within(rowFor("openai")).getByRole("button", { name: /connect/i }));

    const field = screen.getByLabelText(/api key/i) as HTMLInputElement;
    expect(field.type).toBe("password");
    expect(field.getAttribute("autocomplete")).toBe("off");
    // Masking must not break typing or saving.
    fireEvent.change(field, { target: { value: "sk-live-123" } });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
    await waitFor(() => expect(setCredential).toHaveBeenCalledWith("openai", { apiKey: "sk-live-123" }, "host-a"));
  });
});

describe("ConnectorsForm — remove is gated behind a rendered confirm", () => {
  it("does not call the API when the confirm is cancelled", async () => {
    render(<ConnectorsForm host="host-a" />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    fireEvent.click(within(rowFor("anthropic")).getByRole("button", { name: /remove/i }));

    // The dialog must actually appear: native confirm() silently returns false
    // in the Wails webview, which would make remove unreachable there.
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /cancel/i }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(removeCredential).not.toHaveBeenCalled();
  });

  it("removes with the host threaded once confirmed", async () => {
    render(<ConnectorsForm host="host-a" />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    fireEvent.click(within(rowFor("anthropic")).getByRole("button", { name: /remove/i }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /^remove/i }));

    await waitFor(() => expect(removeCredential).toHaveBeenCalledWith("anthropic", "host-a"));
    await waitFor(() => expect(listProviders.mock.calls.length).toBeGreaterThan(1));
  });

  it("does not remove when the confirm action itself fails", async () => {
    removeCredential.mockRejectedValueOnce(new Error("auth.json is read-only"));
    render(<ConnectorsForm host="host-a" />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    fireEvent.click(within(rowFor("anthropic")).getByRole("button", { name: /remove/i }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /^remove/i }));

    // ConfirmDialog renders a rejected onConfirm inline and stays open, so the
    // provider must still read as connected after the failure.
    await waitFor(() => expect(within(dialog).getByText(/read-only/i)).toBeTruthy());
    expect(rowFor("anthropic")).toBeTruthy();
  });
});

describe("ConnectorsForm — testing a stored credential", () => {
  it("surfaces a failed test with the server's own error text", async () => {
    testCredential.mockResolvedValueOnce({ ok: false, error: "401 invalid key" });
    render(<ConnectorsForm host="host-a" />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    fireEvent.click(within(rowFor("anthropic")).getByRole("button", { name: /^test$/i }));

    await waitFor(() => expect(screen.getByText(/401 invalid key/i)).toBeTruthy());
  });

  it("passes the host when testing", async () => {
    render(<ConnectorsForm host="host-a" />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    fireEvent.click(within(rowFor("anthropic")).getByRole("button", { name: /^test$/i }));

    await waitFor(() => expect(testCredential).toHaveBeenCalledWith("anthropic", "host-a"));
  });
});