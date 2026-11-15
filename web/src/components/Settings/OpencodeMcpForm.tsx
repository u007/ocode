import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import type { MCPStatus } from "../../api/types";
import { Button } from "../ui/button";
import { Loader2, RefreshCw } from "lucide-react";

export default function OpencodeMcpForm() {
  const [servers, setServers] = useState<MCPStatus[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [reloading, setReloading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [connecting, setConnecting] = useState(false);
  const [connectNote, setConnectNote] = useState<string | null>(null);
  const connected = servers.some((m) => m.name === "elevenlabs");

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

  const reloadAll = async () => {
    setReloading(true);
    setError(null);
    try {
      const res = await api.reloadMCPAndWait();
      // load() clears the error, so surface reload errors AFTER it — the POST
      // body is only an acknowledgement now, the poll carries the outcome.
      await load();
      if (res.errors.length) {
        setError(res.errors.join("; "));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setReloading(false);
    }
  };

  // connectElevenLabs installs the preset and starts a browser OAuth flow. The
  // flow's own timeout is 2 minutes; poll slightly longer so its error lands.
  const connectElevenLabs = async () => {
    setConnecting(true);
    setError(null);
    setConnectNote(null);
    try {
      const start = await api.connectElevenLabs();
      setConnectNote(start.browser_note ?? "Waiting for the browser sign-in…");
      const startedAt = Date.now();
      let status: { status: string; error?: string };
      for (;;) {
        status = await api.getMCPAuthStatus(start.job_id);
        if (status.status === "done" || status.status === "error") break;
        if (Date.now() - startedAt > 130_000) throw new Error("authorization timed out after 130s");
        await new Promise((resolve) => setTimeout(resolve, 1000));
      }
      if (status.status !== "done") throw new Error(status.error || "authorization failed");
      const res = await api.reloadMCPAndWait();
      await load();
      if (res.errors.length) setError(res.errors.join("; "));
      setConnectNote("Connected. ElevenLabs tools are attached to new turns.");
    } catch (err) {
      // The preset is written before the flow starts, so refresh the list: the
      // card must show the entry even when authorization failed. load() clears
      // the error first, so set the message after it.
      await load();
      setError(err instanceof Error ? err.message : String(err));
      setConnectNote(null);
    } finally {
      setConnecting(false);
    }
  };

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
        <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold text-foreground">MCP Servers</h2>
        <button
          type="button"
          onClick={reloadAll}
          disabled={reloading}
          className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground disabled:opacity-50"
        >
          <RefreshCw className={`w-3 h-3 ${reloading ? "animate-spin" : ""}`} />
          Reload all
        </button>
      </div>
      <div className="rounded-md border border-border p-3 space-y-2">
        <div className="flex items-center justify-between">
          <span className="text-xs font-medium text-foreground">ElevenLabs</span>
          <span className={`text-xs ${connected ? "text-emerald-400" : "text-muted-foreground"}`}>
            {connected ? "configured" : "not connected"}
          </span>
        </div>
        <p className="text-xs text-muted-foreground">
          Speech, music, image and video generation through ElevenLabs. Signs in with your ElevenLabs account
          (OAuth); the callback reaches this machine only.
        </p>
        <Button size="sm" onClick={connectElevenLabs} disabled={connecting} className="h-8 text-xs">
          {connecting && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
          {connecting ? "Waiting for browser…" : connected ? "Reconnect ElevenLabs" : "Connect ElevenLabs"}
        </Button>
        {connectNote && <div className="text-xs text-muted-foreground">{connectNote}</div>}
      </div>
      {error && <div className="text-xs text-red-400">{error}</div>}
      {servers.length === 0 ? (
        <div className="text-xs text-muted-foreground">No MCP servers configured.</div>
      ) : (
        <div className="space-y-1">
          {servers.map((m) => (
            <div key={m.name} className="flex items-center justify-between gap-2 py-2 px-3 rounded-md hover:bg-muted">
              <span className="truncate font-mono text-sm text-foreground">{m.name}</span>
              <button
                type="button"
                onClick={() => toggle(m)}
                disabled={busy === m.name}
                className="flex items-center gap-2 disabled:opacity-50"
              >
                <span className={`text-xs ${m.enabled ? "text-emerald-400" : "text-muted-foreground"}`}>
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
