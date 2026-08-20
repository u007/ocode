import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Loader2 } from "lucide-react";

export default function ProfileDebugForm() {
  const [enabled, setEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getProfileDebugConfig();
      setEnabled(cfg.profile_debug);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.setProfileDebugConfig(enabled);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
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
    <div className="space-y-6">
      <div>
        <h3 className="text-sm font-semibold text-zinc-200">Profile Debug</h3>
        <p className="text-xs text-zinc-500 mt-1">
          When enabled, the active profile and its effective overrides are emitted to the log tab (kind <code className="bg-zinc-800 px-1 rounded">PROFILE</code>) on every profile switch and on the next turn&apos;s agent build. Default off.
        </p>
      </div>

      {error && <div className="text-sm text-red-400 bg-red-950/30 border border-red-900 rounded px-3 py-2">{error}</div>}

      <label className="flex items-center gap-3 cursor-pointer">
        <input
          type="checkbox"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
          className="rounded"
        />
        <span className="text-sm text-zinc-200">Enable profile debug logs</span>
      </label>
      <p className="text-xs text-zinc-500">
        Logs include: window id, active profile (or Default), display name, effective model, override count, credential count, and per-session effective model/project. Filter the log tab by <code className="bg-zinc-800 px-1 rounded">PROFILE</code> to see only these entries.
      </p>

      <Button onClick={save} disabled={saving} size="sm">
        {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
        Save
      </Button>
    </div>
  );
}
