import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { api, type ConnectProvider } from "../../api/client";
import ConnectorsForm from "./ConnectorsForm";

/**
 * Settings → Connectors is the only place a web/desktop user can start an
 * OAuth flow at all, so these tests pin the WIRING rather than either component
 * in isolation: ConnectFlowPanel was fully implemented, fully tested, and
 * reachable from nothing. A green panel suite said nothing about whether a user
 * could ever get to it.
 *
 * What is load-bearing:
 *  - `host` reaches the flow start. Credentials are per machine, so a flow
 *    started without it runs against the wrong auth.json — and for a remote host
 *    the panel defaults to manual mode *because* host is set, so dropping it
 *    silently breaks the second-device case too.
 *  - A provider offering more than one way in (Anthropic: API key or OAuth)
 *    must show a chooser; a provider with one way must not gain a pointless one.
 *  - The API-key form stays the default selection, because "Connect" meant that
 *    before OAuth was reachable here.
 */

const PROVIDERS = [
  {
    id: "anthropic",
    label: "Anthropic",
    status: "✗",
    statusDetail: "no credential",
    hasCredential: false,
    methods: [
      { id: "apikey", label: "API key", kind: "apikey" as const },
      { id: "oauth_console", label: "Anthropic Console", kind: "oauth" as const },
      { id: "remove", label: "Remove stored credential", kind: "remove" as const },
    ],
  },
  {
    id: "openai",
    label: "OpenAI",
    status: "✗",
    statusDetail: "no credential",
    hasCredential: false,
    methods: [
      {
        id: "oauth",
        label: "ChatGPT subscription",
        kind: "oauth" as const,
        modes: ["auto", "manual"] as ("auto" | "manual")[],
      },
    ],
  },
] as unknown as ConnectProvider[];

let listProviders: Mock<typeof api.listConnectProviders>;
let setCredential: Mock<typeof api.setConnectCredential>;
let startFlow: Mock<typeof api.startConnectFlow>;

beforeEach(() => {
  listProviders = vi.fn(async () => ({ providers: PROVIDERS }));
  setCredential = vi.fn(async () => ({ ok: true, provider: PROVIDERS[0] }));
  startFlow = vi.fn(async () => ({
    flowId: "f1",
    kind: "local-callback",
    state: "waiting_input",
  })) as never;
  vi.spyOn(api, "listConnectProviders").mockImplementation(listProviders);
  vi.spyOn(api, "setConnectCredential").mockImplementation(setCredential);
  api.startConnectFlow = startFlow;
  api.getConnectFlow = vi.fn(async () => ({
    flowId: "f1",
    kind: "local-callback",
    state: "waiting_input",
  })) as never;
  api.submitConnectFlowInput = vi.fn(async () => ({
    flowId: "f1",
    kind: "local-callback",
    state: "complete",
  })) as never;
  api.cancelConnectFlow = vi.fn(async () => ({
    flowId: "f1",
    kind: "local-callback",
    state: "cancelled",
  })) as never;
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function rowFor(id: string): HTMLElement {
  return screen.getByTestId(`connector-${id}`);
}



describe("ConnectorsForm — OAuth flows", () => {
  it("starts a flow with the host threaded through to the start call", async () => {
    render(<ConnectorsForm host="james@box" />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    const row = rowFor("openai");
    fireEvent.click(within(row).getByRole("button", { name: /connect/i }));
    fireEvent.click(screen.getByRole("button", { name: /sign in with chatgpt subscription/i }));

    await waitFor(() => expect(startFlow).toHaveBeenCalled());
    // A remote host defaults to manual mode, and that default is derived FROM
    // the host — so an unthreaded host silently produces the broken auto flow.
    expect(startFlow).toHaveBeenCalledWith("openai", "oauth", "james@box", "manual");
  });

  it("offers a method chooser only when the provider has more than one way in", async () => {
    render(<ConnectorsForm />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    // Single-method provider: no chooser, straight to the flow panel.
    const openai = rowFor("openai");
    fireEvent.click(within(openai).getByRole("button", { name: /connect/i }));
    expect(within(openai).queryByRole("group", { name: /sign-in method/i })).toBeNull();

    // Multi-method provider: a chooser, defaulting to the API-key form so
    // "Connect" keeps meaning what it meant before OAuth was reachable.
    const anthropic = rowFor("anthropic");
    fireEvent.click(within(anthropic).getByRole("button", { name: /connect/i }));
    const chooser = within(anthropic).getByRole("group", { name: /sign-in method/i });
    expect(within(chooser).getByRole("button", { name: /api key/i })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByLabelText(/api key/i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /sign in with anthropic console/i })).toBeNull();

    fireEvent.click(within(chooser).getByRole("button", { name: /anthropic console/i }));

    // The key form is replaced, not stacked under the flow panel.
    expect(screen.queryByLabelText(/api key/i)).toBeNull();
    expect(screen.getByRole("button", { name: /sign in with anthropic console/i })).toBeTruthy();
  });

  it("keeps the destructive remove method out of the chooser", async () => {
    // Remove already has its own confirm-gated button on the row. A second
    // entry point for it in the chooser would bypass nothing and confuse a lot:
    // the chooser means "connect this way", and removing is not a way in.
    render(<ConnectorsForm />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    const row = rowFor("anthropic");
    fireEvent.click(within(row).getByRole("button", { name: /connect/i }));
    const chooser = within(row).getByRole("group", { name: /sign-in method/i });
    expect(within(chooser).queryByRole("button", { name: /remove stored credential/i })).toBeNull();
  });

  it("refreshes provider status after a completed paste", async () => {
    // The refresh is driven by the flow reaching `complete`, so the stub has to
    // actually walk the paste: start returns waiting_input, the submit returns
    // complete. Asserting a reload straight after `start` would pass against a
    // panel that never reports completion at all.
    render(<ConnectorsForm />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    const row = rowFor("openai");
    fireEvent.click(within(row).getByRole("button", { name: /connect/i }));
    fireEvent.click(screen.getByRole("button", { name: /sign in with chatgpt subscription/i }));

    const box = await screen.findByRole("textbox", { name: /redirect/i });
    fireEvent.change(box, {
      target: { value: "http://localhost:1455/auth/callback?code=abc&state=st-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: /finish sign-in/i }));

    // The credential now exists and only the server knows its status and mask,
    // so the row must re-list rather than keep claiming "no credential".
    await waitFor(() => expect(listProviders.mock.calls.length).toBeGreaterThan(1));
  });

  it("still saves an API key with the host threaded", async () => {
    render(<ConnectorsForm host="host-a" />);
    await waitFor(() => expect(listProviders).toHaveBeenCalled());

    const row = rowFor("anthropic");
    fireEvent.click(within(row).getByRole("button", { name: /connect/i }));
    fireEvent.change(screen.getByLabelText(/api key/i), { target: { value: "sk-test-1" } });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(setCredential).toHaveBeenCalled());
    expect(setCredential).toHaveBeenCalledWith("anthropic", { apiKey: "sk-test-1" }, "host-a");
  });
});