// Full-app reproduction for the reported bug: "when chat is streaming, the
// model dialog or any dialog popup will auto close".
//
// Mounts the REAL App (providers + routing + CoworkSidebar + ModelDialog +
// SessionTabSync), opens the real ModelDialog through the real sidebar, then
// pumps a realistic streaming burst (session_started rekey → user_message →
// per-token text deltas → turn_done) through the REAL event-routing code
// (SessionTabSync → routeBusEnvelope → chatStore). The burst is verified to
// actually reach the UI (streamed tokens render in the transcript) before
// asserting the dialog stays open. If the dialog survives, the dispatch path
// is exonerated and the close must come from an interaction/environmental
// source rather than streaming state churn.
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeAll, beforeEach } from "vitest";
import { MemoryRouter } from "react-router-dom";
import type { BusEnvelope } from "./lib/eventBus";

type Handler = (env: BusEnvelope) => void;

const bus = vi.hoisted(() => {
  const handlers = new Map<string, Set<Handler>>();
  const reconnect = new Set<() => void>();
  return { handlers, reconnect };
});

beforeAll(() => {
  (window as unknown as { matchMedia: unknown }).matchMedia = ((media: string) => ({
    matches: false,
    media,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;

  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = ResizeObserverStub;
});

vi.mock("./api/client", () => {
  const project = { path: "/proj", name: "proj" };
  const impl: Record<string, () => Promise<unknown>> = {
    listProjects: async () => [project],
    getCurrentProject: async () => ({ project }),
    listProjectSessions: async () => [],
    listGroups: async () => [],
    getSpending: async () => ({ spending_usd: 0 }),
    listModels: async () => [
      { name: "anthropic/claude-a", model: "claude-a", provider: "anthropic", active: false },
    ],
    getConfigModel: async () => ({ model: "" }),
    getSmallModel: async () => ({ model: "" }),
    getAdvisor: async () => ({ model: "" }),
    getAdvisorFull: async () => ({ claude_code: false }),
    getSessionStatus: async () => ({}),
    getSessionState: async () => ({ bootstrap_stage: "", turn_active: false, last_seq: 0 }),
    getSession: async () => ({ messages: [], total: 0, title: "" }),
    listSessions: async () => ({ sessions: [] }),
    listCustomCommands: async () => [],
    listCommands: async () => [],
    listSkills: async () => [],
    listAgents: async () => [],
  };
  const api = new Proxy({} as Record<string, unknown>, {
    get: (target, prop: string) => {
      if (!(prop in target)) {
        target[prop] = vi.fn(impl[prop] ?? (async () => ({})));
      }
      return target[prop];
    },
  });
  return {
    api,
    authHeaders: () => ({}),
    authToken: () => "",
    apiPath: (p: string) => p,
    authedFetch: vi.fn(async () => new Response("{}")),
    getBrowseBase: vi.fn(async () => "http://browse.test"),
    mintBrowseGrant: vi.fn(async () => "G1"),
    revokeBrowseSession: vi.fn(async () => {}),
    browseSrc: (base: string, grant: string | null, key: string) =>
      `${base}/b/${key}/${grant ? "g" : "n"}`,
  };
});

vi.mock("./lib/eventBus", () => ({
  eventBus: {
    on: (event: string, handler: Handler) => {
      if (!bus.handlers.has(event)) bus.handlers.set(event, new Set());
      bus.handlers.get(event)!.add(handler);
      return () => bus.handlers.get(event)?.delete(handler);
    },
    onReconnect: (handler: () => void) => {
      bus.reconnect.add(handler);
      return () => bus.reconnect.delete(handler);
    },
    emit: (event: string, env: BusEnvelope) => {
      bus.handlers.get(event)?.forEach((h) => h(env));
    },
    reconnectAll: () => bus.reconnect.forEach((h) => h()),
    start: () => {},
    stop: () => {},
    setProjects: () => {},
  },
}));

// Keep the streaming surfaces real; stub only unrelated heavy panels.
vi.mock("./components/Chat/AgentPreview", () => ({ default: () => null }));
vi.mock("./components/Agents/AgentsPanel", () => ({ default: () => null }));
vi.mock("./components/common/StatusBar", () => ({ default: () => null }));
vi.mock("./components/Status/StatusPanel", () => ({ default: () => null }));
vi.mock("./components/Git/GitPanel", () => ({ default: () => null }));
vi.mock("./components/Changes/ChangesPanel", () => ({ default: () => null }));
vi.mock("./components/Files/FileTree", () => ({ default: () => null }));
vi.mock("./components/Files/FileEditor", () => ({ default: () => null }));
vi.mock("./components/Logs/LogPanel", () => ({ default: () => null }));
vi.mock("./components/Terminal/TerminalTabs", () => ({ default: () => null }));
vi.mock("./components/Assets/AssetsPanel", () => ({ default: () => null }));
vi.mock("./components/Cron/CronPanel", () => ({ default: () => null }));
vi.mock("./components/Layout/ProjectSidebar", () => ({ default: () => null }));
vi.mock("./components/Layout/SessionSubTabs", () => ({ default: () => null }));
vi.mock("./components/Layout/SessionDialog", () => ({ default: () => null }));
vi.mock("./components/Layout/TopTabs", () => ({ default: () => null }));
vi.mock("./components/Layout/OpenSessionBar", () => ({ default: () => null }));

vi.mock("./pages/SessionPage", () => ({ default: () => null }));
vi.mock("./lib/debug/frontendMemoryReporter", () => ({ default: () => null }));
vi.mock("./components/common/ErrorBoundary", () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

import App from "./App";
import { __resetLastAppliedSeqForTests } from "./lib/sessionEvents";
// The mocked eventBus (see vi.mock above): emit() drives the real router.
// Cast: the real EventBus type has no public emit — the mock adds it.
import { eventBus } from "./lib/eventBus";
const emitEnvelope = (eventBus as unknown as {
  emit: (event: string, env: BusEnvelope) => void;
}).emit;

function renderApp() {
  return render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  window.localStorage.clear();
  bus.handlers.clear();
  __resetLastAppliedSeqForTests();
});

describe("dialogs stay open while chat is streaming", () => {
  it("keeps the ModelDialog open across a full streaming burst", async () => {
    renderApp();

    // Open a chat session tab. The temp tab id is `new-${Date.now()}` —
    // pin Date.now for exactly that call so we know the id for the rekey.
    const nowSpy = vi.spyOn(Date, "now").mockReturnValue(1_700_000_000_000);
    fireEvent.click(await screen.findByRole("button", { name: /new chat session/i }));
    nowSpy.mockRestore();
    const tempId = "new-1700000000000";
    await screen.findByRole("button", { name: /toggle browser panel/i });

    // Open the real model dialog via the real cowork sidebar's Model row.
    // (The settings tab also has a "Model Defaults & Recap" nav button — match
    // the exact "Model" label inside a button instead of a text prefix.)
    const modelRow = await screen.findAllByRole("button").then((buttons) =>
      buttons.find((b) =>
        Array.from(b.querySelectorAll("div, span")).some((el) => el.textContent === "Model"),
      ),
    );
    expect(modelRow).toBeTruthy();
    fireEvent.click(modelRow!);
    await screen.findByRole("dialog");
    expect(screen.getByText("Select Model")).toBeInTheDocument();

    // Streaming burst through the real router. Start with session_started so
    // the temp tab rekeys to the real session id (request_id correlation).
    const sessionId = "sess-1";
    const env = (event: string, seq: number, data: unknown): BusEnvelope => ({
      event,
      seq,
      data,
      session_id: sessionId,
    });
    emitEnvelope("session_started", env("session_started", 1, { session_id: sessionId, request_id: tempId }));
    emitEnvelope("user_message", env("user_message", 2, { content: "hello" }));
    emitEnvelope("turn_started", env("turn_started", 3, {}));
    for (let i = 0; i < 30; i++) {
      emitEnvelope("text", env("text", 4 + i, { delta: `token ${i} ` }));
    }

    // Mid-burst: the stream must have actually reached the chat UI (proving
    // the routing ran) and the dialog must still be open.
    await waitFor(() => {
      expect(screen.getByText(/token 15/)).toBeInTheDocument();
    });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("Select Model")).toBeInTheDocument();

    emitEnvelope("turn_done", env("turn_done", 100, {}));

    // After the turn completes (streaming cleared) the dialog stays open.
    await waitFor(() => {
      expect(screen.getByText(/token 29/)).toBeInTheDocument();
    });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("Select Model")).toBeInTheDocument();
  });
});