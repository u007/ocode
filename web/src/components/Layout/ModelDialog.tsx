import { useState, useEffect } from "react";
import { api } from "../../api/client";
import { useChatDispatch, useChatSelector, getSessionSlice } from "../../stores/chatStore";
import type { ModelInfo } from "../../api/types";
import { advisorSelectionPayload, partitionModelSections } from "./modelSelection";
import { Search, Check, Star, X } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

/** The single model-selection purpose this dialog is opened for. Each settings
 *  field opens the dialog for exactly one purpose — there are no tabs. */
export type ModelDialogTab = "main" | "small" | "advisor" | "recap" | "ocr" | "mask" | "commit" | "summary" | "permission";

const PURPOSE_TITLES: Record<ModelDialogTab, string> = {
  main: "Select Model",
  small: "Select Small Model",
  advisor: "Select Advisor Model",
  recap: "Select Recap Model",
  ocr: "Select OCR Model",
  mask: "Select Mask Model",
  commit: "Select Commit Message Model",
  summary: "Select Summary Model",
  permission: "Select Permission Model",
};

interface Props {
  open: boolean;
  onClose: () => void;
  /** The single field this dialog selects a model for. Defaults to "main". */
  purpose?: ModelDialogTab;
  /** Called when a form-owned purpose's model is picked (recap/ocr/mask/commit/summary).
   *  The owning form persists it via its own Save; the dialog never writes
   *  these itself (writing would bypass the form's other fields and desync it). */
  onPick?: (purpose: ModelDialogTab, modelId: string, model?: ModelInfo) => void;
  /** Current value for form-owned purposes, used to highlight the active model. */
  currentValues?: Partial<Record<ModelDialogTab, string>>;
  /** Active session id. When set, a "main" model pick is scoped to that
   *  session (persisted as a per-session override) instead of the global
   *  config model, so each chat tab keeps its own model. */
  sessionId?: string;
}

export default function ModelDialog({ open, onClose, purpose = "main", onPick, currentValues, sessionId }: Props) {
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [search, setSearch] = useState("");
  // The advisor's Claude Code toggle is owned by AdvisorForm; the dialog must
  // carry the current value through a pick so the server's provider-change
  // convention (provider set → claude_code = (provider === "claude-code"))
  // cannot silently flip a toggle the user set explicitly.
  const [advisorClaudeCode, setAdvisorClaudeCode] = useState(false);
  // Model id whose favorite toggle request is in flight; its star is disabled
  // until the response resyncs, so double-clicks can't race the shared file.
  const [pendingFavorite, setPendingFavorite] = useState<string | null>(null);
  const [permissionModelState, setPermissionModelState] = useState("");
  const activeModel = useChatSelector((s) => s.model);
  const smallModel = useChatSelector((s) => s.smallModel);
  const advisorModel = useChatSelector((s) => s.advisorModel);
  // Per-session main model used to highlight the active model in the picker
  // when a session is scoped. For a real session the status snapshot's
  // main_model (the server's effective model) wins; for a draft tab the
  // locally-picked SessionSlice.model is shown until the session exists.
  const sessionMainModel = useChatSelector((s) => {
    if (!sessionId) return "";
    const slice = getSessionSlice(s, sessionId);
    return slice.tuiStatus?.main_model || slice.model || "";
  });
  const dispatch = useChatDispatch();

  useEffect(() => {
    if (open) {
      setSearch("");
      // Load the standard registry models, augmented for the permission and
      // Security & Redaction (mask) purposes with the user's enabled local/LM
      // Studio models — mirroring the TUI's permission-model and redaction-model
      // pickers, which list enabled LocalModels. The permission judge is
      // typically a local model, so it must be selectable here.
      const loadModels = async () => {
        const base = await api.listModels({ refresh: true });
        if (purpose === "mask" || purpose === "permission") {
          try {
            const local = await api.getLocalModelsConfig();
            const extra: ModelInfo[] = Object.entries(local)
              .filter(([, v]) => v.enabled)
              .map(([id]) => ({ name: id, model: id, provider: "Local Models", active: false }));
            if (extra.length > 0) setModels([...base, ...extra]);
            else setModels(base);
          } catch {
            setModels(base);
          }
        } else {
          setModels(base);
        }
      };
      loadModels().catch(console.error);
      api.getConfigModel().then((res) => {
        dispatch({ type: "SET_MODEL", model: res.model });
      }).catch(console.error);
      api.getSmallModel().then((res) => {
        dispatch({ type: "SET_SMALL_MODEL", model: res.model });
      }).catch(console.error);
      api.getAdvisor().then((res) => {
        dispatch({ type: "SET_ADVISOR_MODEL", model: res.model });
      }).catch(console.error);
      api.getAdvisorFull().then((res) => {
        setAdvisorClaudeCode(res.claude_code);
      }).catch(console.error);
      if (purpose === "permission") {
        api.getPermissionModel().then((res) => {
          setPermissionModelState(res.model ?? "");
        }).catch(console.error);
      }
    }
  }, [open, dispatch, purpose]);

  const filteredModels = models.filter(
    (m) =>
      m.name.toLowerCase().includes(search.toLowerCase()) ||
      m.model.toLowerCase().includes(search.toLowerCase()) ||
      m.provider.toLowerCase().includes(search.toLowerCase()) ||
      (m.display_name ?? "").toLowerCase().includes(search.toLowerCase())
  );

  // Mirror the TUI model picker in two capability layers, per verified TUI
  // behavior (internal/tui/picker.go + the ctrl+f handler in
  // internal/tui/model.go):
  //
  // 1. Sections (Recently Used → ★ Favorites): openModelPicker renders them
  //    and every picker kind that reuses it inherits them — "model",
  //    "small-model", "recap-model", "permission-model", "redaction-model",
  //    "autocontinue-model", "image-model". The advisor ("advisor", picker.go
  //    l.21), OCR ("ocr-model") and embedding pickers build their own lists
  //    and show NO sections. The dialog purposes mapping onto a
  //    sections-bearing kind are main/small/recap/permission/mask (commit and
  //    summary have no TUI picker at all; image-model lives in ImageGenForm).
  // 2. The favorite TOGGLE (star): the ctrl+f handler only acts on
  //    "model" / "permission-model" / "image-model", so the star is offered
  //    on main + permission here.
  const supportsSections =
    purpose === "main" ||
    purpose === "small" ||
    purpose === "recap" ||
    purpose === "permission" ||
    purpose === "mask";
  const supportsFavoriteToggle = purpose === "main" || purpose === "permission";
  const sections = supportsSections ? partitionModelSections(filteredModels) : null;
  // Flat provider grouping for purposes without favorites sections. Must NOT
  // reuse partitionModelSections here: that dedupes recent/favorite models
  // out of the provider groups, which would make them invisible when the
  // sections aren't rendered. Every model stays reachable.
  const groupedModels = filteredModels.reduce((acc, m) => {
    const provider = m.provider || "Other";
    (acc[provider] ??= []).push(m);
    return acc;
  }, {} as Record<string, ModelInfo[]>);
  const providerGroups = sections ? sections.providers : groupedModels;

  const getCurrentModel = () => {
    switch (purpose) {
      case "small":
        return smallModel;
      case "advisor":
        return advisorModel;
      case "permission":
        return currentValues?.permission ?? permissionModelState;
      default:
        // "main": when a session is scoped, highlight that session's own
        // effective model (from its per-session status snapshot) rather than
        // the global config model.
        return currentValues?.[purpose] ?? (sessionId ? sessionMainModel : activeModel);
    }
  };

  const handleSelect = (selectedModel: ModelInfo) => {
    const modelId = selectedModel.name;
    switch (purpose) {
      case "small":
        dispatch({ type: "SET_SMALL_MODEL", model: modelId });
        api.setSmallModel(modelId).catch(console.error);
        break;
      case "advisor":
        {
          const selection = advisorSelectionPayload(selectedModel);
          dispatch({ type: "SET_ADVISOR_MODEL", model: selection.model });
          onPick?.(purpose, selection.model, selectedModel);
          // Carry the current claude_code through the PUT: the server flips
          // claude_code to (provider === "claude-code") whenever provider is
          // set, which would silently disable CLI mode on any non-CLI pick.
          api.setAdvisorFull({ ...selection, claude_code: advisorClaudeCode }).catch(console.error);
        }
        break;
      case "main":
        if (sessionId && sessionId.startsWith("new-")) {
          // Draft tab — the session doesn't exist server-side yet. Keep the
          // pick local to this tab's slice; the first message sends it as the
          // request model, and the server persists it with the transcript.
          dispatch({ type: "SET_SESSION_MODEL", sessionId, model: modelId });
        } else if (sessionId) {
          // Scope the pick to this session: persist a per-session override and
          // let the server's session-tagged status broadcast update this tab's
          // sidebar — never touch the global config model or other sessions.
          // No optimistic local write: SET_TUI_STATUS replaces the whole
          // snapshot, and the authoritative push from pushSessionStatusSnapshot
          // lands on the same tab within one frame.
          api.setSessionModel(sessionId, modelId).catch((err) => {
            console.error("set session model failed", err);
            // On failure, refetch this session's status so the sidebar shows
            // the model actually in effect rather than a stale value.
            api
              .getSessionStatus(sessionId)
              .then((st) => dispatch({ type: "SET_TUI_STATUS", sessionId, status: st }))
              .catch(console.error);
          });
        } else {
          dispatch({ type: "SET_MODEL", model: modelId });
          api.setConfigModel(modelId).catch(console.error);
        }
        break;
      case "permission":
        onPick?.(purpose, modelId, selectedModel);
        // If no form owns this pick (sidebar direct trigger), persist directly.
        if (!onPick) {
          api.setPermissionModel(modelId).catch(console.error);
        }
        break;
      default:
        // Form-owned purpose (recap/ocr/mask/commit/summary): hand the pick to
        // the owning form, which persists it via its own Save.
        onPick?.(purpose, modelId, selectedModel);
        break;
    }
    onClose();
  };

  // Star toggle — the web/desktop counterpart of the TUI picker's ctrl+f.
  // Optimistically flips the row, then resyncs every star from the canonical
  // favorites list the endpoint returns; reverts on failure.
  const toggleFavorite = async (m: ModelInfo) => {
    if (pendingFavorite) return;
    const next = !m.favorite;
    setPendingFavorite(m.name);
    // Optimistic flip; resync every star from the canonical favorites list
    // the endpoint returns, and revert to the pre-click state on failure.
    setModels((prev) =>
      prev.map((x) => (x.name === m.name ? { ...x, favorite: next } : x)),
    );
    try {
      const res = await api.setModelFavorite(m.name, next);
      const favSet = new Set(res.favorites);
      setModels((prev) => prev.map((x) => ({ ...x, favorite: favSet.has(x.name) })));
    } catch (err) {
      console.error(err);
      setModels((prev) =>
        prev.map((x) =>
          x.name === m.name ? { ...x, favorite: m.favorite ?? false } : x,
        ),
      );
    } finally {
      setPendingFavorite(null);
    }
  };

  const handleClear = () => {
    switch (purpose) {
      case "small":
        dispatch({ type: "SET_SMALL_MODEL", model: "" });
        api.setSmallModel("auto").catch(console.error);
        break;
      case "advisor":
        dispatch({ type: "SET_ADVISOR_MODEL", model: "" });
        onPick?.(purpose, "");
        api.setAdvisorFull({ model: "", provider: "", claude_code: advisorClaudeCode }).catch(console.error);
        break;
      case "main":
        if (sessionId && sessionId.startsWith("new-")) {
          dispatch({ type: "SET_SESSION_MODEL", sessionId, model: undefined });
        } else if (sessionId) {
          api.clearSessionModel(sessionId).catch((err) => {
            console.error("clear session model failed", err);
            api
              .getSessionStatus(sessionId)
              .then((st) => dispatch({ type: "SET_TUI_STATUS", sessionId, status: st }))
              .catch(console.error);
          });
        } else {
          dispatch({ type: "SET_MODEL", model: "" });
          api.setConfigModel("").catch(console.error);
        }
        break;
      case "permission":
        onPick?.(purpose, "");
        if (!onPick) {
          api.setPermissionModel("").catch(console.error);
        }
        break;
      default:
        // Form-owned purpose: hand empty string to the owning form.
        onPick?.(purpose, "");
        break;
    }
    onClose();
  };

  // One model row: the select button plus, on favorites-toggle purposes
  // (main/permission — the TUI kinds whose ctrl+f handler acts), the favorite
  // star. The star is a sibling of the select button — clicking it toggles
  // the favorite without selecting the model, mirroring ctrl+f in the TUI.
  const renderRow = (m: ModelInfo) => {
    const selected =
      getCurrentModel() === (purpose === "advisor" ? m.model : m.name) || m.active;
    // Only canonical "provider/model" ids can enter the shared favorites
    // state (the server validates the same way); pseudo-rows like the
    // locally-added LM Studio entries never show a star.
    const canFavorite = supportsFavoriteToggle && m.name.includes("/");
    return (
      <div key={m.name} className="flex items-start gap-1">
        <button
          onClick={() => handleSelect(m)}
          className={`w-full min-w-0 flex items-start justify-between gap-2 px-3 py-2 rounded-md text-sm text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background ${
            selected ? "bg-blue-600/20 text-blue-400" : "text-foreground hover:bg-muted"
          }`}
        >
          <span className="min-w-0 flex-1 whitespace-normal break-words [overflow-wrap:anywhere]">
            {m.display_name && m.display_name !== m.model
              ? `${m.display_name} (${m.model})`
              : m.model}
          </span>
          {selected && <Check className="mt-0.5 h-4 w-4 shrink-0 text-blue-400" />}
        </button>
        {canFavorite && (
          <button
            onClick={() => toggleFavorite(m)}
            disabled={pendingFavorite === m.name}
            aria-label={m.favorite ? `Unfavorite ${m.name}` : `Favorite ${m.name}`}
            title={m.favorite ? "Remove from favorites" : "Add to favorites"}
            className={`mt-1.5 shrink-0 p-1 rounded-md transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
              m.favorite
                ? "text-yellow-400 hover:text-yellow-300"
                : "text-muted-foreground/50 hover:text-yellow-400"
            }`}
          >
            <Star className="h-4 w-4" fill={m.favorite ? "currentColor" : "none"} />
          </button>
        )}
      </div>
    );
  };

  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="sm:max-w-2xl bg-card border-border">
        <DialogHeader>
          <DialogTitle className="text-foreground">{PURPOSE_TITLES[purpose]}</DialogTitle>
        </DialogHeader>

        {/* Search */}
        <div className="relative">
          <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <input
            type="text"
            placeholder="Search models..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full pl-10 pr-4 py-2 bg-muted border border-border rounded-md text-sm text-foreground placeholder-muted-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            autoFocus
          />
        </div>

        {/* Clear button */}
        <button
          onClick={handleClear}
          className="w-full flex items-center justify-center gap-2 px-3 py-2 rounded-md text-sm text-muted-foreground hover:text-foreground hover:bg-muted transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
        >
          <X className="h-4 w-4" />
          Clear (not set)
        </button>

        {/* Model list — for favorites-capable purposes: Recently Used,
            ★ Favorites, then provider groups, mirroring the TUI picker
            (internal/tui/picker.go openModelPicker). Others: provider groups. */}
        <div className="max-h-96 overflow-y-auto">
          {sections && sections.recents.length > 0 && (
            <div className="mb-4">
              <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                Recently Used
              </div>
              {sections.recents.map(renderRow)}
            </div>
          )}
          {sections && sections.favorites.length > 0 && (
            <div className="mb-4">
              <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                ★ Favorites
              </div>
              {sections.favorites.map(renderRow)}
            </div>
          )}
          {Object.entries(providerGroups).map(([provider, providerModels]) => (
            <div key={provider} className="mb-4">
              <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                {provider}
              </div>
              {providerModels.map(renderRow)}
            </div>
          ))}
          {filteredModels.length === 0 && (
            <div className="text-center py-8 text-muted-foreground text-sm">
              No models found
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
