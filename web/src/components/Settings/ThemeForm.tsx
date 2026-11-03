import { useCallback, useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { api } from "../../api/client";
import type { ThemeRef } from "../../api/types";
import { loadAppearanceSettings, setColorThemeId } from "../../lib/appearanceTheme";

const SOURCE_LABELS: Record<string, string> = {
  builtin: "Built-in",
  opencode: "opencode",
  vscode: "VS Code",
};

const SOURCE_BADGE_CLASSES: Record<string, string> = {
  builtin: "bg-sky-500/15 text-sky-300",
  opencode: "bg-violet-500/15 text-violet-300",
  vscode: "bg-amber-500/15 text-amber-300",
};

function sourceLabel(source: string): string {
  return SOURCE_LABELS[source] ?? source;
}

function ThemeButton({
  theme,
  active,
  saving,
  onPick,
}: {
  theme: ThemeRef;
  active: boolean;
  saving: boolean;
  onPick: (id: string) => void;
}) {
  return (
    <button
      type="button"
      onClick={() => onPick(theme.id)}
      disabled={saving}
      aria-pressed={active}
      className={`flex items-center justify-between gap-2 text-xs rounded px-2 py-1.5 text-left transition-colors disabled:opacity-60 ${
        active
          ? "bg-emerald-600/30 text-emerald-300 border border-emerald-600/50"
          : "bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground"
      }`}
      title={theme.id}
    >
      <span className="truncate">{theme.label}</span>
      <span className="flex shrink-0 items-center gap-1">
        {saving && <Loader2 className="w-3 h-3 animate-spin" />}
        <span
          className={`rounded px-1 py-0.5 text-[10px] uppercase tracking-wide ${
            SOURCE_BADGE_CLASSES[theme.source] ?? "bg-muted text-muted-foreground"
          }`}
        >
          {sourceLabel(theme.source)}
        </span>
      </span>
    </button>
  );
}

export default function ThemeForm() {
  const [themes, setThemes] = useState<ThemeRef[]>([]);
  const [currentTheme, setCurrentTheme] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [warnings, setWarnings] = useState<string[]>([]);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      // The settings call is shared with the appearance store, so picking a
      // theme below does not have to re-fetch it to preserve icon theme/roots.
      const [list, settings] = await Promise.all([
        api.getAppearanceThemes(),
        loadAppearanceSettings(),
      ]);
      setThemes(Array.isArray(list.themes) ? list.themes : []);
      setWarnings([...(list.warnings ?? []), ...(settings.warnings ?? [])]);
      // Show the APPLIED id: the server's effective fallback when nothing is
      // stored (or the stored theme disappeared), otherwise the selection.
      setCurrentTheme(settings.effective_color_theme || settings.color_theme || "");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const pick = async (id: string) => {
    setError(null);
    setSaving(id);
    try {
      // Persist first, then apply: a rejected write must not repaint the UI
      // with a theme the server refused to store.
      await setColorThemeId(id);
      setCurrentTheme(id);
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err);
      setError(`Could not apply ${id}: ${reason}`);
    } finally {
      setSaving(null);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
      </div>
    );
  }

  const registry = themes.filter((theme) => theme.source !== "vscode");
  const vscode = themes.filter((theme) => theme.source === "vscode");

  const renderGroup = (title: string, items: ThemeRef[]) =>
    items.length === 0 ? null : (
      <section className="space-y-1.5">
        <h3 className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          {title}
        </h3>
        <div className="grid grid-cols-3 gap-1.5">
          {items.map((theme) => (
            <ThemeButton
              key={theme.id}
              theme={theme}
              active={currentTheme === theme.id}
              saving={saving === theme.id}
              onPick={pick}
            />
          ))}
        </div>
      </section>
    );

  return (
    <div className="p-6 max-w-lg space-y-4">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="text-sm font-semibold text-foreground">Theme</h2>
        <span className="text-xs text-muted-foreground">
          Current: <code className="text-foreground">{currentTheme || "none"}</code>
        </span>
      </div>
      {error && (
        <div role="alert" className="text-xs text-red-400">
          {error}
        </div>
      )}
      {warnings.length > 0 && (
        <ul className="space-y-0.5 text-[11px] text-amber-400/90">
          {warnings.map((warning) => (
            <li key={warning}>{warning}</li>
          ))}
        </ul>
      )}
      {themes.length === 0 ? (
        <div className="text-xs text-muted-foreground">No themes available.</div>
      ) : (
        <div className="space-y-4">
          {renderGroup("Built-in & opencode", registry)}
          {renderGroup("VS Code themes", vscode)}
        </div>
      )}
    </div>
  );
}
