import { render, screen, act } from "@testing-library/react";
import { describe, it, expect, beforeEach } from "vitest";
import { TerminalProvider, useTerminalState, getProjectTerminals, PROCESSES_TAB_ID } from "./terminalStore";

function Harness({ projectPath }: { projectPath: string }) {
  const { state, activate, openTerminal, closeTerminal, setActiveId, markAlerted, clearAlert } =
    useTerminalState();
  const { terminals, activeId, live } = getProjectTerminals(state, projectPath);
  return (
    <div>
      <div data-testid="live">{String(live)}</div>
      <div data-testid="active-id">{activeId}</div>
      <div data-testid="count">{terminals.length}</div>
      <div data-testid="titles">{terminals.map((t) => t.title).join(",")}</div>
      <div data-testid="alerted">
        {terminals.map((t) => (t.alerted ? t.id : "")).filter(Boolean).join(",")}
      </div>
      <button onClick={() => activate(projectPath)}>activate</button>
      <button onClick={() => openTerminal(projectPath)}>open</button>
      <button onClick={() => activeId && closeTerminal(projectPath, activeId)}>close-active</button>
      <button onClick={() => setActiveId(projectPath, PROCESSES_TAB_ID)}>focus-processes</button>
      {terminals.map((t) => (
        <span key={t.id}>
          <button onClick={() => markAlerted(projectPath, t.id)}>{`mark-${t.id}`}</button>
          <button onClick={() => clearAlert(projectPath, t.id)}>{`clear-${t.id}`}</button>
        </span>
      ))}
    </div>
  );
}

beforeEach(() => {
  window.localStorage.clear();
});

function seedPersisted(projectPath: string, terminals: { id: string; title: string }[], activeId: string) {
  window.localStorage.setItem(
    "ocode.ui.terminals.project.v1",
    JSON.stringify({ version: 1, projects: { [projectPath]: { terminals, activeId } } }),
  );
}

describe("terminalStore", () => {
  it("getProjectTerminals peeks persisted metadata without going live", () => {
    seedPersisted("/proj", [{ id: "term-1-1", title: "Terminal 1" }], "term-1-1");
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    expect(screen.getByTestId("live").textContent).toBe("false");
    expect(screen.getByTestId("count").textContent).toBe("1");
    expect(screen.getByTestId("active-id").textContent).toBe("term-1-1");
  });

  it("activate() with nothing persisted creates one fresh terminal and goes live", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("count").textContent).toBe("1");
  });

  it("activate() with persisted terminals restores them and goes live", () => {
    seedPersisted("/proj", [{ id: "term-9-1", title: "Terminal 9" }], "term-9-1");
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("titles").textContent).toBe("Terminal 9");
  });

  it("openTerminal() on a never-activated project seeds from persisted terminals then appends a new one", () => {
    seedPersisted("/proj", [{ id: "term-9-1", title: "Terminal 9" }], "term-9-1");
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("open").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("count").textContent).toBe("2");
  });

  it("closeTerminal() falls back the active id to the last remaining terminal", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    act(() => screen.getByText("open").click());
    expect(screen.getByTestId("count").textContent).toBe("2");
    act(() => screen.getByText("close-active").click());
    expect(screen.getByTestId("count").textContent).toBe("1");
  });

  it("setActiveId() to the Processes sentinel activates a never-activated project first", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("focus-processes").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("active-id").textContent).toBe(PROCESSES_TAB_ID);
  });

  it("markAlerted surfaces an alert on the terminal and clearAlert removes it", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    const id = screen.getByTestId("active-id").textContent!;
    expect(screen.getByTestId("alerted").textContent).toBe("");
    act(() => screen.getByText(`mark-${id}`).click());
    expect(screen.getByTestId("alerted").textContent).toBe(id);
    act(() => screen.getByText(`clear-${id}`).click());
    expect(screen.getByTestId("alerted").textContent).toBe("");
  });

  it("closing an alerted terminal drops its alert flag along with the terminal", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    const id = screen.getByTestId("active-id").textContent!;
    act(() => screen.getByText(`mark-${id}`).click());
    expect(screen.getByTestId("alerted").textContent).toBe(id);
    act(() => screen.getByText("close-active").click());
    expect(screen.getByTestId("alerted").textContent).toBe("");
  });

  it("alert state is ephemeral and is never persisted to disk", async () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    const id = screen.getByTestId("active-id").textContent!;
    act(() => screen.getByText(`mark-${id}`).click());
    expect(screen.getByTestId("alerted").textContent).toBe(id);
    // Let the debounced persistence (200ms) flush.
    await new Promise((r) => setTimeout(r, 300));
    const raw = window.localStorage.getItem("ocode.ui.terminals.project.v1");
    expect(raw).not.toBeNull();
    expect(raw).not.toContain("alerted");
  });
});
