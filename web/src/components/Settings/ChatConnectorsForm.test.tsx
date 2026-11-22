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

describe("ChatConnectorsForm — add and test", () => {
  let lastPostBody: string | null;

  beforeEach(() => {
    lastPostBody = null;
    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: any) => {
      const url = String(input);
      const method = (init && init.method) || "GET";
      if (url === "/api/messaging/accounts" && method === "GET") {
        return { ok: true, json: async () => [] };
      }
      if (url === "/api/messaging/accounts" && method === "POST") {
        lastPostBody = init?.body ?? null;
        return { ok: true, status: 200, json: async () => ({ id: "tg-1" }) };
      }
      throw new Error(`ChatConnectorsForm.test: unexpected fetch to ${url} ${method}`);
    });
  });

  it("posts credentials on add, reloads, and reports success", async () => {
    render(<ChatConnectorsForm />);
    await waitFor(() => expect(screen.getByText(/hub\.mercstudio\.com/)).toBeTruthy());

    fireEvent.change(screen.getByPlaceholderText("Bot token"), {
      target: { value: "123456:ABC" },
    });
    fireEvent.click(screen.getByRole("button", { name: /add account/i }));

    await waitFor(() => expect(lastPostBody).not.toBeNull());
    expect(JSON.parse(String(lastPostBody))).toMatchObject({
      provider: "telegram",
      label: "",
      credentials: "123456:ABC",
    });
    await waitFor(() => expect(screen.getByText(/Account added./)).toBeTruthy());
  });

  it("shows an error when add fails", async () => {
    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: any) => {
      const url = String(input);
      const method = (init && init.method) || "GET";
      if (url === "/api/messaging/accounts" && method === "GET") {
        return { ok: true, json: async () => [] };
      }
      if (url === "/api/messaging/accounts" && method === "POST") {
        return { ok: false, status: 400, text: async () => "invalid token" };
      }
      throw new Error(`ChatConnectorsForm.test: unexpected fetch to ${url} ${method}`);
    });
    render(<ChatConnectorsForm />);
    await waitFor(() => expect(screen.getByText(/hub\.mercstudio\.com/)).toBeTruthy());

    fireEvent.change(screen.getByPlaceholderText("Bot token"), {
      target: { value: "bad" },
    });
    fireEvent.click(screen.getByRole("button", { name: /add account/i }));

    await waitFor(() => expect(screen.getByText(/Add failed:/)).toBeTruthy());
  });

  it("tests a telegram account and shows the result", async () => {
    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: any) => {
      const url = String(input);
      const method = (init && init.method) || "GET";
      if (url === "/api/messaging/accounts" && method === "GET") {
        return {
          ok: true,
          json: async () => [
            { id: "tg-1", provider: "telegram", label: "tg", status: "disconnected" },
          ],
        };
      }
      if (url === "/api/messaging/accounts/tg-1/test" && method === "POST") {
        return { ok: true, json: async () => ({ ok: true }) };
      }
      throw new Error(`ChatConnectorsForm.test: unexpected fetch to ${url} ${method}`);
    });
    render(<ChatConnectorsForm />);
    await waitFor(() => expect(screen.getByText(/tg/)).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: /test/i }));

    await waitFor(() => expect(screen.getByText(/Token valid./)).toBeTruthy());
  });

  it("shows the error when a test fails", async () => {
    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: any) => {
      const url = String(input);
      const method = (init && init.method) || "GET";
      if (url === "/api/messaging/accounts" && method === "GET") {
        return {
          ok: true,
          json: async () => [
            { id: "tg-1", provider: "telegram", label: "tg", status: "disconnected" },
          ],
        };
      }
      if (url === "/api/messaging/accounts/tg-1/test" && method === "POST") {
        return { ok: true, json: async () => ({ ok: false, error: "bad token" }) };
      }
      throw new Error(`ChatConnectorsForm.test: unexpected fetch to ${url} ${method}`);
    });
    render(<ChatConnectorsForm />);
    await waitFor(() => expect(screen.getByText(/tg/)).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: /test/i }));

    await waitFor(() => expect(screen.getByText(/bad token/)).toBeTruthy());
  });
});
