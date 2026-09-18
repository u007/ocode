import { act, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { TerminalProvider, useTerminalState, getProjectTerminals } from "./terminalStore";
import { loadProjectTerminals, projectTerminalsKey } from "../components/Terminal/terminalPersistence";

const authedFetchMock = vi.fn((..._args: unknown[]) => Promise.resolve({ ok: true, status: 204 }));
vi.mock("@/api/client", () => ({
  authedFetch: (...a: unknown[]) => authedFetchMock(...a),
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
}));

function Harness({ projectPath, host }: { projectPath: string; host?: string }) {
  const { state, closeTerminal, killTerminal } = useTerminalState();
  const { terminals, live } = getProjectTerminals(state, projectPath, host);
  return (
    <div>
      <div data-testid="live">{String(live)}</div>
      <div data-testid="ids">{terminals.map((t) => t.id).join(",")}</div>
      <button onClick={() => closeTerminal(projectPath, "term-1-1", host)}>close</button>
      <button onClick={() => void killTerminal(projectPath, "term-1-1", host)}>kill</button>
    </div>
  );
}

function seedPersisted(projectPath: string, host: string | undefined, id: string, title: string) {
  const key = projectTerminalsKey(projectPath, host);
  window.localStorage.setItem(
    "ocode.ui.terminals.project.v1",
    JSON.stringify({ version: 1, projects: { [key]: { terminals: [{ id, title }], activeId: id } } }),
  );
}

beforeEach(() => {
  window.localStorage.clear();
  authedFetchMock.mockClear();
  authedFetchMock.mockResolvedValue({ ok: true, status: 204 });
});

describe("terminalStore close/kill of a peeked project", () => {
  it("closeTerminal removes a persisted (not-yet-live) terminal and DELETEs the shell", () => {
    seedPersisted("/proj", undefined, "term-1-1", "Terminal 1");
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    expect(screen.getByTestId("live").textContent).toBe("false");
    expect(screen.getByTestId("ids").textContent).toBe("term-1-1");

    act(() => screen.getByText("close").click());

    // The persisted entry is gone immediately (otherwise the tab would stay on
    // screen and the panel would reattach/respawn the shell).
    expect(loadProjectTerminals("/proj")).toBeNull();
    expect(authedFetchMock).toHaveBeenCalledWith("/api/terminal/term-1-1", { method: "DELETE" });
  });

  it("closeTerminal on an unknown id is a no-op (no DELETE, no fall-through)", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("close").click());
    expect(authedFetchMock).not.toHaveBeenCalled();
  });

  it("killTerminal removes a persisted terminal and DELETEs it on the remote host", async () => {
    seedPersisted("/proj", "dev@box", "term-1-1", "Terminal 1");
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" host="dev@box" />
      </TerminalProvider>,
    );

    await act(async () => {
      screen.getByText("kill").click();
    });

    expect(loadProjectTerminals("/proj", "dev@box")).toBeNull();
    const [url, init] = authedFetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe(`/api/remote/${encodeURIComponent("dev@box")}/api/terminal/term-1-1`);
    expect(init.method).toBe("DELETE");
    expect((init.headers as Headers).get("X-Ocode-Project")).toBe("/proj");
  });

  it("killTerminal still DELETEs a terminal this window never opened", async () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" host="dev@box" />
      </TerminalProvider>,
    );
    await act(async () => {
      screen.getByText("kill").click();
    });
    expect(authedFetchMock).toHaveBeenCalledWith(
      `/api/remote/${encodeURIComponent("dev@box")}/api/terminal/term-1-1`,
      expect.objectContaining({ method: "DELETE" }),
    );
  });
});
