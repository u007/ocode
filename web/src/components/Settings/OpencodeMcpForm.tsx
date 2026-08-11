import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import type { MCPStatus } from "../../api/types";
import { Loader2 } from "lucide-react";

export default function OpencodeMcpForm() {
  const [servers, setServers] = useState<MCPStatus[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setServers(await api.getMCP());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const toggle = async (m: MCPStatus) => {
    setBusy(m.name);
    setError(null);
    try {
      await api.setMCPEnabled(m.name, !m.enabled);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">MCP Servers</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      {servers.length === 0 ? (
        <div className="text-xs text-zinc-500">No MCP servers configured.</div>
      ) : (
        <div className="space-y-1">
          {servers.map((m) => (
            <div key={m.name} className="flex items-center justify-between gap-2 py-2 px-3 rounded-md hover:bg-zinc-800">
              <span className="truncate font-mono text-sm text-zinc-300">{m.name}</span>
              <button
                type="button"
                onClick={() => toggle(m)}
                disabled={busy === m.name}
                className="flex items-center gap-2 disabled:opacity-50"
              >
                <span className={`text-xs ${m.enabled ? "text-emerald-400" : "text-zinc-500"}`}>
                  {m.enabled ? "on" : "off"}
                </span>
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
