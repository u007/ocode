import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { api } from "../../api/client";
import { openExternalURL } from "../../lib/externalLinks";
import CloudConnectorsForm, { hubCallbackOrigin } from "./CloudConnectorsForm";

vi.mock("../../lib/externalLinks", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("../../lib/externalLinks")>();
  return { ...actual, openExternalURL: vi.fn() };
});

/**
 * Settings → Cloud connectors is the only surface that starts the
 * kakiit-brokered OAuth handoff, so these tests pin the wiring that a green
 * handler suite cannot: the CTA's visibility by hub status, sorted provider
 * rows with a real status badge, the browse action, and the destructive
 * disconnect behind a rendered confirm that says the revoke is GLOBAL.
 *
 * Native confirm() silently returns false in the Wails/WKWebView desktop
 * webview, which would make a disconnect guarded by it permanently
 * unreachable while looking fine in a browser — hence the rendered dialog.
 */
const PROJECT = "/work/my-app";
const HOST = "user@box";
const FUTURE = "2099-01-01T00:00:00Z";
const PAST = "2001-01-01T00:00:00Z";

const HUB_CONNECTIONS = [
  {
    provider: "onedrive",
    connection_id: "cx-onedrive",
    root_path: "/Docs",
    expires_at: PAST,
  },
  {
    provider: "dropbox",
    connection_id: "cx-dropbox",
    root_path: "/Backups",
    expires_at: FUTURE,
  },
];
const LOCAL_CONNECTORS = [
  { id: "gdrive-main", provider: "google-drive", root_path: "root" },
];

const HUB_METHODS = [
  "listHubCloudConnections",
  "registerHubCloudCallback",
  "startHubCloudOAuth",
  "redeemHubCloudCode",
  "refreshHubCloudToken",
  "disconnectHubCloud",
] as const;

let listHub: ReturnType<typeof vi.fn>;
let listLocal: ReturnType<typeof vi.fn>;
let register: ReturnType<typeof vi.fn>;
let start: ReturnType<typeof vi.fn>;
let refresh: ReturnType<typeof vi.fn>;
let disconnect: ReturnType<typeof vi.fn>;
let browse: ReturnType<typeof vi.fn>;
let removeLocal: ReturnType<typeof vi.fn>;
const originals: Record<string, unknown> = {};

beforeEach(() => {
  cleanup();
  for (const key of HUB_METHODS) originals[key] = (api as any)[key];
  originals.listCloudConnectors = (api as any).listCloudConnectors;
  originals.browseCloudConnector = (api as any).browseCloudConnector;
  originals.removeCloudConnector = (api as any).removeCloudConnector;

  listHub = vi.fn(async () => HUB_CONNECTIONS);
  listLocal = vi.fn(async () => LOCAL_CONNECTORS);
  register = vi.fn(async () => ({ ok: true }));
  start = vi.fn(async () => ({ auth_url: "https://example.test/auth" }));
  refresh = vi.fn(async () => ({ ok: true }));
  disconnect = vi.fn(async () => ({ ok: true }));
  browse = vi.fn(async () => []);
  removeLocal = vi.fn(async () => ({ ok: true }));

  Object.assign(api, {
    listHubCloudConnections: listHub,
    registerHubCloudCallback: register,
    startHubCloudOAuth: start,
    refreshHubCloudToken: refresh,
    disconnectHubCloud: disconnect,
    listCloudConnectors: listLocal,
    browseCloudConnector: browse,
    removeCloudConnector: removeLocal,
  });
  vi.spyOn(window, "open").mockImplementation(() => null);
  vi.mocked(openExternalURL).mockClear();
});

afterEach(() => {
  for (const [key, value] of Object.entries(originals))
    (api as any)[key] = value;
  cleanup();
  vi.restoreAllMocks();
});

function rowFor(provider: string): HTMLElement {
  return screen.getByTestId(`cloud-provider-row-${provider}`);
}

describe("CloudConnectorsForm — kakiit CTA by hub status", () => {
  it("shows the Connect via kakiit CTA and 'Not logged in' when disconnected", async () => {
    listHub.mockResolvedValueOnce([]);
    listLocal.mockResolvedValueOnce([]);
    render(<CloudConnectorsForm project={PROJECT} host={HOST} />);

    await waitFor(() => expect(listHub).toHaveBeenCalledWith(PROJECT, HOST));
    expect(screen.getByText(/not logged in to kakiit/i)).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /connect via kakiit/i }),
    ).toBeTruthy();
  });

  it("hides the CTA when the only connections are local-only", async () => {
    listHub.mockResolvedValueOnce([]);
    listLocal.mockResolvedValueOnce(LOCAL_CONNECTORS);
    render(<CloudConnectorsForm project={PROJECT} host={HOST} />);

    await waitFor(() => expect(listHub).toHaveBeenCalledWith(PROJECT, HOST));
    expect(screen.getByText(/local only/i)).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: /connect via kakiit/i }),
    ).toBeNull();
  });

  it("shows a connected header when a hub connection exists", async () => {
    render(<CloudConnectorsForm project={PROJECT} host={HOST} />);
    await waitFor(() => expect(listHub).toHaveBeenCalledWith(PROJECT, HOST));
    expect(screen.getByText(/connected via kakiit/i)).toBeTruthy();
  });
});

describe("CloudConnectorsForm — provider rows", () => {
  it("renders rows sorted by provider name with status badges", async () => {
    render(<CloudConnectorsForm project={PROJECT} host={HOST} />);
    await waitFor(() => expect(listHub).toHaveBeenCalled());

    const rows = screen.getAllByTestId(/^cloud-provider-row-/);
    expect(rows.map((row) => row.getAttribute("data-provider"))).toEqual([
      "dropbox",
      "google-drive",
      "onedrive",
    ]);
    expect(within(rowFor("dropbox")).getByText("Active")).toBeTruthy();
    expect(within(rowFor("onedrive")).getByText("Expired")).toBeTruthy();
    expect(within(rowFor("google-drive")).getByText("Local-only")).toBeTruthy();
  });

  it("shows an icon empty state instead of 'None.'", async () => {
    listHub.mockResolvedValueOnce([]);
    listLocal.mockResolvedValueOnce([]);
    render(<CloudConnectorsForm project={PROJECT} host={HOST} />);
    await waitFor(() => expect(listHub).toHaveBeenCalled());

    expect(screen.getByTestId("cloud-connectors-empty")).toBeTruthy();
    expect(screen.queryByText(/^None\.$/)).toBeNull();
  });

  it("browses a hub connection by its connection id, threading host", async () => {
    render(<CloudConnectorsForm project={PROJECT} host={HOST} />);
    await waitFor(() => expect(listHub).toHaveBeenCalled());

    fireEvent.click(
      within(rowFor("dropbox")).getByRole("button", { name: /browse/i }),
    );
    await waitFor(() =>
      expect(browse).toHaveBeenCalledWith("cx-dropbox", "", HOST),
    );
  });
});

describe("CloudConnectorsForm — destructive actions", () => {
  it("confirms before disconnecting and states the revoke is global", async () => {
    render(<CloudConnectorsForm project={PROJECT} host={HOST} />);
    await waitFor(() => expect(listHub).toHaveBeenCalled());

    fireEvent.click(
      within(rowFor("dropbox")).getByRole("button", { name: /disconnect/i }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/revok/i)).toBeTruthy();
    expect(within(dialog).getByText(/all your projects/i)).toBeTruthy();
    expect(disconnect).not.toHaveBeenCalled();

    fireEvent.click(
      within(dialog).getByRole("button", { name: /disconnect/i }),
    );
    await waitFor(() =>
      expect(disconnect).toHaveBeenCalledWith(PROJECT, "cx-dropbox", HOST),
    );
  });

  it("does not disconnect when the confirm is cancelled", async () => {
    render(<CloudConnectorsForm project={PROJECT} host={HOST} />);
    await waitFor(() => expect(listHub).toHaveBeenCalled());

    fireEvent.click(
      within(rowFor("dropbox")).getByRole("button", { name: /disconnect/i }),
    );
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /cancel/i }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(disconnect).not.toHaveBeenCalled();
  });
});

describe("CloudConnectorsForm — hub start wiring", () => {
  it("threads project and host into the hub start call with a real PKCE challenge", async () => {
    listHub.mockResolvedValueOnce([]);
    listLocal.mockResolvedValueOnce([]);
    render(<CloudConnectorsForm project={PROJECT} host={HOST} />);
    await waitFor(() => expect(listHub).toHaveBeenCalledWith(PROJECT, HOST));

    fireEvent.click(
      screen.getByRole("button", { name: /connect via kakiit/i }),
    );
    await waitFor(() => expect(start).toHaveBeenCalledTimes(1));

    const [project, payload, host] = start.mock.calls[0];
    expect(project).toBe(PROJECT);
    expect(host).toBe(HOST);
    expect(Object.keys(payload).sort()).toEqual([
      "client_state",
      "code_challenge",
      "connector_id",
      "install_id",
      "provider",
    ]);
    expect(payload.provider).toBe("dropbox");
    // base64url(SHA-256) is exactly 43 URL-safe characters.
    expect(payload.code_challenge).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(payload.client_state).toContain(encodeURIComponent(PROJECT));
    expect(payload.client_state).toContain(encodeURIComponent(HOST));

    // kakiit now requires a callback registered for this install before it
    // accepts /oauth/start, so the CTA must register first — and the loopback
    // callback must carry both the project and the remote host back to ocode.
    const expectedCallback =
      `${window.location.origin}/api/cloud/oauth/kakiit/return` +
      `?project=${encodeURIComponent(PROJECT)}&host=${encodeURIComponent(HOST)}`;
    expect(register).toHaveBeenCalledTimes(1);
    expect(register).toHaveBeenCalledWith(
      PROJECT,
      payload.install_id,
      expectedCallback,
      HOST,
    );
    expect(register.mock.invocationCallOrder[0]).toBeLessThan(
      start.mock.invocationCallOrder[0],
    );
  });
});

describe("CloudConnectorsForm — kakiit callback origin", () => {
  // jsdom's window.location cannot be moved to https, so the desktop
  // loopback conversion is pinned on the pure function directly.
  it("serves a desktop https loopback origin as plain http", () => {
    expect(hubCallbackOrigin("https://127.0.0.1:52341")).toBe(
      "http://127.0.0.1:52341",
    );
    expect(hubCallbackOrigin("https://localhost:52341")).toBe(
      "http://localhost:52341",
    );
    expect(hubCallbackOrigin("https://[::1]:52341")).toBe(
      "http://[::1]:52341",
    );
  });

  it("leaves http loopback and non-loopback origins unchanged", () => {
    expect(hubCallbackOrigin("http://127.0.0.1:52341")).toBe(
      "http://127.0.0.1:52341",
    );
    expect(hubCallbackOrigin("http://localhost:3000")).toBe(
      "http://localhost:3000",
    );
    expect(hubCallbackOrigin("https://ocode.example.com")).toBe(
      "https://ocode.example.com",
    );
  });
});

describe("CloudConnectorsForm — kakiit return flow", () => {
  it("opens the kakiit auth page through openExternalURL, not window.open", async () => {
    listHub.mockResolvedValueOnce([]);
    listLocal.mockResolvedValueOnce([]);
    render(<CloudConnectorsForm project={PROJECT} host={HOST} />);
    await waitFor(() => expect(listHub).toHaveBeenCalled());

    fireEvent.click(
      screen.getByRole("button", { name: /connect via kakiit/i }),
    );
    await waitFor(() =>
      expect(openExternalURL).toHaveBeenCalledWith("https://example.test/auth"),
    );
    expect(window.open).not.toHaveBeenCalled();
  });

  it("reloads the connection list once when the window regains focus after a start", async () => {
    listHub.mockResolvedValueOnce([]);
    listLocal.mockResolvedValueOnce([]);
    render(<CloudConnectorsForm project={PROJECT} host={HOST} />);
    await waitFor(() => expect(listHub).toHaveBeenCalledTimes(1));

    fireEvent.click(
      screen.getByRole("button", { name: /connect via kakiit/i }),
    );
    await waitFor(() => expect(openExternalURL).toHaveBeenCalled());
    expect(listHub).toHaveBeenCalledTimes(1);

    window.dispatchEvent(new Event("focus"));
    await waitFor(() => expect(listHub).toHaveBeenCalledTimes(2));

    // The listener removes itself after firing once.
    window.dispatchEvent(new Event("focus"));
    expect(listHub).toHaveBeenCalledTimes(2);
  });

  it("does not reload on focus when no kakiit start happened", async () => {
    listHub.mockResolvedValueOnce([]);
    listLocal.mockResolvedValueOnce([]);
    render(<CloudConnectorsForm project={PROJECT} host={HOST} />);
    await waitFor(() => expect(listHub).toHaveBeenCalledTimes(1));

    window.dispatchEvent(new Event("focus"));
    expect(listHub).toHaveBeenCalledTimes(1);
  });
});
