import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import ModelDialog from "./ModelDialog";
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
  };
  const dispatchSpy = vi.fn();
  return { models, api, dispatchSpy };
});

vi.mock("../../api/client", () => ({ api: hoisted.api }));
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
    // Recently Used (getAllByText would also match nothing else).
    expect(screen.getAllByText("claude-a")).toHaveLength(1);
    // Favorites/regular models are not duplicated across sections: gpt-b is
    // only in ★ Favorites, gpt-c only in the openai provider group.
    expect(screen.getAllByText("gpt-b")).toHaveLength(1);
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
    expect(screen.getByText("gpt-b").closest("button")?.textContent).not.toContain("tuned");
    expect(screen.getByText("compound").closest("button")?.textContent).not.toContain("tuned");
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

  it("renders the priority sections without star toggles for small/recap/mask (TUI reuses openModelPicker, ctrl+f does not act)", async () => {
    for (const purpose of ["small", "recap", "mask"] as const) {
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
      await waitFor(() => expect(screen.getByText("gpt-b")).toBeInTheDocument());

      fireEvent.click(screen.getByText("gpt-b"));

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
