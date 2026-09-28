import { describe, expect, it, vi, beforeEach } from "vitest";
import { useEffect } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import ModelDialog from "./ModelDialog";
import { ProjectProvider, useProjectDispatch } from "../../stores/projectStore";
import { useSessionHost } from "../../hooks/useSessionHost";
import type { CompactConfig } from "../../api/client";
import type { ModelInfo } from "../../api/types";

// Mirrors web/src/components/Layout/ModelDialog.test.tsx's harness: the
// summary picker is a NEW direct-persisting purpose (sidebar-owned), so it
// needs the same api mock surface plus get/setCompactConfig.
const hoisted = vi.hoisted(() => {
  const models: ModelInfo[] = [
    { name: "anthropic/claude-haiku-4-5", model: "claude-haiku-4-5", provider: "anthropic", active: false },
    { name: "openai/gpt-4o-mini", model: "gpt-4o-mini", provider: "openai", active: false },
  ];
  const STORED: CompactConfig = {
    enabled: true,
    summary_provider: "",
    summary_model: "",
    token_threshold: 0.85,
    keep_recent_turns: 3,
    keep_recent_tokens: 20000,
    min_messages: 8,
    summary_timeout_seconds: 300,
    summary_first_token_timeout_seconds: 300,
    summary_max_retries: 2,
    max_summary_input_tokens: 50000,
  };
  const api = {
    listModels: vi.fn(async () => models.map((m) => ({ ...m }))),
    getConfigModel: vi.fn(async () => ({ model: "" })),
    getSmallModel: vi.fn(async () => ({ model: "" })),
    getAdvisor: vi.fn(async () => ({ model: "" })),
    getLocalModelsConfig: vi.fn(async () => ({})),
    getCompactConfig: vi.fn(async (): Promise<CompactConfig> => ({ ...STORED })),
    // Models the server MERGE (patch onto the stored block), not a replace.
    setCompactConfig: vi.fn(
      async (patch: Partial<CompactConfig>): Promise<CompactConfig> => ({ ...STORED, ...patch }),
    ),
    // ProjectProvider fires these on mount.
    listProjects: vi.fn(async (): Promise<unknown[]> => []),
    getCurrentProject: vi.fn(async () => null),
    listProjectSessions: vi.fn(async () => []),
    listGroups: vi.fn(async () => []),
    getTabs: vi.fn(async () => ({ projects: {} })),
    setTabs: vi.fn(async () => ({ status: "ok" })),
  };
  const dispatchSpy = vi.fn();
  return { models, api, STORED, dispatchSpy };
});

vi.mock("../../api/client", () => ({ api: hoisted.api }));
vi.mock("../../lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {}, start: () => {}, stop: () => {} },
}));
vi.mock("../../stores/chatStore", () => ({
  useChatSelector: (sel: (s: { model: string; smallModel: string; advisorModel: string }) => unknown) =>
    sel({ model: "", smallModel: "", advisorModel: "" }),
  // useChatDispatch MUST return a STABLE reference: it lands in ModelDialog's
  // open-effect dependency array, so a fresh vi.fn() per render re-runs the
  // effect (and its setState) forever.
  useChatDispatch: () => hoisted.dispatchSpy,
  getSessionSlice: () => ({ tuiStatus: { main_model: "" } }),
}));

// Full ProjectInfo shape (host is what useSessionHost reads) — mirrors the
// fixture in ModelDialog.test.tsx.
const remoteProject = {
  path: "/remote",
  name: "remote",
  host: "devbox",
  added_at: "",
  last_used_at: "",
  order: 1,
  group: "",
};

function HeaderModelDialog({ sessionId, purpose }: { sessionId: string; purpose?: import("./ModelDialog").ModelDialogTab }) {
  const host = useSessionHost(sessionId);
  return <ModelDialog open onClose={vi.fn()} sessionId={sessionId} host={host} purpose={purpose} />;
}

function SeedRemoteTab({ children }: { children: React.ReactNode }) {
  const dispatch = useProjectDispatch();
  useEffect(() => {
    dispatch({ type: "SET_PROJECTS", projects: [remoteProject] });
    dispatch({ type: "SET_ACTIVE_PROJECT", project: remoteProject });
    dispatch({
      type: "ADD_TAB",
      tab: { id: "sess-remote", projectPath: "/remote", title: "Remote", activeSubTab: "chat" },
    });
  }, [dispatch]);
  return <>{children}</>;
}

beforeEach(() => {
  vi.clearAllMocks();
  hoisted.api.listProjects.mockResolvedValue([remoteProject]);
});

describe("ModelDialog summary purpose (sidebar direct trigger)", () => {
  it("reads the stored summary model on open so the active row is highlighted", async () => {
    hoisted.api.getCompactConfig.mockResolvedValueOnce({
      ...hoisted.STORED,
      summary_model: "anthropic/claude-haiku-4-5",
    });

    render(<ModelDialog open onClose={vi.fn()} purpose="summary" />);

    await waitFor(() => expect(hoisted.api.getCompactConfig).toHaveBeenCalledTimes(1));
    // The stored model's row carries the selection Check; the other does not.
    const row = (id: string) => document.querySelector<HTMLElement>(`[data-list-nav-id="${id}"]`);
    await waitFor(() =>
      expect(row("anthropic:anthropic/claude-haiku-4-5")?.querySelector(".lucide-check")).not.toBeNull(),
    );
    expect(row("openai:openai/gpt-4o-mini")?.querySelector(".lucide-check")).toBeNull();
  });

  it("persists a pick straight to the compact config, keeping the on/off gate untouched", async () => {
    render(<ModelDialog open onClose={vi.fn()} purpose="summary" />);
    await waitFor(() => expect(hoisted.api.listModels).toHaveBeenCalled());

    fireEvent.click(await screen.findByText("gpt-4o-mini"));

    // ONLY the two model keys travel: the server merges them onto the block it
    // reads from disk, so the compaction tuning another control owns survives.
    await waitFor(() =>
      expect(hoisted.api.setCompactConfig).toHaveBeenCalledExactlyOnceWith(
        { summary_model: "openai/gpt-4o-mini", summary_provider: "" },
        undefined,
      ),
    );
    // A pick must never carry `enabled` — the gate belongs to the checkbox.
    expect(hoisted.api.setCompactConfig.mock.calls[0][0]).not.toHaveProperty("enabled");
  });

  it("routes the pick to the session's host", async () => {
    render(
      <ProjectProvider>
        <SeedRemoteTab>
          <HeaderModelDialog sessionId="sess-remote" purpose="summary" />
        </SeedRemoteTab>
      </ProjectProvider>,
    );

    await waitFor(() => expect(hoisted.api.listModels).toHaveBeenCalledWith({ configured: true }, "devbox"));
    await waitFor(() => expect(hoisted.api.getCompactConfig).toHaveBeenCalledWith("devbox"));

    fireEvent.click(await screen.findByText("gpt-4o-mini"));

    await waitFor(() =>
      expect(hoisted.api.setCompactConfig).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ summary_model: "openai/gpt-4o-mini" }),
        "devbox",
      ),
    );
  });

  it("clears the model back to the auto fallback", async () => {
    hoisted.api.getCompactConfig.mockResolvedValueOnce({
      ...hoisted.STORED,
      summary_model: "openai/gpt-4o-mini",
    });

    render(<ModelDialog open onClose={vi.fn()} purpose="summary" />);
    await waitFor(() => expect(hoisted.api.listModels).toHaveBeenCalled());

    fireEvent.click(await screen.findByRole("button", { name: /clear/i }));

    // Explicit empty strings, not absent keys: only an ABSENT key is left
    // alone server-side, so the Clear must send "" to actually clear.
    await waitFor(() =>
      expect(hoisted.api.setCompactConfig).toHaveBeenCalledExactlyOnceWith(
        { summary_model: "", summary_provider: "" },
        undefined,
      ),
    );
  });

  it("defers to the owning form when one is supplied, so a form's Save is not bypassed", async () => {
    const onPick = vi.fn();
    render(<ModelDialog open onClose={vi.fn()} purpose="summary" onPick={onPick} currentValues={{ summary: "openai/gpt-4o-mini" }} />);
    await waitFor(() => expect(hoisted.api.listModels).toHaveBeenCalled());

    fireEvent.click(await screen.findByText("claude-haiku-4-5"));

    await waitFor(() => expect(onPick).toHaveBeenCalledWith("summary", "anthropic/claude-haiku-4-5", expect.anything()));
    // Writing here would bypass the form's other fields and desync it.
    expect(hoisted.api.setCompactConfig).not.toHaveBeenCalled();
  });

  it("does not fetch the stored model when the owning form already supplies it", async () => {
    render(
      <ModelDialog
        open
        onClose={vi.fn()}
        purpose="summary"
        onPick={vi.fn()}
        currentValues={{ summary: "openai/gpt-4o-mini" }}
      />,
    );
    await waitFor(() => expect(hoisted.api.listModels).toHaveBeenCalled());

    expect(hoisted.api.getCompactConfig).not.toHaveBeenCalled();
  });

  it("shows the recents/favorites sections like every other sidebar-owned picker", async () => {
    hoisted.api.listModels.mockResolvedValueOnce([
      { ...hoisted.models[0], recent: true, favorite: true },
      { ...hoisted.models[1], favorite: true },
    ]);

    render(<ModelDialog open onClose={vi.fn()} purpose="summary" />);

    // "summary" gained a sidebar row, so it now behaves like autocontinue:
    // the sections-bearing layout, not the flat provider grouping.
    await waitFor(() => expect(screen.getByText("Recently Used")).toBeInTheDocument());
    expect(screen.getByText("★ Favorites")).toBeInTheDocument();
  });
});
