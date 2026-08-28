import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import type { PluginInfo } from "../../api/types";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2, Trash2, Download } from "lucide-react";

export default function OcodePluginsForm() {
  const [astEnabled, setAstEnabled] = useState(false);
  const [plugins, setPlugins] = useState<PluginInfo[]>([]);
  const [source, setSource] = useState("");
  const [installing, setInstalling] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [localModels, setLocalModels] = useState<Record<string, { enabled: boolean; max_parallel: number }>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [pluginsEnabled, list, models] = await Promise.all([
        api.getPluginsEnabledConfig(),
        api.listPlugins(),
        api.getLocalModelsConfig(),
      ]);
      setAstEnabled(!!pluginsEnabled?.ast);
      setPlugins(Array.isArray(list) ? list.slice().sort((a, b) => a.name.localeCompare(b.name)) : []);
      setLocalModels(models && typeof models === "object" ? models : {});
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const saveAst = async (next: boolean) => {
    setSaving(true);
    setError(null);
    try {
      await api.setPluginsEnabledConfig(next);
      setAstEnabled(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const toggleExternal = async (p: PluginInfo) => {
    setBusy(p.name);
    setError(null);
    try {
      await api.setPluginEnabled(p.name, !p.enabled);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  const removeExternal = async (p: PluginInfo) => {
    setBusy(p.name);
    setError(null);
    try {
      await api.removePlugin(p.name);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  const install = async () => {
    const src = source.trim();
    if (!src) return;
    setInstalling(true);
    setError(null);
    try {
      await api.installPlugin(src);
      setSource("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setInstalling(false);
    }
  };

  const toggleLocalModel = async (name: string) => {
    const next = { ...localModels, [name]: { ...localModels[name], enabled: !localModels[name].enabled } };
    setLocalModels(next);
    setSaving(true);
    setError(null);
    try {
      await api.setLocalModelsConfig(next);
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
    <div className="p-6 max-w-lg space-y-6">
      <div>
        <h2 className="text-sm font-semibold text-zinc-200 mb-2">Plugins & Local Models</h2>
        {error && <div className="text-xs text-red-400 mb-2">{error}</div>}
        <label className="flex items-center gap-2 text-xs text-zinc-400">
          <input
            type="checkbox"
            checked={astEnabled}
            disabled={saving}
            onChange={(e) => saveAst(e.target.checked)}
          />
          AST structural search/rewrite tool (ast_grep) enabled
        </label>
      </div>

      <div className="border-t border-zinc-700 pt-4">
        <div className="text-xs font-semibold text-zinc-300 mb-2">External plugins</div>
        <div className="flex items-center gap-2 mb-3">
          <Input
            value={source}
            onChange={(e) => setSource(e.target.value)}
            placeholder="name, git URL, or owner/repo@ref"
            className="h-8 text-xs"
            onKeyDown={(e) => {
              if (e.key === "Enter") install();
            }}
          />
          <Button size="sm" className="h-8 gap-1.5 text-xs shrink-0" onClick={install} disabled={installing || !source.trim()}>
            {installing ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Download className="w-3.5 h-3.5" />}
            Install
          </Button>
        </div>
        {plugins.length === 0 ? (
          <div className="text-xs text-zinc-500">No plugins installed.</div>
        ) : (
          <div className="space-y-1">
            {plugins.map((p) => (
              <div key={p.name} className="flex items-center justify-between gap-2 py-2 px-3 rounded-md hover:bg-zinc-800">
                <div className="min-w-0 text-sm text-zinc-300 truncate">{p.name}</div>
                <div className="flex items-center gap-1.5 shrink-0">
                  <Button
                    variant={p.enabled ? "default" : "outline"}
                    size="sm"
                    className="h-7 text-xs min-w-[56px]"
                    onClick={() => toggleExternal(p)}
                    disabled={busy === p.name}
                  >
                    {busy === p.name ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : p.enabled ? "On" : "Off"}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 w-7 p-0 text-zinc-500 hover:text-red-400"
                    onClick={() => removeExternal(p)}
                    disabled={busy === p.name}
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="border-t border-zinc-700 pt-4">
        <div className="text-xs font-semibold text-zinc-300 mb-2">Local models</div>
        {Object.keys(localModels).length === 0 ? (
          <div className="text-xs text-zinc-500">No local models registered.</div>
        ) : (
          <div className="space-y-1">
            {Object.entries(localModels).map(([name, m]) => (
              <div key={name} className="flex items-center justify-between gap-2 py-2 px-3 rounded-md hover:bg-zinc-800">
                <div className="min-w-0 text-sm text-zinc-300 truncate font-mono">{name}</div>
                <Button
                  variant={m.enabled ? "default" : "outline"}
                  size="sm"
                  className="h-7 text-xs min-w-[56px]"
                  onClick={() => toggleLocalModel(name)}
                >
                  {m.enabled ? "On" : "Off"}
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
