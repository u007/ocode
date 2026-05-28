import { useState, useEffect } from "react";
import { api } from "../../api/client";
import type { AgentInfo } from "../../api/types";

interface Props {
  activeAgent: string;
  onSelect: (name: string) => void;
}

export default function AgentTabs({ activeAgent, onSelect }: Props) {
  const [agents, setAgents] = useState<AgentInfo[]>([]);

  useEffect(() => {
    api.listAgents().then(setAgents).catch(console.error);
  }, []);

  if (agents.length === 0) return null;

  return (
    <div className="p-3 border-b border-zinc-700">
      <label className="text-xs text-zinc-500 uppercase tracking-wider">Agent</label>
      <div className="mt-1 flex flex-wrap gap-1">
        {agents.map((a) => (
          <button
            key={a.name}
            onClick={() => onSelect(a.name)}
            className={`rounded px-2 py-1 text-xs ${
              activeAgent === a.name
                ? "bg-blue-600 text-white"
                : "bg-zinc-800 text-zinc-400 hover:bg-zinc-700"
            }`}
          >
            {a.name}
          </button>
        ))}
      </div>
    </div>
  );
}
