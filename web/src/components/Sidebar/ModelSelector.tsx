import { useState, useEffect } from "react";
import { api } from "../../api/client";
import { useChatState, useChatDispatch } from "../../stores/chatStore";
import type { ModelInfo } from "../../api/types";

export default function ModelSelector() {
  const [models, setModels] = useState<ModelInfo[]>([]);
  const { model: activeModel } = useChatState();
  const dispatch = useChatDispatch();

  useEffect(() => {
    api.listModels().then(setModels).catch(console.error);
  }, []);

  return (
    <div className="p-3 border-b border-zinc-700">
      <label className="text-xs text-zinc-500 uppercase tracking-wider">Model</label>
      <select
        value={activeModel || ""}
        onChange={(e) => dispatch({ type: "SET_MODEL", model: e.target.value })}
        className="mt-1 w-full rounded border border-zinc-600 bg-zinc-800 px-2 py-1.5 text-sm text-zinc-100"
      >
        {models.map((m) => (
          <option key={m.name} value={m.name}>{m.name}</option>
        ))}
      </select>
    </div>
  );
}
