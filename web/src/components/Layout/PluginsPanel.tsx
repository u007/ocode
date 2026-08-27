import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import type { PluginInfo } from "../../api/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2, Trash2, Download, Puzzle } from "lucide-react";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// PluginsPanel is the web-side manager for ocode plugins. It lists installed
// plugins with enable/disable toggles, installs a plugin by name/URL (optionally
// pinned with `@ref`), and removes a plugin. Mirrors the TUI plugin management.
export default function PluginsPanel({ open, onOpenChange }: Props) {
  const [plugins, setPlugins] = useState<PluginInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [source, setSource] = useState("");
  const [installing, setInstalling] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [astEnabled, setAstEnabled] = useState(false);
  const [astBusy, setAstBusy] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [list, pluginsEnabled] = await Promise.all([
        api.listPlugins(),
        api.getPluginsEnabledConfig(),
      ]);
      // Sort alphabetically for a stable listing.
      setPlugins(list.slice().sort((a, b) => a.name.localeCompare(b.name)));
      setAstEnabled(pluginsEnabled.ast);
    } catch (err) {
      console.error("failed to load plugins", err);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) load();
  }, [open, load]);

  const toggleAst = async () => {
    setAstBusy(true);
    setError(null);
    try {
      await api.setPluginsEnabledConfig(!astEnabled);
      setAstEnabled(!astEnabled);
    } catch (err) {
      console.error("failed to toggle ast plugin", err);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setAstBusy(false);
    }
  };

  const toggle = async (p: PluginInfo) => {
    setBusy(p.name);
    setError(null);
    try {
      await api.setPluginEnabled(p.name, !p.enabled);
      await load();
    } catch (err) {
      console.error("failed to toggle plugin", err);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  const remove = async (p: PluginInfo) => {
    setBusy(p.name);
    setError(null);
    try {
      await api.removePlugin(p.name);
      await load();
    } catch (err) {
      console.error("failed to remove plugin", err);
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
      console.error("failed to install plugin", err);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setInstalling(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[80vh] p-0 overflow-hidden gap-0 !flex flex-col">
        <DialogHeader className="px-4 py-3 border-b border-border">
          <DialogTitle className="flex items-center gap-2 text-sm">
            <Puzzle className="w-4 h-4" />
            Plugins
          </DialogTitle>
        </DialogHeader>

        {/* Install row */}
        <div className="flex items-center gap-2 px-4 py-3 border-b border-border">
          <Input
            value={source}
            onChange={(e) => setSource(e.target.value)}
            placeholder="name, git URL, or owner/repo@ref"
            className="h-8 text-xs"
            onKeyDown={(e) => {
              if (e.key === "Enter") install();
            }}
          />
          <Button
            size="sm"
            className="h-8 gap-1.5 text-xs shrink-0"
            onClick={install}
            disabled={installing || !source.trim()}
          >
            {installing ? (
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
            ) : (
              <Download className="w-3.5 h-3.5" />
            )}
            Install
          </Button>
        </div>

        {error && (
          <div className="px-4 py-2 text-xs text-red-400 border-b border-border">
            {error}
          </div>
        )}

        <div className="flex-1 min-h-0 overflow-y-auto p-3 space-y-3">
          {/* Builtin plugins — mirrors TUI's "Builtin plugins:" section */}
          <div>
            <div className="text-xs font-semibold text-muted-foreground mb-2">
              Builtin plugins
            </div>
            <div className="flex items-center justify-between gap-2 py-2 px-3 rounded-md hover:bg-accent/50">
              <div className="min-w-0">
                <div className="text-sm text-foreground truncate">ast</div>
                <div className="text-xs text-muted-foreground truncate">
                  ast-grep structural search/rewrite (needs the ast-grep CLI on PATH).
                </div>
              </div>
              <Button
                variant={astEnabled ? "default" : "outline"}
                size="sm"
                className="h-7 text-xs min-w-[56px] shrink-0"
                onClick={toggleAst}
                disabled={astBusy}
              >
                {astBusy ? (
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                ) : astEnabled ? (
                  "On"
                ) : (
                  "Off"
                )}
              </Button>
            </div>
          </div>

          <div className="border-t border-border pt-3">
            <div className="text-xs font-semibold text-muted-foreground mb-2">
              Installed plugins
            </div>
          {loading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
            </div>
          ) : plugins.length === 0 ? (
            <div className="text-xs text-muted-foreground text-center py-8">
              No plugins installed.
            </div>
          ) : (
            plugins.map((p) => (
              <div
                key={p.name}
                className="flex items-center justify-between gap-2 py-2 px-3 rounded-md hover:bg-accent/50"
              >
                <div className="min-w-0">
                  <div className="text-sm text-foreground truncate">{p.name}</div>
                  <div className="text-xs text-muted-foreground truncate" title={p.description || p.source}>
                    {p.description || p.source}
                  </div>
                </div>
                <div className="flex items-center gap-1.5 shrink-0">
                  <Button
                    variant={p.enabled ? "default" : "outline"}
                    size="sm"
                    className="h-7 text-xs min-w-[56px]"
                    onClick={() => toggle(p)}
                    disabled={busy === p.name}
                  >
                    {busy === p.name ? (
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    ) : p.enabled ? (
                      "On"
                    ) : (
                      "Off"
                    )}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 w-7 p-0 text-muted-foreground hover:text-red-400"
                    onClick={() => remove(p)}
                    disabled={busy === p.name}
                    title="Remove plugin"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </Button>
                </div>
              </div>
            ))
          )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
