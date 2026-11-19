import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import ChatConnectorsForm from "./ChatConnectorsForm";
import { api } from "../../api/client";

vi.mock("../../api/client", () => ({
  api: { getSyncURLConfig: vi.fn() },
}));

// The bus auto-starts a long-lived SSE fetch on first subscribe; the form only
// needs a no-op unsubscribe here.
vi.mock("../../lib/eventBus", () => ({
  eventBus: { on: () => () => {} },
}));

const mockSyncConfig = vi.mocked(api.getSyncURLConfig);

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
  mockSyncConfig.mockResolvedValue({
    sync_url: "",
    resolved_url: "https://hub.mercstudio.com",
  });
  fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    // load() fetches /api/messaging/accounts explicitly; /api/messaging/sync is
    // exercised by the sync-push test below. Anything else fails loudly so an
    // unmocked call never passes silently.
    if (url === "/api/messaging/accounts") {
      return { ok: true, json: async () => [] };
    }
    if (url === "/api/messaging/sync") {
      return { ok: true, json: async () => [] };
    }
    throw new Error(`ChatConnectorsForm.test: unexpected fetch to ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("ChatConnectorsForm — messaging sync target", () => {
  it("shows the server-resolved sync target and no editable URL input", async () => {
    render(<ChatConnectorsForm />);

    await waitFor(() =>
      expect(screen.getByText(/hub\.mercstudio\.com/)).toBeTruthy(),
    );
    expect(screen.queryByPlaceholderText(/sync url/i)).toBeNull();
    expect(screen.queryByRole("textbox", { name: /sync url/i })).toBeNull();
  });

  it("pushes without a url so the server resolves the hub itself", async () => {
    render(<ChatConnectorsForm />);

    fireEvent.click(screen.getByRole("button", { name: /sync push/i }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input]) => String(input) === "/api/messaging/sync",
      );
      expect(call).toBeTruthy();
      expect(JSON.parse(String(call![1].body))).toEqual({ dir: "push" });
    });
  });
});
