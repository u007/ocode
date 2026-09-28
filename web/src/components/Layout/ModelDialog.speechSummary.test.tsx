import { describe, expect, it, vi, beforeEach } from "vitest";
import { useEffect } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import ModelDialog from "./ModelDialog";
import { ProjectProvider, useProjectDispatch } from "../../stores/projectStore";
import { useSessionHost } from "../../hooks/useSessionHost";
import type { SpeechSummaryConfig } from "../../api/client";
import type { ModelInfo } from "../../api/types";

// Mirrors ModelDialog.summary.test.tsx's harness. "speechsummary" is a
// SEPARATE purpose from "summary": "summary" is the COMPACTION model that
// CompactForm still owns, and mixing them would write a pick to the wrong
// config. These tests pin the separation as much as the feature itself.
const hoisted = vi.hoisted(() => {
  const models: ModelInfo[] = [
    { name: "anthropic/claude-haiku-4-5", model: "claude-haiku-4-5", provider: "anthropic", active: false },
    { name: "openai/gpt-4o-mini", model: "gpt-4o-mini", provider: "openai", active: false },
  ];
  const STORED: SpeechSummaryConfig = { enabled: true, model: "" };
  const api = {
    listModels: vi.fn(async () => models.map((m) => ({ ...m }))),
    getConfigModel: vi.fn(async () => ({ model: "" })),
    getSmallModel: vi.fn(async () => ({ model: "" })),
    getAdvisor: vi.fn(async () => ({ model: "" })),
    getLocalModelsConfig: vi.fn(async () => ({})),
    getSpeechSummaryConfig: vi.fn(async (): Promise<SpeechSummaryConfig> => ({ ...STORED })),
    setSpeechSummaryConfig: vi.fn(
      async (patch: Partial<SpeechSummaryConfig>): Promise<SpeechSummaryConfig> => ({ ...STORED, ...patch }),
    ),
    // Present so a test can PROVE this purpose never touches compaction.
    getCompactConfig: vi.fn(async () => ({ summary_model: "should/not/be/used" })),
    setCompactConfig: vi.fn(async () => ({})),
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
  useChatDispatch: () => hoisted.dispatchSpy,
  getSessionSlice: () => ({ tuiStatus: { main_model: "" } }),
}));

const remoteProject = {
  path: "/remote",
  name: "remote",
  host: "devbox",
  added_at: "",
  last_used_at: "",
  order: 1,
  group: "",
};

function HeaderModelDialog({
  sessionId,
  purpose,
}: {
  sessionId: string;
  purpose?: import("./ModelDialog").ModelDialogTab;
}) {
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

const row = (id: string) => document.querySelector<HTMLElement>(`[data-list-nav-id="${id}"]`);

describe("ModelDialog speechsummary purpose", () => {
  it("titled so the user can tell it apart from the compaction picker", async () => {
    render(<ModelDialog open onClose={vi.fn()} purpose="speechsummary" />);
    // Two "Summary Model" dialogs in one product is a support burden; the title
    // is the only place the difference is visible before picking.
    expect(await screen.findByText("Select Speech Summary Model")).toBeInTheDocument();
  });

  it("reads the stored speech-summary model on open so the active row is highlighted", async () => {
    hoisted.api.getSpeechSummaryConfig.mockResolvedValueOnce({
      enabled: true,
      model: "anthropic/claude-haiku-4-5",
    });

    render(<ModelDialog open onClose={vi.fn()} purpose="speechsummary" />);

    await waitFor(() => expect(hoisted.api.getSpeechSummaryConfig).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(row("anthropic:anthropic/claude-haiku-4-5")?.querySelector(".lucide-check")).not.toBeNull(),
    );
    expect(row("openai:openai/gpt-4o-mini")?.querySelector(".lucide-check")).toBeNull();
  });

  it("persists a pick carrying ONLY the model, so the on/off gate is untouched", async () => {
    render(<ModelDialog open onClose={vi.fn()} purpose="speechsummary" />);
    await waitFor(() => expect(hoisted.api.listModels).toHaveBeenCalled());

    fireEvent.click(await screen.findByText("gpt-4o-mini"));

    await waitFor(() =>
      expect(hoisted.api.setSpeechSummaryConfig).toHaveBeenCalledExactlyOnceWith(
        { model: "openai/gpt-4o-mini" },
        undefined,
      ),
    );
    // A single-key body: the gate is a separate control and must survive.
    expect(Object.keys(hoisted.api.setSpeechSummaryConfig.mock.calls[0][0])).toEqual(["model"]);
  });

  it("never touches the compaction config", async () => {
    // The single most important assertion here: "summary" and "speechsummary"
    // both look like "the summary model" but write different blocks.
    render(<ModelDialog open onClose={vi.fn()} purpose="speechsummary" />);
    await waitFor(() => expect(hoisted.api.listModels).toHaveBeenCalled());
    fireEvent.click(await screen.findByText("gpt-4o-mini"));
    await waitFor(() => expect(hoisted.api.setSpeechSummaryConfig).toHaveBeenCalled());

    expect(hoisted.api.getCompactConfig).not.toHaveBeenCalled();
    expect(hoisted.api.setCompactConfig).not.toHaveBeenCalled();
  });

  it("clears with an explicit empty string, not an absent key", async () => {
    hoisted.api.getSpeechSummaryConfig.mockResolvedValueOnce({
      enabled: true,
      model: "openai/gpt-4o-mini",
    });
    render(<ModelDialog open onClose={vi.fn()} purpose="speechsummary" />);
    await waitFor(() => expect(hoisted.api.getSpeechSummaryConfig).toHaveBeenCalled());
    await waitFor(() =>
      expect(row("openai:openai/gpt-4o-mini")?.querySelector(".lucide-check")).not.toBeNull(),
    );

    fireEvent.click(screen.getByRole("button", { name: /Clear \(not set\)/ }));

    // undefined would be dropped by JSON.stringify and the stored model would
    // survive the "clear" — the exact opposite of what the user asked for.
    await waitFor(() =>
      expect(hoisted.api.setSpeechSummaryConfig).toHaveBeenCalledExactlyOnceWith({ model: "" }, undefined),
    );
  });

  it("honours a caller-supplied value without reading the stored block", async () => {
    // The owning-form contract: a value passed in is authoritative.
    render(
      <ModelDialog
        open
        onClose={vi.fn()}
        purpose="speechsummary"
        onPick={vi.fn()}
        currentValues={{ speechsummary: "openai/gpt-4o-mini" }}
      />,
    );
    await waitFor(() => expect(hoisted.api.listModels).toHaveBeenCalled());

    expect(hoisted.api.getSpeechSummaryConfig).not.toHaveBeenCalled();
  });

  it("hands the pick to onPick when a form owns the field", async () => {
    const onPick = vi.fn();
    render(
      <ModelDialog
        open
        onClose={vi.fn()}
        purpose="speechsummary"
        onPick={onPick}
        currentValues={{ speechsummary: "" }}
      />,
    );
    await waitFor(() => expect(hoisted.api.listModels).toHaveBeenCalled());

    fireEvent.click(await screen.findByText("gpt-4o-mini"));

    expect(onPick).toHaveBeenCalledWith("speechsummary", "openai/gpt-4o-mini", expect.anything());
    // The form owns persistence, so the dialog must not also write.
    expect(hoisted.api.setSpeechSummaryConfig).not.toHaveBeenCalled();
  });

  it("shows the recents/favorites sections like every other sidebar-owned picker", async () => {
    hoisted.api.listModels.mockResolvedValueOnce([
      { ...hoisted.models[0], recent: true, favorite: true },
      { ...hoisted.models[1], favorite: true },
    ]);

    render(<ModelDialog open onClose={vi.fn()} purpose="speechsummary" />);

    await waitFor(() => expect(screen.getByText("Recently Used")).toBeInTheDocument());
    expect(screen.getByText("★ Favorites")).toBeInTheDocument();
  });

  it("reads and writes the REMOTE host, not the local server", async () => {
    render(
      <ProjectProvider>
        <SeedRemoteTab>
          <HeaderModelDialog sessionId="sess-remote" purpose="speechsummary" />
        </SeedRemoteTab>
      </ProjectProvider>,
    );
    await waitFor(() => expect(hoisted.api.listModels).toHaveBeenCalled());

    // A remote project's speech summarises on the host; reading or writing the
    // local block would leave the remote session on the wrong model.
    await waitFor(() => expect(hoisted.api.getSpeechSummaryConfig).toHaveBeenCalledWith("devbox"));

    fireEvent.click(await screen.findByText("gpt-4o-mini"));
    await waitFor(() =>
      expect(hoisted.api.setSpeechSummaryConfig).toHaveBeenCalledWith(
        { model: "openai/gpt-4o-mini" },
        "devbox",
      ),
    );
  });
});
