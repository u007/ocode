import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * Deleting a profile and removing a provider key were guarded by native
 * `confirm()`, which SILENTLY RETURNS FALSE in the Wails/WKWebView desktop
 * webview — so both actions were unreachable in the desktop app while looking
 * correct in a browser. They also destroy on-disk credentials, so the wording
 * must stay explicit.
 */
const authedFetch = vi.hoisted(() => vi.fn());
vi.mock("@/api/client", () => ({ authedFetch: (...args: unknown[]) => authedFetch(...args) }));
vi.mock("../ProfileSwitcher", () => ({ getActiveWindowId: () => "win-1" }));

import ProfilesManager from "./ProfilesManager";

const profile = {
  name: "work",
  displayName: "Work",
  overrideCount: 2,
  credentialCount: 1,
};

const credential = { provider: "openai", label: "OpenAI", masked: "sk-…1234", kind: "apiKey" };

/** Route authedFetch by method+url so each test only asserts what it cares about. */
function routeFetch(overrides: Record<string, { ok?: boolean; body?: unknown }> = {}) {
  authedFetch.mockImplementation(async (url: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${url}`;
    for (const [pattern, spec] of Object.entries(overrides)) {
      if (key.includes(pattern)) {
        return {
          ok: spec.ok ?? true,
          status: spec.ok === false ? 400 : 200,
          text: async () => (typeof spec.body === "string" ? spec.body : JSON.stringify(spec.body ?? {})),
          json: async () => spec.body ?? {},
        };
      }
    }
    // Detail routes first: they all START with "GET /api/profiles", so a
    // list-first router would shadow them and silently yield no credentials.
    if (key.includes("/effective")) {
      return { ok: true, status: 200, text: async () => "{}", json: async () => ({}) };
    }
    if (key.includes("/auth")) {
      return {
        ok: true,
        status: 200,
        text: async () => JSON.stringify({ credentials: [credential] }),
        json: async () => ({ credentials: [credential] }),
      };
    }
    if (key === "GET /api/profiles") {
      return {
        ok: true,
        status: 200,
        text: async () => JSON.stringify({ profiles: [profile] }),
        json: async () => ({ profiles: [profile] }),
      };
    }
    return { ok: true, status: 200, text: async () => "{}", json: async () => ({}) };
  });
}

function jsonCalls(method: string) {
  return authedFetch.mock.calls.filter((call) => (call[1] as RequestInit | undefined)?.method === method);
}

async function renderManager() {
  const view = render(<ProfilesManager />);
  // The row's name button renders "work ▸" as split text nodes, so wait on the
  // row's own Delete button instead.
  await screen.findByRole("button", { name: "Delete" });
  return view;
}

/** Expand the profile row so its credential list (and the Remove button) shows. */
async function expandProfile() {
  fireEvent.click(screen.getByRole("button", { name: /work/ }));
  // The credential row renders "{masked} ({kind})" as split text nodes, so wait
  // on its Remove button instead.
  await screen.findByRole("button", { name: "Remove" });
}

function dialog() {
  return screen.getByRole("dialog");
}

beforeEach(() => {
  authedFetch.mockReset();
  routeFetch();
});

describe("ProfilesManager delete confirmation", () => {
  it("does not delete the profile until the confirm is accepted", async () => {
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));

    expect(dialog()).toBeDefined();
    expect(jsonCalls("DELETE")).toHaveLength(0);

    fireEvent.click(within(dialog()).getByRole("button", { name: "Delete profile" }));
    await waitFor(() =>
      expect(jsonCalls("DELETE")[0][0]).toBe("/api/profiles/work"),
    );
  });

  it("states what the delete destroys and that it cannot be undone", async () => {
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));

    const copy = dialog().textContent ?? "";
    // The fixture has 2 overrides + 1 key; the old native copy named both.
    expect(copy).toContain("2");
    expect(copy).toMatch(/1 key/i);
    expect(copy).toMatch(/cannot be undone/i);
  });

  it("keeps the profile when the confirm is cancelled", async () => {
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    fireEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));

    expect(jsonCalls("DELETE")).toHaveLength(0);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps the confirm open and shows the reason when the delete fails", async () => {
    routeFetch({ "DELETE /api/profiles/work": { ok: false, body: { error: "profile is active" } } });
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    fireEvent.click(within(dialog()).getByRole("button", { name: "Delete profile" }));

    await waitFor(() =>
      expect(within(dialog()).getByRole("alert").textContent).toMatch(/cannot delete|active|failed/i),
    );
    expect(dialog()).toBeDefined();
  });
});

describe("ProfilesManager provider key removal", () => {
  it("does not remove the key until the confirm is accepted", async () => {
    await renderManager();
    await expandProfile();
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));

    expect(dialog()).toBeDefined();
    expect(jsonCalls("DELETE")).toHaveLength(0);

    fireEvent.click(within(dialog()).getByRole("button", { name: "Remove key" }));
    await waitFor(() =>
      expect(jsonCalls("DELETE")[0][0]).toBe("/api/profiles/work/auth/openai"),
    );
  });

  it("keeps the key when the confirm is cancelled", async () => {
    await renderManager();
    await expandProfile();
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    fireEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));

    expect(jsonCalls("DELETE")).toHaveLength(0);
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("ProfilesManager rename dialog", () => {
  it("does not rename until the dialog is submitted", async () => {
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Rename" }));

    // A rendered dialog, not the native prompt() (which is unsupported in the
    // Wails/WKWebView desktop webview, so rename was dead there).
    const d = dialog();
    expect(within(d).getByRole("textbox")).toHaveValue("work");
    expect(jsonCalls("POST")).toHaveLength(0);

    fireEvent.change(within(d).getByRole("textbox"), { target: { value: "job" } });
    fireEvent.click(within(d).getByRole("button", { name: "Rename" }));
    await waitFor(() =>
      expect(jsonCalls("POST")[0][0]).toBe("/api/profiles/work/rename"),
    );
    expect(JSON.parse((jsonCalls("POST")[0][1] as RequestInit).body as string)).toEqual({
      newName: "job",
    });
  });

  it("issues no request when the rename dialog is cancelled", async () => {
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Rename" }));
    fireEvent.change(within(dialog()).getByRole("textbox"), { target: { value: "job" } });
    fireEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));

    expect(jsonCalls("POST")).toHaveLength(0);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("refuses an invalid name without contacting the server", async () => {
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Rename" }));
    fireEvent.change(within(dialog()).getByRole("textbox"), { target: { value: "Not Valid!" } });
    fireEvent.click(within(dialog()).getByRole("button", { name: "Rename" }));

    // The dialog renders the full rule, so match a substring, not the whole string.
    await waitFor(() => expect(screen.getByText(/name must match/)).toBeDefined());
    expect(jsonCalls("POST")).toHaveLength(0);
    expect(dialog()).toBeDefined();
  });

  it("normalizes a submitted name the way the server expects", async () => {
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Rename" }));
    fireEvent.change(within(dialog()).getByRole("textbox"), { target: { value: "  Job-2  " } });
    fireEvent.click(within(dialog()).getByRole("button", { name: "Rename" }));
    await waitFor(() => expect(jsonCalls("POST")).toHaveLength(1));
    expect(JSON.parse((jsonCalls("POST")[0][1] as RequestInit).body as string)).toEqual({
      newName: "job-2",
    });
  });

  it("submits on Enter in the name field", async () => {
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Rename" }));
    const box = within(dialog()).getByRole("textbox");
    fireEvent.change(box, { target: { value: "job" } });
    fireEvent.keyDown(box, { key: "Enter" });

    await waitFor(() => expect(jsonCalls("POST")).toHaveLength(1));
    expect(JSON.parse((jsonCalls("POST")[0][1] as RequestInit).body as string)).toEqual({
      newName: "job",
    });
  });

  it("keeps the dialog open and shows the reason when the rename fails", async () => {
    routeFetch({ "POST /api/profiles/work/rename": { ok: false, body: "name already in use" } });
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Rename" }));
    fireEvent.change(within(dialog()).getByRole("textbox"), { target: { value: "job" } });
    fireEvent.click(within(dialog()).getByRole("button", { name: "Rename" }));

    await waitFor(() =>
      expect(within(dialog()).getByRole("alert").textContent).toContain("already in use"),
    );
    expect(dialog()).toBeDefined();
  });
});
