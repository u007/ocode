import { useState, useEffect } from "react";
import { api } from "../../api/client";
import { useChatDispatch, useChatState } from "../../stores/chatStore";
import type { ModelInfo } from "../../api/types";
import { advisorSelectionPayload } from "./modelSelection";
import { Search, Check } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

/** The single model-selection purpose this dialog is opened for. Each settings
 *  field opens the dialog for exactly one purpose — there are no tabs. */
export type ModelDialogTab = "main" | "small" | "advisor" | "recap" | "ocr" | "mask" | "commit" | "summary";

const PURPOSE_TITLES: Record<ModelDialogTab, string> = {
  main: "Select Model",
  small: "Select Small Model",
  advisor: "Select Advisor Model",
  recap: "Select Recap Model",
  ocr: "Select OCR Model",
  mask: "Select Mask Model",
  commit: "Select Commit Message Model",
  summary: "Select Summary Model",
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
}

export default function ModelDialog({ open, onClose, purpose = "main", onPick, currentValues }: Props) {
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [search, setSearch] = useState("");
  // The advisor's Claude Code toggle is owned by AdvisorForm; the dialog must
  // carry the current value through a pick so the server's provider-change
  // convention (provider set → claude_code = (provider === "claude-code"))
  // cannot silently flip a toggle the user set explicitly.
  const [advisorClaudeCode, setAdvisorClaudeCode] = useState(false);
  const { model: activeModel, smallModel, advisorModel } = useChatState();
  const dispatch = useChatDispatch();

  useEffect(() => {
    if (open) {
      api.listModels().then(setModels).catch(console.error);
      setSearch("");
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
    }
  }, [open, dispatch]);

  const filteredModels = models.filter(
    (m) =>
      m.name.toLowerCase().includes(search.toLowerCase()) ||
      m.model.toLowerCase().includes(search.toLowerCase()) ||
      m.provider.toLowerCase().includes(search.toLowerCase())
  );

  const groupedModels = filteredModels.reduce((acc, m) => {
    const provider = m.provider || "Other";
    if (!acc[provider]) acc[provider] = [];
    acc[provider].push(m);
    return acc;
  }, {} as Record<string, ModelInfo[]>);

  const getCurrentModel = () => {
    switch (purpose) {
      case "small":
        return smallModel;
      case "advisor":
        return advisorModel;
      default:
        return currentValues?.[purpose] ?? activeModel;
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
        dispatch({ type: "SET_MODEL", model: modelId });
        api.setConfigModel(modelId).catch(console.error);
        break;
      default:
        // Form-owned purpose (recap/ocr/mask/commit/summary): hand the pick to
        // the owning form, which persists it via its own Save.
        onPick?.(purpose, modelId, selectedModel);
        break;
    }
    onClose();
  };

  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="sm:max-w-md bg-zinc-900 border-zinc-700">
        <DialogHeader>
          <DialogTitle className="text-zinc-100">{PURPOSE_TITLES[purpose]}</DialogTitle>
        </DialogHeader>

        {/* Search */}
        <div className="relative">
          <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-zinc-500" />
          <input
            type="text"
            placeholder="Search models..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full pl-10 pr-4 py-2 bg-zinc-800 border border-zinc-700 rounded-md text-sm text-zinc-100 placeholder-zinc-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-900"
            autoFocus
          />
        </div>

        {/* Model list */}
        <div className="max-h-96 overflow-y-auto">
          {Object.entries(groupedModels).map(([provider, providerModels]) => (
            <div key={provider} className="mb-4">
              <div className="px-2 py-1 text-xs font-semibold text-zinc-500 uppercase tracking-wider">
                {provider}
              </div>
              {providerModels.map((m) => (
                <button
                  key={m.name}
                  onClick={() => handleSelect(m)}
                  className={`w-full flex items-center justify-between px-3 py-2 rounded-md text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-900 ${
                    getCurrentModel() === (purpose === "advisor" ? m.model : m.name) || m.active
                      ? "bg-blue-600/20 text-blue-400"
                      : "text-zinc-300 hover:bg-zinc-800"
                  }`}
                >
                  <span className="truncate">{m.model}</span>
                  {(getCurrentModel() === (purpose === "advisor" ? m.model : m.name) || m.active) && (
                    <Check className="h-4 w-4 text-blue-400 flex-shrink-0" />
                  )}
                </button>
              ))}
            </div>
          ))}
          {filteredModels.length === 0 && (
            <div className="text-center py-8 text-zinc-500 text-sm">
              No models found
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
