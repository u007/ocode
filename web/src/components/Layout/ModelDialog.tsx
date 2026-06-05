import { useState, useEffect } from "react";
import { api } from "../../api/client";
import { useChatDispatch, useChatState } from "../../stores/chatStore";
import type { ModelInfo } from "../../api/types";
import { Search, Check } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

interface Props {
  open: boolean;
  onClose: () => void;
  initialTab?: "main" | "small" | "advisor";
}

export default function ModelDialog({ open, onClose, initialTab = "main" }: Props) {
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [search, setSearch] = useState("");
  const [activeTab, setActiveTab] = useState<"main" | "small" | "advisor">(initialTab);
  const { model: activeModel, smallModel, advisorModel } = useChatState();
  const dispatch = useChatDispatch();

  useEffect(() => {
    if (open) {
      setActiveTab(initialTab);
      api.listModels().then(setModels).catch(console.error);
      setSearch("");
      // Fetch current small and advisor models
      api.getSmallModel().then((res) => {
        dispatch({ type: "SET_SMALL_MODEL", model: res.model });
      }).catch(console.error);
      api.getAdvisor().then((res) => {
        dispatch({ type: "SET_ADVISOR_MODEL", model: res.model });
      }).catch(console.error);
    }
  }, [open, initialTab, dispatch]);

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
    switch (activeTab) {
      case "small":
        return smallModel;
      case "advisor":
        return advisorModel;
      default:
        return activeModel;
    }
  };

  const handleSelect = (modelId: string) => {
    switch (activeTab) {
      case "small":
        dispatch({ type: "SET_SMALL_MODEL", model: modelId });
        api.setSmallModel(modelId).catch(console.error);
        break;
      case "advisor":
        dispatch({ type: "SET_ADVISOR_MODEL", model: modelId });
        api.setAdvisor(modelId).catch(console.error);
        break;
      default:
        dispatch({ type: "SET_MODEL", model: modelId });
        api.setConfigModel(modelId).catch(console.error);
        break;
    }
    onClose();
  };

  const tabs = [
    { id: "main" as const, label: "Model" },
    { id: "small" as const, label: "Small Model" },
    { id: "advisor" as const, label: "Advisor" },
  ];

  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="sm:max-w-md bg-zinc-900 border-zinc-700">
        <DialogHeader>
          <DialogTitle className="text-zinc-100">Select Model</DialogTitle>
        </DialogHeader>

        {/* Tabs */}
        <div className="flex gap-1 border-b border-zinc-700 pb-2">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={`px-3 py-1.5 text-sm rounded-md transition-colors ${
                activeTab === tab.id
                  ? "bg-zinc-700 text-white"
                  : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800"
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {/* Search */}
        <div className="relative">
          <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-zinc-500" />
          <input
            type="text"
            placeholder="Search models..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full pl-10 pr-4 py-2 bg-zinc-800 border border-zinc-700 rounded-md text-sm text-zinc-100 placeholder-zinc-500 focus:outline-none focus:border-blue-500"
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
                  onClick={() => handleSelect(m.name)}
                  className={`w-full flex items-center justify-between px-3 py-2 rounded-md text-sm transition-colors ${
                    getCurrentModel() === m.name || m.active
                      ? "bg-blue-600/20 text-blue-400"
                      : "text-zinc-300 hover:bg-zinc-800"
                  }`}
                >
                  <span className="truncate">{m.model}</span>
                  {(getCurrentModel() === m.name || m.active) && (
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
