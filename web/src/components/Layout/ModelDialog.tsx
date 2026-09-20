import { useState, useEffect, useMemo, useCallback, useRef } from "react";
import { api } from "../../api/client";
import { useChatDispatch, useChatSelector, getSessionSlice } from "../../stores/chatStore";
import type { ModelInfo } from "../../api/types";
import { advisorSelectionPayload, capProviderGroups, LOCAL_MODELS_PROVIDER, LOCAL_MODELS_UNCAPPED, partitionModelSections } from "./modelSelection";
import { reportActionError } from "../../lib/actionErrors";
import { Search, Check, Star, X, RefreshCw } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

/** The single model-selection purpose this dialog is opened for. Each settings
 *  field opens the dialog for exactly one purpose — there are no tabs. */
export type ModelDialogTab = "main" | "small" | "advisor" | "recap" | "ocr" | "mask" | "commit" | "summary" | "permission" | "explorer" | "context" | "autocontinue";

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
  explorer: "Select Explorer Model",
  context: "Select Context Model",
  autocontinue: "Select Auto-Continue Judge Model",
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
  /** SSH/WSL host of the session's project. When set, the model list and every
   *  session-scoped call go to that host's server (`/api/remote/<host>/…`) —
   *  a remote session's model registry and context live there, not locally.
   *  Resolved by the caller via `useSessionHost(sessionId)`. */
  host?: string;
}

export default function ModelDialog({ open, onClose, purpose = "main", onPick, currentValues, sessionId, host }: Props) {
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
  // True while the explicit Refresh action is fetching live provider lists.
  const [refreshing, setRefreshing] = useState(false);
  // "All providers" toggle (default off): off requests configured=true, so
  // only providers with credentials/config are listed. On requests the full
  // registry (~8k models). Reset to off each time the dialog opens.
  const [showAllProviders, setShowAllProviders] = useState(false);
  // Monotonic load token: a late cached/refresh response must not overwrite a
  // newer open (e.g. the dialog was closed and reopened for another purpose).
  const loadSeqRef = useRef(0);
  const [permissionModelState, setPermissionModelState] = useState("");
  const [explorerModelState, setExplorerModelState] = useState("");
  const [contextModelState, setContextModelState] = useState("");
  const [autoContinueModelState, setAutoContinueModelState] = useState("");
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

  // Fire-and-forget persistence with VISIBLE failure reporting. This dialog
  // closes immediately on pick (handleSelect/handleClear end with onClose), so
  // a dialog-local error banner would unmount before the user could read it —
  // failures route to the app-wide ActionErrorToast instead. Without this a
  // rejected write (404/500) was console.error-only, leaving the picker looking
  // like it accepted a model that never took effect.
  const persist = (what: string, fn: () => Promise<unknown>) => {
    fn().catch((err) => {
      console.error(`${what} failed`, err);
      reportActionError(err, what);
    });
  };

  // Pass `host` only when present so a local call stays byte-identical — an
  // explicit trailing `undefined` would change every local request's arity.
  const hostArgs = useMemo<[] | [string]>(() => (host ? [host] : []), [host]);

  // Augment the registry list for the permission, Security & Redaction (mask),
  // and auto-continue judge purposes with the user's enabled local/LM Studio
  // models — mirroring the TUI's permission-model, redaction-model and
  // autocontinue-model pickers, which list enabled LocalModels. The permission
  // judge and auto-continue judge are typically local models, so they must be
  // selectable here. Shared by the initial cached load and the Refresh action.
  const withLocalModels = useCallback(
    async (base: ModelInfo[]): Promise<ModelInfo[]> => {
      if (purpose !== "mask" && purpose !== "permission" && purpose !== "autocontinue") {
        return base;
      }
      try {
        const local = await api.getLocalModelsConfig(...hostArgs);
        const extra: ModelInfo[] = Object.entries(local)
          .filter(([, v]) => v.enabled)
          .map(([id]) => ({ name: id, model: id, provider: LOCAL_MODELS_PROVIDER, active: false }));
        return extra.length > 0 ? [...base, ...extra] : base;
      } catch {
        return base;
      }
    },
    // `hostArgs` is REQUIRED here: the body reads it, and `purpose` does not
    // change when the session's host resolves (undefined → "devbox") or the
    // active tab switches. Without it useCallback returns the stale closure,
    // so `loadList` (which does depend on hostArgs) reruns and augments the
    // list from the LOCAL server — the permission/mask/auto-continue pickers
    // then silently omit the remote host's enabled local models.
    [purpose, hostArgs],
  );

  // Loads the model list (cached, or live-refreshed when `refresh` is set) and
  // augments it. `showAll` selects the full registry vs configured providers
  // only. Guarded by loadSeqRef so a late response from a previous open or
  // toggle cannot clobber newer state.
  const loadList = useCallback(
    async (opts: { refresh?: boolean; showAll: boolean }) => {
      const seq = ++loadSeqRef.current;
      const apiOpts: { refresh?: boolean; configured?: boolean } = {};
      if (opts.refresh) apiOpts.refresh = true;
      if (!opts.showAll) apiOpts.configured = true;
      let base: ModelInfo[];
      try {
        base = await api.listModels(apiOpts, ...hostArgs);
      } catch (err) {
        // A failed list leaves the picker blank — surface it, or the dialog
        // looks like the registry is simply empty (e.g. a remote session whose
        // request reached a server that does not know it).
        console.error("load models failed", err);
        reportActionError(err, "Loading the model list");
        return;
      }
      const next = await withLocalModels(base);
      if (loadSeqRef.current === seq) setModels(next);
    },
    [hostArgs, withLocalModels],
  );

  // Explicit live refresh (web counterpart of the TUI picker's ctrl+r). Only
  // this path passes `refresh: true`, which makes the server fetch live
  // provider lists over the network and can take several seconds — never put
  // it on the open path.
  const refreshModels = useCallback(async () => {
    if (refreshing) return;
    setRefreshing(true);
    try {
      await loadList({ refresh: true, showAll: showAllProviders });
    } catch (err) {
      console.error("refresh models failed", err);
      reportActionError(err, "Refreshing the model list");
    } finally {
      setRefreshing(false);
    }
  }, [refreshing, showAllProviders, loadList]);

  // Flip between configured-only and the full registry (no refetch of the
  // config fields, so search text is preserved). loadList's seq guard drops the
  // in-flight response from the previous mode. The state updater stays pure —
  // StrictMode double-invokes it, so the fetch must not live inside.
  const toggleShowAllProviders = useCallback(() => {
    const next = !showAllProviders;
    setShowAllProviders(next);
    loadList({ showAll: next }).catch(console.error);
  }, [showAllProviders, loadList]);

  useEffect(() => {
    if (!open) return;
    setSearch("");
    // Default off: every open starts from configured providers only.
    setShowAllProviders(false);
    // Late responses from a previous open (or from a Refresh) must not clobber
    // the current dialog state (loadList's seq guard).
    // Cached list first so the picker renders immediately. The registry can be
    // thousands of models and a live refresh takes seconds, so opening never
    // blocks on the network; the Refresh action below is the live path.
    // configured=true keeps only providers with credentials/config — the
    // hundreds of other registry providers are unusable noise.
    loadList({ showAll: false }).catch(console.error);
    api.getConfigModel(...hostArgs).then((res) => {
      dispatch({ type: "SET_MODEL", model: res.model });
    }).catch(console.error);
    api.getSmallModel(...hostArgs).then((res) => {
      dispatch({ type: "SET_SMALL_MODEL", model: res.model });
    }).catch(console.error);
    api.getAdvisor(...hostArgs).then((res) => {
      dispatch({ type: "SET_ADVISOR_MODEL", model: res.model });
    }).catch(console.error);
    api.getAdvisorFull(...hostArgs).then((res) => {
      setAdvisorClaudeCode(res.claude_code);
    }).catch(console.error);
    if (purpose === "permission") {
      api.getPermissionModel(...hostArgs).then((res) => {
        setPermissionModelState(res.model ?? "");
      }).catch(console.error);
    }
    if (purpose === "explorer" && !currentValues?.explorer) {
      api.getExplorerModel(...hostArgs).then((res) => {
        setExplorerModelState(res.model ?? "");
      }).catch(console.error);
    }
    if (purpose === "context" && !currentValues?.context) {
      api.getContextModel(...hostArgs).then((res) => {
        setContextModelState(res.model ?? "");
      }).catch(console.error);
    }
    if (purpose === "autocontinue" && !currentValues?.autocontinue) {
      api.getAutoContinue(...hostArgs).then((res) => {
        setAutoContinueModelState(res.model ?? "");
      }).catch(console.error);
    }
  }, [open, dispatch, purpose, currentValues?.explorer, currentValues?.context, currentValues?.autocontinue, loadList, hostArgs]);

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
    purpose === "mask" ||
    purpose === "explorer" ||
    purpose === "context" ||
    // TUI's autocontinue-model picker reuses openModelPicker (recents +
    // favorites sections render; the ctrl+f star does not act on it).
    purpose === "autocontinue";
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
  // Mounting every provider row makes the dialog's first paint slow on the
  // full registry (~8k models). Cap the unfiltered render and point at the
  // search box; search filters the full list, so nothing becomes unreachable.
  // The client-appended Local Models group is exempt: those rows are the
  // typically-local judge models, and must not be pushed out by a large
  // configured registry.
  const cappedProviders = capProviderGroups(providerGroups, undefined, LOCAL_MODELS_UNCAPPED);

  const getCurrentModel = () => {
    switch (purpose) {
      case "small":
        return smallModel;
      case "advisor":
        return advisorModel;
      case "permission":
        return currentValues?.permission ?? permissionModelState;
      case "explorer":
        return currentValues?.explorer ?? explorerModelState;
      case "context":
        return currentValues?.context ?? contextModelState;
      case "autocontinue":
        return currentValues?.autocontinue ?? autoContinueModelState;
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
        persist("Changing the small model", () => api.setSmallModel(modelId, ...hostArgs));
        break;
      case "advisor":
        {
          const selection = advisorSelectionPayload(selectedModel);
          dispatch({ type: "SET_ADVISOR_MODEL", model: selection.model });
          onPick?.(purpose, selection.model, selectedModel);
          // Carry the current claude_code through the PUT: the server flips
          // claude_code to (provider === "claude-code") whenever provider is
          // set, which would silently disable CLI mode on any non-CLI pick.
          persist("Changing the advisor model", () => api.setAdvisorFull({ ...selection, claude_code: advisorClaudeCode }, ...hostArgs));
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
          api.setSessionModel(sessionId, modelId, ...hostArgs).catch((err) => {
            console.error("set session model failed", err);
            reportActionError(err, "Changing this session's model");
            // On failure, refetch this session's status so the sidebar shows
            // the model actually in effect rather than a stale value.
            api
              .getSessionStatus(sessionId, ...hostArgs)
              .then((st) => dispatch({ type: "SET_TUI_STATUS", sessionId, status: st }))
              .catch(console.error);
          });
        } else {
          dispatch({ type: "SET_MODEL", model: modelId });
          persist("Changing the model", () => api.setConfigModel(modelId, ...hostArgs));
        }
        break;
      case "permission":
        onPick?.(purpose, modelId, selectedModel);
        // If no form owns this pick (sidebar direct trigger), persist directly.
        if (!onPick) {
          persist("Changing the permission model", () => api.setPermissionModel(modelId, ...hostArgs));
        }
        break;
      case "explorer":
        onPick?.(purpose, modelId, selectedModel);
        if (!onPick) {
          setExplorerModelState(modelId);
          persist("Changing the explorer model", () => api.setExplorerModel(modelId, ...hostArgs));
        }
        break;
      case "context":
        onPick?.(purpose, modelId, selectedModel);
        if (!onPick) {
          setContextModelState(modelId);
          persist("Changing the context model", () => api.setContextModel(modelId, ...hostArgs));
        }
        break;
      case "autocontinue":
        onPick?.(purpose, modelId, selectedModel);
        if (!onPick) {
          // No form owns this pick (sidebar direct trigger): persist the judge
          // model directly. Leave the on/off gate untouched — mirroring the
          // TUI's `/autocontinue model <name>`, which never toggles the gate.
          setAutoContinueModelState(modelId);
          persist("Changing the auto-continue model", () => api.setAutoContinue({ model: modelId }, ...hostArgs));
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
      const res = await api.setModelFavorite(m.name, next, ...hostArgs);
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
        persist("Clearing the small model", () => api.setSmallModel("auto", ...hostArgs));
        break;
      case "advisor":
        dispatch({ type: "SET_ADVISOR_MODEL", model: "" });
        onPick?.(purpose, "");
        persist("Clearing the advisor model", () => api.setAdvisorFull({ model: "", provider: "", claude_code: advisorClaudeCode }, ...hostArgs));
        break;
      case "main":
        if (sessionId && sessionId.startsWith("new-")) {
          dispatch({ type: "SET_SESSION_MODEL", sessionId, model: undefined });
        } else if (sessionId) {
          api.clearSessionModel(sessionId, ...hostArgs).catch((err) => {
            console.error("clear session model failed", err);
            reportActionError(err, "Clearing this session's model");
            api
              .getSessionStatus(sessionId, ...hostArgs)
              .then((st) => dispatch({ type: "SET_TUI_STATUS", sessionId, status: st }))
              .catch(console.error);
          });
        } else {
          dispatch({ type: "SET_MODEL", model: "" });
          persist("Clearing the model", () => api.setConfigModel("", ...hostArgs));
        }
        break;
      case "permission":
        onPick?.(purpose, "");
        if (!onPick) {
          persist("Clearing the permission model", () => api.setPermissionModel("", ...hostArgs));
        }
        break;
      case "explorer":
        onPick?.(purpose, "");
        if (!onPick) {
          setExplorerModelState("");
          persist("Clearing the explorer model", () => api.setExplorerModel("auto", ...hostArgs));
        }
        break;
      case "context":
        onPick?.(purpose, "");
        if (!onPick) {
          setContextModelState("");
          persist("Clearing the context model", () => api.setContextModel("auto", ...hostArgs));
        }
        break;
      case "autocontinue":
        onPick?.(purpose, "");
        if (!onPick) {
          // Clear = judge model cleared, gate untouched (TUI parity: "auto"/
          // "none" clears the model, meaning StepLimitHit-only resumes).
          setAutoContinueModelState("");
          persist("Clearing the auto-continue model", () => api.setAutoContinue({ clear: true }, ...hostArgs));
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
  // `withProvider` mirrors the TUI's modelPickerLabel(id) which shows the full
  // "provider/model" id in the Recently Used / Favorites sections where there
  // is no per-provider header to disambiguate.
  const renderRow = (m: ModelInfo, opts?: { withProvider?: boolean }) => {
    const selected =
      getCurrentModel() === (purpose === "advisor" ? m.model : m.name) || m.active;
    // Only canonical "provider/model" ids can enter the shared favorites
    // state (the server validates the same way); pseudo-rows like the
    // locally-added LM Studio entries never show a star.
    const canFavorite = supportsFavoriteToggle && m.name.includes("/");
    const withProvider = opts?.withProvider ?? false;
    const baseLabel = withProvider ? m.name : m.model;
    // Match TUI display-name decoration but keep the provider prefix when
    // withProvider is true. Guard against display_name that is just the bare
    // model or the full id restated.
    const label =
      m.display_name && m.display_name !== m.model && m.display_name !== m.name
        ? `${m.display_name} (${baseLabel})`
        : baseLabel;
    return (
      <div key={m.name} className="flex items-start gap-1">
        <button
          onClick={() => handleSelect(m)}
          className={`w-full min-w-0 flex items-start justify-between gap-2 px-3 py-2 rounded-md text-sm text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background ${
            selected ? "bg-blue-600/20 text-blue-400" : "text-foreground hover:bg-muted"
          }`}
        >
          <span className="min-w-0 flex-1 whitespace-normal break-words [overflow-wrap:anywhere]">
            {label}
          </span>
          {(m.has_model_prompt || m.has_kaizen) && (
            <span
              title={
                (m.has_model_prompt ? "Custom model prompt (OCODE.md) active" : "") +
                (m.has_model_prompt && m.has_kaizen ? " + " : "") +
                (m.has_kaizen ? "Kaizen conduct directives active" : "")
              }
              className="mt-0.5 shrink-0 rounded border border-blue-400/40 bg-blue-500/10 px-1 py-0 text-[10px] leading-4 text-blue-400"
            >
              tuned
            </span>
          )}
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

        {/* Search + live refresh. The list opens from the cached registry
            instantly; Refresh fetches live provider lists (TUI ctrl+r). */}
        <div className="flex items-center gap-2">
          <div className="relative flex-1">
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
          <button
            type="button"
            onClick={refreshModels}
            disabled={refreshing}
            aria-label="Refresh model list"
            title="Fetch live provider model lists (may take a few seconds)"
            className="shrink-0 flex items-center gap-1.5 px-3 py-2 rounded-md text-sm text-muted-foreground hover:text-foreground hover:bg-muted transition-colors disabled:opacity-60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <RefreshCw className={`h-4 w-4 ${refreshing ? "animate-spin" : ""}`} />
            {refreshing ? "Refreshing" : "Refresh"}
          </button>
        </div>

        {/* Default off: only providers with credentials/config are listed. On
            fetches the full registry (~8k models), which is what makes the
            default list fast. Reset to off on every open. */}
        <label className="flex items-center gap-2 text-xs text-muted-foreground select-none cursor-pointer">
          <input
            type="checkbox"
            checked={showAllProviders}
            onChange={toggleShowAllProviders}
            aria-label="Show all providers"
            className="h-3.5 w-3.5 accent-blue-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
          All providers (including unconfigured)
        </label>

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
              {sections.recents.map((m) => renderRow(m, { withProvider: true }))}
            </div>
          )}
          {sections && sections.favorites.length > 0 && (
            <div className="mb-4">
              <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                ★ Favorites
              </div>
              {sections.favorites.map((m) => renderRow(m, { withProvider: true }))}
            </div>
          )}
          {Object.entries(cappedProviders.groups).map(([provider, providerModels]) => (
            <div key={provider} className="mb-4">
              <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                {provider}
              </div>
              {providerModels.map((m) => renderRow(m))}
            </div>
          ))}
          {cappedProviders.hidden > 0 && (
            <div className="px-3 py-2 text-xs text-muted-foreground">
              {cappedProviders.hidden.toLocaleString()} more models not shown — type to refine your search.
            </div>
          )}
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
