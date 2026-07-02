import { useState, useEffect, useCallback } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { ScrollArea } from "../ui/scroll-area";
import { Separator } from "../ui/separator";
import { Loader2, Palette, Puzzle, Save } from "lucide-react";

interface MonacoSettings {
  theme: string;
  font_size: number;
  tab_size: number;
  word_wrap: boolean;
  minimap: boolean;
  line_numbers: boolean;
}

interface MonacoExtension {
  name: string;
  label: string;
  enabled: boolean;
  builtin: boolean;
}

const THEMES = [
  { id: "ocode-dark", label: "Ocode Dark" },
  { id: "vs-dark", label: "VS Dark" },
  { id: "vs", label: "VS Light" },
  { id: "hc-black", label: "High Contrast" },
];

export default function MonacoSettingsPanel() {
  const [settings, setSettings] = useState<MonacoSettings | null>(null);
  const [extensions, setExtensions] = useState<MonacoExtension[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [tab, setTab] = useState<"settings" | "extensions">("settings");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [s, exts] = await Promise.all([
        api.getMonacoSettings(),
        api.listMonacoExtensions(),
      ]);
      setSettings(s);
      setExtensions(exts);
    } catch (err) {
      console.error("Failed to load Monaco config:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    if (!settings) return;
    setSaving(true);
    try {
      await api.setMonacoSettings(settings as unknown as Record<string, unknown>);
      setDirty(false);
    } catch (err) {
      console.error("Failed to save Monaco settings:", err);
    } finally {
      setSaving(false);
    }
  };

  const toggleExtension = async (name: string) => {
    try {
      const updated = await api.toggleMonacoExtension(name);
      setExtensions(updated);
    } catch (err) {
      console.error("Failed to toggle extension:", err);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-6 h-6 text-muted-foreground animate-spin" />
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full bg-background">
      {/* Header */}
      <div className="flex items-center gap-1 px-4 h-10 border-b border-border">
        <Button
          variant={tab === "settings" ? "secondary" : "ghost"}
          size="sm"
          className="h-7 gap-1.5"
          onClick={() => setTab("settings")}
        >
          <Palette className="w-3.5 h-3.5" />
          <span className="text-xs">Settings</span>
        </Button>
        <Button
          variant={tab === "extensions" ? "secondary" : "ghost"}
          size="sm"
          className="h-7 gap-1.5"
          onClick={() => setTab("extensions")}
        >
          <Puzzle className="w-3.5 h-3.5" />
          <span className="text-xs">Extensions</span>
        </Button>
      </div>

      <ScrollArea className="flex-1">
        {tab === "settings" && settings && (
          <div className="p-4 space-y-5">
            {/* Theme */}
            <div>
              <label className="text-xs font-medium text-foreground block mb-2">
                Theme
              </label>
              <div className="grid grid-cols-2 gap-2">
                {THEMES.map((t) => (
                  <Button
                    key={t.id}
                    variant={settings.theme === t.id ? "default" : "outline"}
                    size="sm"
                    className="justify-start h-9 text-xs"
                    onClick={() => {
                      setSettings({ ...settings, theme: t.id });
                      setDirty(true);
                    }}
                  >
                    {t.label}
                  </Button>
                ))}
              </div>
            </div>

            <Separator />

            {/* Font size */}
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-foreground">
                Font Size
              </label>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 w-7 p-0"
                  onClick={() => {
                    if (settings.font_size > 8) {
                      setSettings({ ...settings, font_size: settings.font_size - 1 });
                      setDirty(true);
                    }
                  }}
                >
                  −
                </Button>
                <span className="text-sm text-foreground w-8 text-center tabular-nums">
                  {settings.font_size}
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 w-7 p-0"
                  onClick={() => {
                    if (settings.font_size < 30) {
                      setSettings({ ...settings, font_size: settings.font_size + 1 });
                      setDirty(true);
                    }
                  }}
                >
                  +
                </Button>
              </div>
            </div>

            <Separator />

            {/* Tab size */}
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-foreground">
                Tab Size
              </label>
              <select
                className="h-8 px-2 text-xs bg-muted border border-input rounded text-foreground"
                value={settings.tab_size}
                onChange={(e) => {
                  setSettings({ ...settings, tab_size: parseInt(e.target.value) });
                  setDirty(true);
                }}
              >
                {[2, 4, 8].map((n) => (
                  <option key={n} value={n}>
                    {n} spaces
                  </option>
                ))}
              </select>
            </div>

            <Separator />

            {/* Toggles */}
            <div className="space-y-3">
              <ToggleRow
                label="Word Wrap"
                checked={settings.word_wrap}
                onChange={(v) => {
                  setSettings({ ...settings, word_wrap: v });
                  setDirty(true);
                }}
              />
              <ToggleRow
                label="Minimap"
                checked={settings.minimap}
                onChange={(v) => {
                  setSettings({ ...settings, minimap: v });
                  setDirty(true);
                }}
              />
              <ToggleRow
                label="Line Numbers"
                checked={settings.line_numbers}
                onChange={(v) => {
                  setSettings({ ...settings, line_numbers: v });
                  setDirty(true);
                }}
              />
            </div>
          </div>
        )}

        {tab === "extensions" && (
          <div className="p-4 space-y-1">
            {extensions.map((ext) => (
              <div
                key={ext.name}
                className="flex items-center justify-between py-2 px-3 rounded-md hover:bg-accent/50"
              >
                <div className="min-w-0">
                  <div className="text-sm text-foreground">{ext.label}</div>
                  <div className="text-xs text-muted-foreground">
                    {ext.name}
                    {ext.builtin && " • built-in"}
                  </div>
                </div>
                <Button
                  variant={ext.enabled ? "default" : "outline"}
                  size="sm"
                  className="h-7 text-xs min-w-[60px]"
                  onClick={() => toggleExtension(ext.name)}
                >
                  {ext.enabled ? "On" : "Off"}
                </Button>
              </div>
            ))}
          </div>
        )}
      </ScrollArea>

      {/* Save button (only for settings) */}
      {tab === "settings" && (
        <div className="border-t border-border p-3">
          <Button
            size="sm"
            className="w-full h-8 gap-1.5 text-xs"
            onClick={save}
            disabled={!dirty || saving}
          >
            {saving ? (
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
            ) : (
              <Save className="w-3.5 h-3.5" />
            )}
            {saving ? "Saving..." : "Save Settings"}
          </Button>
        </div>
      )}
    </div>
  );
}

function ToggleRow({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between">
      <label className="text-xs font-medium text-foreground">{label}</label>
      <button
        role="switch"
        aria-checked={checked}
        className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ${
          checked ? "bg-primary" : "bg-muted"
        }`}
        onClick={() => onChange(!checked)}
      >
        <span
          className={`pointer-events-none inline-block h-4 w-4 rounded-full bg-background shadow-lg ring-0 transition-transform ${
            checked ? "translate-x-4" : "translate-x-0"
          }`}
        />
      </button>
    </div>
  );
}
