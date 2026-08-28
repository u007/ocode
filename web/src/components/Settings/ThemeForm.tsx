import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { applyThemeColors } from "../../hooks/useTheme";
import { Loader2 } from "lucide-react";

interface ThemeOption {
  name: string;
  label: string;
}

export default function ThemeForm() {
  const [themes, setThemes] = useState<ThemeOption[]>([]);
  const [currentTheme, setCurrentTheme] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await api.getThemes();
      setThemes(Array.isArray(res.themes) ? res.themes : []);
      setCurrentTheme(res.current ?? "");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // Apply a theme by fetching its colors from the server and writing them to
  // the CSS variables (same mechanism the sidebar Theme section used — the
  // change is visual only; the TUI theme is owned by the terminal config).
  const apply = async (name: string) => {
    setError(null);
    try {
      const resp = await api.getTheme(name);
      applyThemeColors(resp.colors);
      setCurrentTheme(name);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
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
      <h2 className="text-sm font-semibold text-zinc-200">Theme</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      {themes.length === 0 ? (
        <div className="text-xs text-zinc-500">No themes available.</div>
      ) : (
        <div className="grid grid-cols-3 gap-1.5">
          {themes.map((t) => (
            <button
              key={t.name}
              type="button"
              onClick={() => apply(t.name)}
              className={`text-xs rounded px-2 py-1.5 truncate transition-colors ${
                currentTheme === t.name
                  ? "bg-emerald-600/30 text-emerald-300 border border-emerald-600/50"
                  : "bg-zinc-800 text-zinc-400 hover:bg-zinc-700"
              }`}
              title={t.name}
            >
              {t.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
