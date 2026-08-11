import { useCallback, useEffect, useState } from "react";
import { api, type TUISettings } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

const EMPTY: TUISettings = { theme: "", mouse: null, scroll_speed: 0, keybinds: {}, leader_timeout: 0, branchless: false };

export default function TUIForm() {
  const [cfg, setCfg] = useState<TUISettings>(EMPTY);
  const [keybindsText, setKeybindsText] = useState("{}");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const c = await api.getTUISettings();
      setCfg(c);
      setKeybindsText(JSON.stringify(c.keybinds ?? {}, null, 2));
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
      let keybinds: Record<string, string> = {};
      try {
        keybinds = JSON.parse(keybindsText);
      } catch {
        setError("Keybinds must be valid JSON");
        setSaving(false);
        return;
      }
      setCfg(await api.setTUISettings({ ...cfg, keybinds }));
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
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">TUI</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Theme</label>
        <Input value={cfg.theme} onChange={(e) => setCfg({ ...cfg, theme: e.target.value })} className="h-8 text-xs" />
      </div>
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input
          type="checkbox"
          checked={Boolean(cfg.mouse)}
          onChange={(e) => setCfg({ ...cfg, mouse: e.target.checked })}
        />
        Mouse support
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Scroll speed</label>
        <Input
          type="number"
          value={cfg.scroll_speed}
          onChange={(e) => setCfg({ ...cfg, scroll_speed: Number(e.target.value) })}
          className="h-8 text-xs w-32"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Leader key timeout (ms)</label>
        <Input
          type="number"
          value={cfg.leader_timeout}
          onChange={(e) => setCfg({ ...cfg, leader_timeout: Number(e.target.value) })}
          className="h-8 text-xs w-32"
        />
      </div>
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input
          type="checkbox"
          checked={cfg.branchless}
          onChange={(e) => setCfg({ ...cfg, branchless: e.target.checked })}
        />
        Branchless mode
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Keybinds (JSON)</label>
        <textarea
          value={keybindsText}
          onChange={(e) => setKeybindsText(e.target.value)}
          rows={6}
          className="w-full rounded-md border border-zinc-700 bg-zinc-800 px-2 py-1.5 text-xs text-zinc-200 font-mono"
        />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
