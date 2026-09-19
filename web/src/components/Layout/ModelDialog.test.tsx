import { describe, expect, it, vi, beforeEach } from "vitest";
import { useEffect } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import ModelDialog from "./ModelDialog";
import { ProjectProvider, useProjectDispatch } from "../../stores/projectStore";
import { useSessionHost } from "../../hooks/useSessionHost";
import type { ModelInfo } from "../../api/types";

// Fixtures in the exact order GET /api/models returns them: Recently Used
// first, then favorites, then provider-sorted remainder (see HandleListModels
// in internal/server/handler.go).
const hoisted = vi.hoisted(() => {
  const models: ModelInfo[] = [
    // Both recent and favorite — must appear only under Recently Used, but
    // keep its star lit (raw membership, mirroring TUI ctrl+f/IsFavorite).
    { name: "anthropic/claude-a", model: "claude-a", provider: "anthropic", active: false, recent: true, favorite: true, has_kaizen: true },
    { name: "openai/gpt-b", model: "gpt-b", provider: "openai", active: false, favorite: true },
    { name: "openai/gpt-c", model: "gpt-c", provider: "openai", active: false, has_model_prompt: true },
    { name: "groq/compound", model: "compound", provider: "groq", active: false },
  ];
  const api = {
    listModels: vi.fn(async () => models.map((m) => ({ ...m }))),
    getConfigModel: vi.fn(async () => ({ model: "" })),
    getSmallModel: vi.fn(async () => ({ model: "" })),
    getAdvisor: vi.fn(async () => ({ model: "" })),
    getAdvisorFull: vi.fn(async () => ({ claude_code: false })),
    setModelFavorite: vi.fn(async (m: string, fav: boolean) => ({
      model: m,
      favorite: fav,
      favorites: fav
        ? ["anthropic/claude-a", "openai/gpt-b", m]
        : ["anthropic/claude-a"],
    })),
    setConfigModel: vi.fn(async () => ({})),
    getLocalModelsConfig: vi.fn(async () => ({})),
    getPermissionModel: vi.fn(async () => ({ model: "" })),
    setPermissionModel: vi.fn(async () => ({})),
    setSessionModel: vi.fn(async () => ({ model: "", session_id: "" })),
    clearSessionModel: vi.fn(async () => ({ model: "", session_id: "" })),
    getSessionStatus: vi.fn(async () => ({ main_model: "" })),
    // Auto-continue judge picker (purpose="autocontinue").
    getAutoContinue: vi.fn(async () => ({ enabled: false, model: "" })),
    setAutoContinue: vi.fn(async (_fields: { enabled?: boolean; model?: string; clear?: boolean }) => ({
      enabled: false,
      model: "",
    })),
    // ProjectProvider fires these on mount.
    listProjects: vi.fn(async (): Promise<unknown[]> => []),
    getCurrentProject: vi.fn(async () => null),
    listProjectSessions: vi.fn(async () => []),
    listGroups: vi.fn(async () => []),
    getTabs: vi.fn(async () => ({ projects: {} })),
    setTabs: vi.fn(async () => ({ status: "ok" })),
  };
  const dispatchSpy = vi.fn();
  return { models, api, dispatchSpy };
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

beforeEach(() => {
  vi.clearAllMocks();
});

describe("ModelDialog favorites/recents sections", () => {
  it("renders Recently Used and ★ Favorites above provider groups with TUI dedupe", async () => {
    render(<ModelDialog open onClose={vi.fn()} />);

    await waitFor(() => expect(screen.getByText("Recently Used")).toBeInTheDocument());
    expect(screen.getByText("★ Favorites")).toBeInTheDocument();

    // A model that is both recent and favorite appears exactly once, under
    // Recently Used with its full provider/model id (matching TUI's
    // modelPickerLabel), while provider groups show the bare model.
    expect(screen.getByText("anthropic/claude-a")).toBeInTheDocument();
    expect(screen.getAllByText("anthropic/claude-a")).toHaveLength(1);
    // gpt-b is favorite-only so it renders in ★ Favorites with provider prefix.
    expect(screen.getByText("openai/gpt-b")).toBeInTheDocument();
    expect(screen.getAllByText("openai/gpt-b")).toHaveLength(1);
    // gpt-c is provider-grouped, so it shows the bare model under the openai header.
    expect(screen.getByText("gpt-c")).toBeInTheDocument();
    expect(screen.getAllByText("gpt-c")).toHaveLength(1);

    // Radix dialogs portal to document.body, so section order must be read
    // from the document, not the render container.
    const text = document.body.textContent ?? "";
    expect(text.indexOf("Recently Used")).toBeLessThan(text.indexOf("★ Favorites"));
    expect(text.indexOf("★ Favorites")).toBeLessThan(text.indexOf("openai"));
  });

  it("star click toggles the favorite without selecting or closing the dialog", async () => {
    const onClose = vi.fn();
    render(<ModelDialog open onClose={onClose} />);
    await waitFor(() => expect(screen.getByText("Recently Used")).toBeInTheDocument());

    // The recent+favorite row shows a lit star (aria-label says Unfavorite).
    const litStar = screen.getByLabelText("Unfavorite anthropic/claude-a");
    expect(litStar).toBeInTheDocument();

    // Favorite a plain model from the provider groups.
    fireEvent.click(screen.getByLabelText("Favorite openai/gpt-c"));
    await waitFor(() =>
      expect(hoisted.api.setModelFavorite).toHaveBeenCalledWith("openai/gpt-c", true),
    );
    // Star resyncs to the favorited state; dialog stays open (mirrors the
    // TUI picker where ctrl+f refreshes items in place).
    await waitFor(() =>
      expect(screen.getByLabelText("Unfavorite openai/gpt-c")).toBeInTheDocument(),
    );
    expect(onClose).not.toHaveBeenCalled();
    expect(hoisted.api.setConfigModel).not.toHaveBeenCalled();

    // Unfavorite it again.
    fireEvent.click(screen.getByLabelText("Unfavorite openai/gpt-c"));
    await waitFor(() =>
      expect(hoisted.api.setModelFavorite).toHaveBeenCalledWith("openai/gpt-c", false),
    );
    await waitFor(() =>
      expect(screen.getByLabelText("Favorite openai/gpt-c")).toBeInTheDocument(),
    );
  });

  it("badges tuned models (custom prompt / kaizen) with a 'tuned' marker", async () => {
    render(<ModelDialog open onClose={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("Recently Used")).toBeInTheDocument());

    // claude-a (has_kaizen) and gpt-c (has_model_prompt) get the badge; the
    // other rows must not (Radix portals rows to document.body).
    expect(screen.getAllByText("tuned")).toHaveLength(2);
    const tuned = screen.getAllByText("tuned");
    for (const el of tuned) {
      expect(el.closest("button")?.textContent).toMatch(/claude-a|gpt-c/);
    }
    // gpt-b lives in ★ Favorites with provider prefix, compound in provider group bare.
    expect(screen.getByText("openai/gpt-b").closest("button")?.textContent).not.toContain("tuned");
    expect(screen.getByText("compound").closest("button")?.textContent).not.toContain("tuned");
  });

  it("shows provider-qualified names in Recently Used / Favorites and keeps display_name provider-prefixed", async () => {
    const displayModels: ModelInfo[] = [
      { name: "anthropic/claude-a", model: "claude-a", provider: "anthropic", active: false, recent: true, display_name: "Claude A" },
      { name: "openai/gpt-b", model: "gpt-b", provider: "openai", active: false, favorite: true, display_name: "GPT B" },
      // Guard: display_name === model should not duplicate parenthesis — falls back to base label.
      { name: "groq/compound", model: "compound", provider: "groq", active: false, display_name: "compound" },
      // Guard: display_name === name should not duplicate — falls back to base label.
      { name: "openai/gpt-c", model: "gpt-c", provider: "openai", active: false, display_name: "openai/gpt-c" },
      // Pseudo local model without provider prefix — should render bare without crashing.
      { name: "my-local", model: "my-local", provider: "", active: false, recent: true },
    ];
    hoisted.api.listModels.mockResolvedValueOnce(displayModels.map((m) => ({ ...m })));
    render(<ModelDialog open onClose={vi.fn()} />);

    await waitFor(() => expect(screen.getByText("Recently Used")).toBeInTheDocument());
    // Recently Used: provider-qualified even with display_name
    expect(screen.getByText("Claude A (anthropic/claude-a)")).toBeInTheDocument();
    expect(screen.getByText("my-local")).toBeInTheDocument();
    // Favorites: provider-qualified with display_name
    expect(screen.getByText("GPT B (openai/gpt-b)")).toBeInTheDocument();
    // Guard cases: provider groups show bare model, no parenthetical duplication
    expect(screen.getByText("compound")).toBeInTheDocument();
    expect(screen.queryByText("compound (compound)")).toBeNull();
    // gpt-c is provider-grouped with display_name === name, guard keeps bare label
    const gptCBtn = screen.getByText("gpt-c").closest("button")?.textContent ?? "";
    expect(gptCBtn).not.toContain("(openai/gpt-c)");
    expect(gptCBtn).not.toContain("(gpt-c)");
  });

  it("keeps the flat provider list for purposes the TUI does not offer favorites on", async () => {
    render(<ModelDialog open onClose={vi.fn()} purpose="advisor" />);
    await waitFor(() => expect(hoisted.api.listModels).toHaveBeenCalled());

    expect(screen.queryByText("Recently Used")).toBeNull();
    expect(screen.queryByText("★ Favorites")).toBeNull();
    // Provider grouping still renders.
    expect(screen.getAllByText("gpt-c").length).toBeGreaterThan(0);
    expect(screen.queryByLabelText(/Favorite|Unfavorite/)).toBeNull();
  });

  it("renders the priority sections without star toggles for small/recap/mask/autocontinue (TUI reuses openModelPicker, ctrl+f does not act)", async () => {
    for (const purpose of ["small", "recap", "mask", "autocontinue"] as const) {
      const { unmount } = render(
        <ModelDialog open onClose={vi.fn()} purpose={purpose} />,
      );
      await waitFor(() =>
        expect(screen.getByText("Recently Used")).toBeInTheDocument(),
      );
      expect(screen.getByText("★ Favorites")).toBeInTheDocument();
      // Sections render, but the star is not offered — matching the TUI
      // ctrl+f handler (model/permission/image kinds only).
      expect(screen.queryAllByLabelText(/avorite /)).toHaveLength(0);
      unmount();
    }
  });

  it("permission picker offers both the sections and the star toggle", async () => {
    render(<ModelDialog open onClose={vi.fn()} purpose="permission" />);
    await waitFor(() => expect(screen.getByText("Recently Used")).toBeInTheDocument());
    expect(screen.getByText("★ Favorites")).toBeInTheDocument();
    expect(screen.getByLabelText("Unfavorite anthropic/claude-a")).toBeInTheDocument();
    expect(screen.getByLabelText("Favorite openai/gpt-c")).toBeInTheDocument();
  });

  describe("main model scoping (per-chat-session model)", () => {
    it("routes a main-model pick to the session endpoint, not the global config", async () => {
      render(<ModelDialog open onClose={vi.fn()} purpose="main" sessionId="ses_123" />);
      await waitFor(() => expect(screen.getByText("gpt-c")).toBeInTheDocument());

      fireEvent.click(screen.getByText("gpt-c"));

      await waitFor(() =>
        expect(hoisted.api.setSessionModel).toHaveBeenCalledWith("ses_123", "openai/gpt-c"),
      );
      // The global config model must not be touched — that's what leaked the
      // pick across every other open session tab.
      expect(hoisted.api.setConfigModel).not.toHaveBeenCalled();
    });

    it("keeps a draft tab's pick local to the tab (SET_SESSION_MODEL) — no API calls", async () => {
      render(<ModelDialog open onClose={vi.fn()} purpose="main" sessionId="new-1700000000" />);
      await waitFor(() => expect(screen.getByText("openai/gpt-b")).toBeInTheDocument());

      fireEvent.click(screen.getByText("openai/gpt-b"));

      expect(hoisted.dispatchSpy).toHaveBeenCalledWith({
        type: "SET_SESSION_MODEL",
        sessionId: "new-1700000000",
        model: "openai/gpt-b",
      });
      expect(hoisted.api.setSessionModel).not.toHaveBeenCalled();
      expect(hoisted.api.setConfigModel).not.toHaveBeenCalled();
    });

    it("clearing on a real session calls the session-scoped DELETE only", async () => {
      render(<ModelDialog open onClose={vi.fn()} purpose="main" sessionId="ses_9" />);
      await waitFor(() => expect(screen.getByText("gpt-c")).toBeInTheDocument());

      fireEvent.click(screen.getByText("Clear (not set)"));

      await waitFor(() => expect(hoisted.api.clearSessionModel).toHaveBeenCalledWith("ses_9"));
      expect(hoisted.api.setConfigModel).not.toHaveBeenCalled();
    });

    it("without a session context falls back to the global config model", async () => {
      render(<ModelDialog open onClose={vi.fn()} purpose="main" />);
      await waitFor(() => expect(screen.getByText("gpt-c")).toBeInTheDocument());

      fireEvent.click(screen.getByText("gpt-c"));

      await waitFor(() => expect(hoisted.api.setConfigModel).toHaveBeenCalledWith("openai/gpt-c"));
      expect(hoisted.api.setSessionModel).not.toHaveBeenCalled();
    });
  });
});

// A remote project's session header must resolve the session's host with
// `useSessionHost` and hand it to the model dialog, so the model list and the
// per-session pick come from that host's server rather than the local one.
const remoteProject = {
  path: "/remote",
  name: "remote",
  host: "devbox",
  added_at: "",
  last_used_at: "",
  order: 1,
  group: "",
};

/** Mirrors App's header wiring: resolve the active session's host, pass it in. */
function HeaderModelDialog({ sessionId }: { sessionId: string }) {
  const host = useSessionHost(sessionId);
  return <ModelDialog open onClose={vi.fn()} sessionId={sessionId} host={host} />;
}

/** Register the remote project and bind the session to a tab under it. */
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

describe("ModelDialog remote session host", () => {
  beforeEach(() => {
    hoisted.api.listModels.mockClear();
    hoisted.api.setSessionModel.mockClear();
    hoisted.api.listProjects.mockResolvedValue([remoteProject]);
  });

  it("loads a remote session's model list from that host and scopes the pick there", async () => {
    render(
      <ProjectProvider>
        <SeedRemoteTab>
          <HeaderModelDialog sessionId="sess-remote" />
        </SeedRemoteTab>
      </ProjectProvider>,
    );

    await waitFor(() =>
      expect(hoisted.api.listModels).toHaveBeenCalledWith({ configured: true }, "devbox"),
    );

    fireEvent.click(await screen.findByText("gpt-c"));
    await waitFor(() =>
      expect(hoisted.api.setSessionModel).toHaveBeenCalledWith("sess-remote", "openai/gpt-c", "devbox"),
    );
  });

  it("toggles a favorite on the session's host, not the local machine", async () => {
    render(
      <ProjectProvider>
        <SeedRemoteTab>
          <HeaderModelDialog sessionId="sess-remote" />
        </SeedRemoteTab>
      </ProjectProvider>,
    );

    await waitFor(() => expect(hoisted.api.listModels).toHaveBeenCalledWith({ configured: true }, "devbox"));

    // The displayed star state comes from the host's list, so the write must
    // land on the host's model.json too — a local write would corrupt it.
    fireEvent.click(await screen.findByLabelText("Favorite openai/gpt-c"));
    await waitFor(() =>
      expect(hoisted.api.setModelFavorite).toHaveBeenCalledWith("openai/gpt-c", true, "devbox"),
    );
  });
});

// ── Auto-continue judge-model purpose (web counterpart of the TUI's
// kind="autocontinue-model" picker) ─────────────────────────────────────────
describe("ModelDialog autocontinue purpose", () => {
  it("shows the auto-continue title and merges enabled local models into the list", async () => {
    hoisted.api.getLocalModelsConfig.mockResolvedValueOnce({
      "bonsai-8b": { enabled: true },
      "disabled-8b": { enabled: false },
    });
    render(<ModelDialog open onClose={vi.fn()} purpose="autocontinue" />);

    await waitFor(() => expect(screen.getByText("Select Auto-Continue Judge Model")).toBeInTheDocument());
    // Enabled local models join the registry entries; disabled ones don't.
    await waitFor(() => expect(screen.getByText("bonsai-8b")).toBeInTheDocument());
    expect(screen.queryByText("disabled-8b")).toBeNull();
    // The current judge model is fetched so the active row can highlight.
    await waitFor(() => expect(hoisted.api.getAutoContinue).toHaveBeenCalled());
  });

  it("a pick with no owning form persists via setAutoContinue({model}) without touching the gate", async () => {
    render(<ModelDialog open onClose={vi.fn()} purpose="autocontinue" />);
    await waitFor(() => expect(screen.getByText("gpt-c")).toBeInTheDocument());

    fireEvent.click(screen.getByText("gpt-c"));

    await waitFor(() =>
      expect(hoisted.api.setAutoContinue).toHaveBeenCalledWith({ model: "openai/gpt-c" }),
    );
    // The enabled/model write shape matters: no `enabled` key, so the gate is untouched.
    const arg = hoisted.api.setAutoContinue.mock.calls[0][0];
    expect(Object.keys(arg)).toEqual(["model"]);
  });

  it("clear calls setAutoContinue({clear:true}) — judge model cleared, gate untouched", async () => {
    render(<ModelDialog open onClose={vi.fn()} purpose="autocontinue" />);
    await waitFor(() => expect(screen.getByText("gpt-c")).toBeInTheDocument());

    fireEvent.click(screen.getByText("Clear (not set)"));

    await waitFor(() =>
      expect(hoisted.api.setAutoContinue).toHaveBeenCalledWith({ clear: true }),
    );
  });
});

// ── Model-list loading (open must be instant; live fetch is explicit) ──────
describe("ModelDialog model-list loading", () => {
  it("opens from the cached list and never blocks on a live refresh", async () => {
    render(<ModelDialog open onClose={vi.fn()} />);

    await waitFor(() =>
      expect(hoisted.api.listModels).toHaveBeenCalledWith({ configured: true }),
    );
    // A refresh on open is a multi-second network round trip — must not happen.
    expect(hoisted.api.listModels).not.toHaveBeenCalledWith(expect.objectContaining({ refresh: true }));
    await waitFor(() => expect(screen.getByText("Recently Used")).toBeInTheDocument());
  });

  it("Refresh fetches live provider lists and updates the rows", async () => {
    render(<ModelDialog open onClose={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("Recently Used")).toBeInTheDocument());

    hoisted.api.listModels.mockResolvedValueOnce([
      { name: "openai/gpt-live", model: "gpt-live", provider: "openai", active: false },
    ]);
    fireEvent.click(screen.getByLabelText("Refresh model list"));

    await waitFor(() =>
      expect(hoisted.api.listModels).toHaveBeenCalledWith({ refresh: true, configured: true }),
    );
    await waitFor(() => expect(screen.getByText("gpt-live")).toBeInTheDocument());
  });
});

describe("ModelDialog provider render cap", () => {
  it("caps the unfiltered provider list and points at search", async () => {
    // 6 providers × 100 = 600 > MAX_VISIBLE_PROVIDER_MODELS (500), so the last
    // provider is dropped entirely and the hint reports the hidden count.
    const many: ModelInfo[] = [];
    for (let p = 0; p < 6; p++) {
      for (let m = 0; m < 100; m++) {
        many.push({
          name: `p${p}/p${p}-m${m}`,
          model: `p${p}-m${m}`,
          provider: `p${p}`,
          active: false,
        });
      }
    }
    hoisted.api.listModels.mockResolvedValueOnce(many);
    render(<ModelDialog open onClose={vi.fn()} />);

    await waitFor(() =>
      expect(screen.getByText(/100 more models not shown/)).toBeInTheDocument(),
    );
    // The last provider's rows are beyond the budget and are not mounted.
    expect(screen.queryByText("p5-m0")).toBeNull();
    // The first provider's rows are present.
    expect(screen.getAllByText("p0-m0").length).toBeGreaterThan(0);
  });

  it("keeps enabled local judge models selectable even when the registry fills the cap", async () => {
    // 6 providers × 100 = 600 > 500: without the Local Models exemption the
    // client-appended local group would be beyond the budget and unmounted.
    const many: ModelInfo[] = [];
    for (let p = 0; p < 6; p++) {
      for (let m = 0; m < 100; m++) {
        many.push({
          name: `p${p}/p${p}-m${m}`,
          model: `p${p}-m${m}`,
          provider: `p${p}`,
          active: false,
        });
      }
    }
    hoisted.api.listModels.mockResolvedValueOnce(many);
    hoisted.api.getLocalModelsConfig.mockResolvedValueOnce({ "bonsai-8b": { enabled: true } });
    render(<ModelDialog open onClose={vi.fn()} purpose="permission" />);

    await waitFor(() => expect(screen.getByText("bonsai-8b")).toBeInTheDocument());
    // The cap still trimmed the registry.
    expect(screen.queryByText("p5-m0")).toBeNull();
  });
});

describe("ModelDialog all-providers toggle", () => {
  const unconfigured: ModelInfo = {
    name: "nano-gpt/only-unconfigured",
    model: "only-unconfigured",
    provider: "nano-gpt",
    active: false,
  };

  it("defaults to configured-only, loads the full registry when toggled on, and resets off on reopen", async () => {
    const { rerender } = render(<ModelDialog open onClose={vi.fn()} />);
    // Default: opened with configured=true and the unconfigured row is absent.
    await waitFor(() =>
      expect(hoisted.api.listModels).toHaveBeenCalledWith({ configured: true }),
    );
    expect(screen.getByLabelText("Show all providers")).not.toBeChecked();
    expect(screen.queryByText("only-unconfigured")).toBeNull();

    // Toggle on: no configured param, full list rendered.
    hoisted.api.listModels.mockResolvedValueOnce([
      ...hoisted.models.map((m) => ({ ...m })),
      unconfigured,
    ]);
    fireEvent.click(screen.getByLabelText("Show all providers"));
    await waitFor(() =>
      expect(hoisted.api.listModels).toHaveBeenCalledWith({}),
    );
    await waitFor(() => expect(screen.getByText("only-unconfigured")).toBeInTheDocument());

    // Close and reopen: the toggle resets to off (default) and the list is
    // requested configured-only again.
    hoisted.api.listModels.mockClear();
    rerender(<ModelDialog open={false} onClose={vi.fn()} />);
    rerender(<ModelDialog open onClose={vi.fn()} />);
    await waitFor(() =>
      expect(hoisted.api.listModels).toHaveBeenCalledWith({ configured: true }),
    );
    expect(screen.getByLabelText("Show all providers")).not.toBeChecked();
    expect(screen.queryByText("only-unconfigured")).toBeNull();
  });
});
