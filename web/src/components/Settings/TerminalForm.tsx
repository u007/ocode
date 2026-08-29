import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";
import {
  DEFAULT_TERMINAL_FONT_FAMILY,
  DEFAULT_TERMINAL_FONT_SIZE,
  refreshTerminalConfig,
} from "@/hooks/useTerminalConfig";
import {
  loadSoundSettings,
  saveSoundSettings,
  playAlertSound,
  type TerminalSoundSettings,
} from "../Terminal/terminalAlertSound";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";

// Radix Select rejects an empty-string item value, so "use the system
// default shell" is represented by this sentinel and mapped back to "" when
// saving.
const SYSTEM_DEFAULT_SHELL = "__default__";

export default function TerminalForm() {
  const [scrollback, setScrollback] = useState(0);
  const [fontFamily, setFontFamily] = useState(DEFAULT_TERMINAL_FONT_FAMILY);
  const [fontSize, setFontSize] = useState(DEFAULT_TERMINAL_FONT_SIZE);
  const [shell, setShell] = useState(SYSTEM_DEFAULT_SHELL);
  const [defaultShell, setDefaultShell] = useState("");
  const [availableShells, setAvailableShells] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getTerminalConfig();
      setScrollback(cfg.scrollback_lines);
      setFontFamily(cfg.font_family || DEFAULT_TERMINAL_FONT_FAMILY);
      setFontSize(cfg.font_size || DEFAULT_TERMINAL_FONT_SIZE);
      setShell(cfg.shell || SYSTEM_DEFAULT_SHELL);
      setDefaultShell(cfg.default_shell || "");
      setAvailableShells(cfg.available_shells || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // Sound/bell alert preferences live in the browser (see terminalAlertSound);
  // load them separately from the server-backed terminal config above.
  const [soundEnabled, setSoundEnabled] = useState(true);
  const [soundDataUrl, setSoundDataUrl] = useState<string | null>(null);
  const [soundFileName, setSoundFileName] = useState<string | null>(null);

  useEffect(() => {
    const s = loadSoundSettings();
    setSoundEnabled(s.enabled);
    setSoundDataUrl(s.dataUrl);
    setSoundFileName(s.fileName);
  }, []);

  const persistSound = (patch: Partial<TerminalSoundSettings>) => {
    const next: TerminalSoundSettings = {
      enabled: soundEnabled,
      dataUrl: soundDataUrl,
      fileName: soundFileName,
      ...patch,
    };
    setSoundEnabled(next.enabled);
    setSoundDataUrl(next.dataUrl);
    setSoundFileName(next.fileName);
    saveSoundSettings(next);
  };

  const onPickSound = (file: File) => {
    const reader = new FileReader();
    reader.onload = () => {
      const dataUrl = typeof reader.result === "string" ? reader.result : null;
      if (!dataUrl) return;
      persistSound({ dataUrl, fileName: file.name });
    };
    reader.readAsDataURL(file);
  };

  const onClearSound = () => persistSound({ dataUrl: null, fileName: null });

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.setTerminalScrollbackLines(scrollback);
      await api.setTerminalFontConfig(fontFamily, fontSize);
      await api.setTerminalShell(shell === SYSTEM_DEFAULT_SHELL ? "" : shell);
      await refreshTerminalConfig();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
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
      <h2 className="text-sm font-semibold text-foreground">Terminal</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Scrollback lines</label>
        <Input
          type="number"
          value={scrollback}
          onChange={(e) => setScrollback(Number(e.target.value))}
          className="h-8 text-xs w-32"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Font family</label>
        <Input
          type="text"
          value={fontFamily}
          onChange={(e) => setFontFamily(e.target.value)}
          placeholder={DEFAULT_TERMINAL_FONT_FAMILY}
          className="h-8 text-xs w-full"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Font size (px)</label>
        <Input
          type="number"
          value={fontSize}
          onChange={(e) => setFontSize(Number(e.target.value))}
          className="h-8 text-xs w-32"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Shell</label>
        <Select value={shell} onValueChange={setShell}>
          <SelectTrigger className="h-8 text-xs w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={SYSTEM_DEFAULT_SHELL} className="text-xs">
              System default{defaultShell ? ` (${defaultShell})` : ""}
            </SelectItem>
            {availableShells.map((path) => (
              <SelectItem key={path} value={path} className="text-xs">
                {path}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-2 border-t border-border pt-4">
        <label className="text-xs font-medium text-foreground">Sound alerts</label>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={soundEnabled}
            onChange={(e) => persistSound({ enabled: e.target.checked })}
            className="h-3.5 w-3.5 accent-blue-500"
          />
          Play a sound when a backgrounded terminal rings a bell or triggers a notification
        </label>
        <div className="flex items-center gap-2">
          <Input
            type="file"
            accept="audio/*"
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file) onPickSound(file);
              e.target.value = "";
            }}
            className="h-8 text-xs w-full"
          />
          {soundFileName && (
            <Button size="sm" variant="ghost" onClick={onClearSound} className="h-8 text-xs whitespace-nowrap shrink-0">
              Clear
            </Button>
          )}
          <Button size="sm" variant="outline" onClick={playAlertSound} className="h-8 text-xs whitespace-nowrap shrink-0">
            Test
          </Button>
        </div>
        <p className="text-[11px] text-muted-foreground">
          {soundFileName
            ? `Custom sound: ${soundFileName}`
            : "No custom sound selected — a system-style beep is used by default."}
        </p>
      </div>

      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
