import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";
import {
  DEFAULT_TERMINAL_CELL_HEIGHT,
  DEFAULT_TERMINAL_FONT_FAMILY,
  DEFAULT_TERMINAL_FONT_SIZE,
  refreshTerminalConfig,
} from "@/hooks/useTerminalConfig";
import type { GhosttyReport, TerminalAppearance } from "../Terminal/terminalAppearance";
import { buildKeybindTable } from "../Terminal/ghosttyKeybinds";
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

/**
 * Terminal settings.
 *
 * Appearance is a WHOLE-BLOCK choice between the user's Ghostty configuration
 * and ocode's own settings (see docs: the two are never mixed, or one terminal
 * would combine a Ghostty font with ocode colours). While Ghostty is the source,
 * the ocode fields show the RESOLVED values read-only; "Copy to ocode settings"
 * seeds them so switching sources keeps the look.
 */
export default function TerminalForm() {
  const [scrollback, setScrollback] = useState(0);
  // ocode-source settings (persisted).
  const [fontFamily, setFontFamily] = useState(DEFAULT_TERMINAL_FONT_FAMILY);
  const [fontSize, setFontSize] = useState(DEFAULT_TERMINAL_FONT_SIZE);
  const [cellWidthPercent, setCellWidthPercent] = useState(100);
  const [cellHeightPercent, setCellHeightPercent] = useState(
    DEFAULT_TERMINAL_CELL_HEIGHT.percent ?? 10,
  );
  const [theme, setTheme] = useState("");
  const [appearanceSource, setAppearanceSource] = useState<"ghostty" | "ocode">("ghostty");
  const [shell, setShell] = useState(SYSTEM_DEFAULT_SHELL);
  const [defaultShell, setDefaultShell] = useState("");
  const [availableShells, setAvailableShells] = useState<string[]>([]);
  // What the server resolved + what it read from Ghostty.
  const [appearance, setAppearance] = useState<TerminalAppearance | null>(null);
  const [ghostty, setGhostty] = useState<GhosttyReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getTerminalConfig();
      setScrollback(cfg.scrollback_lines);
      // Seed the ocode-source fields from the PERSISTED values, never from the
      // resolved ones: with Ghostty active those are Ghostty's numbers, and
      // showing them here would make switching sources silently save them.
      const stored = cfg.appearance?.settings;
      setFontFamily(stored?.font_family || cfg.font_family || DEFAULT_TERMINAL_FONT_FAMILY);
      setFontSize(stored?.font_size || cfg.font_size || DEFAULT_TERMINAL_FONT_SIZE);
      setCellWidthPercent(stored?.cell_width_percent ?? 100);
      setCellHeightPercent(
        stored?.cell_height_percent ?? DEFAULT_TERMINAL_CELL_HEIGHT.percent ?? 10,
      );
      setAppearanceSource(cfg.appearance?.requested ?? "ghostty");
      setTheme(stored?.theme ?? "");
      setAppearance(cfg.appearance ?? null);
      setGhostty(cfg.ghostty ?? null);
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

  // Keybind report: what will actually run, and what was read but cannot.
  const keybindReport = useMemo(() => buildKeybindTable(ghostty?.keybinds), [ghostty]);

  const ghosttyActive = appearanceSource === "ghostty" && !!ghostty?.found;
  const ghosttyMissing = appearanceSource === "ghostty" && !ghostty?.found;

  const useOcodeValues = () => {
    if (!appearance) return;
    // Seed from what is on screen right now, so the look survives the switch.
    if (appearance.source === "ghostty") {
      setFontFamily(appearance.font_family || DEFAULT_TERMINAL_FONT_FAMILY);
      setFontSize(appearance.font_size || DEFAULT_TERMINAL_FONT_SIZE);
      setCellWidthPercent(absolutePercent(appearance.cell_width_adjust, 100));
      setCellHeightPercent(absolutePercent(appearance.cell_height_adjust, 100));
      setTheme(appearance.theme_name ?? "");
    }
    setAppearanceSource("ocode");
  };

  // ── Sound/bell alert preferences live in the browser (see
  // terminalAlertSound); they are not part of the server-side config above.
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
      // The appearance block is saved in one request: source + every ocode field,
      // so a half-written block can never mix two sources.
      await api.setTerminalAppearance({
        appearance_source: appearanceSource,
        font_family: fontFamily,
        font_size: fontSize,
        cell_width_percent: cellWidthPercent,
        cell_height_percent: cellHeightPercent,
        theme,
      });
      await api.setTerminalShell(shell === SYSTEM_DEFAULT_SHELL ? "" : shell);
      await refreshTerminalConfig();
      await load();
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
      {appearance?.errors?.length ? (
        <ul className="space-y-1 rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-[11px] text-amber-600 dark:text-amber-500">
          {appearance.errors.map((message) => (
            <li key={message}>{message}</li>
          ))}
        </ul>
      ) : null}

      <div className="space-y-2 rounded-md border border-border p-3">
        <div className="text-xs font-medium text-foreground">Appearance source</div>
        <label className="flex items-start gap-2 text-xs text-muted-foreground">
          <input
            type="radio"
            name="terminal-appearance-source"
            checked={appearanceSource === "ghostty"}
            onChange={() => setAppearanceSource("ghostty")}
            className="mt-0.5 h-3.5 w-3.5 accent-blue-500"
          />
          <span>
            Follow my Ghostty configuration
            {ghostty?.found ? (
              <span className="block text-[11px] text-muted-foreground">
                Font, size, cell spacing and theme come from{" "}
                <code className="text-[11px]">{(ghostty.paths ?? []).join(", ")}</code>.
              </span>
            ) : (
              <span className="block text-[11px] text-muted-foreground">
                No Ghostty configuration found on this machine — ocode&rsquo;s settings below are
                used instead.
              </span>
            )}
          </span>
        </label>
        <label className="flex items-start gap-2 text-xs text-muted-foreground">
          <input
            type="radio"
            name="terminal-appearance-source"
            checked={appearanceSource === "ocode"}
            onChange={() => setAppearanceSource("ocode")}
            className="mt-0.5 h-3.5 w-3.5 accent-blue-500"
          />
          <span>
            Use ocode&rsquo;s terminal settings
            <span className="block text-[11px] text-muted-foreground">
              Overrides the Ghostty font, spacing and theme with the values below.
            </span>
          </span>
        </label>
        {ghosttyActive && (
          <div className="rounded bg-muted/40 p-2 text-[11px] text-muted-foreground space-y-0.5">
            <div>
              Resolved: <span className="text-foreground">{appearance?.font_family}</span> at{" "}
              <span className="text-foreground">{appearance?.font_size}px</span>
              {appearance?.theme_name ? (
                <>
                  {" "}
                  · theme <span className="text-foreground">{appearance.theme_name}</span>
                </>
              ) : null}
              {" "}· cell {signedPercent(appearance?.cell_width_adjust)} / {signedPercent(appearance?.cell_height_adjust)}
            </div>
            <Button
              size="sm"
              variant="outline"
              onClick={useOcodeValues}
              className="h-7 text-[11px] mt-1"
            >
              Copy to ocode settings
            </Button>
          </div>
        )}
        {ghosttyMissing && (
          <div className="text-[11px] text-muted-foreground">
            Ocode is using its own settings because no Ghostty config exists here.
          </div>
        )}
        {(ghostty?.errors ?? []).length > 0 && (
          <div className="text-[11px] text-amber-400">
            {(ghostty?.errors ?? []).join(" · ")}
          </div>
        )}
      </div>

      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Font family</label>
        <Input
          type="text"
          value={fontFamily}
          onChange={(e) => setFontFamily(e.target.value)}
          placeholder={DEFAULT_TERMINAL_FONT_FAMILY}
          disabled={ghosttyActive}
          className="h-8 text-xs w-full"
        />
      </div>
      <div className="grid grid-cols-3 gap-3">
        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">Font size (px)</label>
          <Input
            type="number"
            value={fontSize}
            onChange={(e) => setFontSize(Number(e.target.value))}
            disabled={ghosttyActive}
            className="h-8 text-xs w-full"
          />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">Cell width %</label>
          <Input
            type="number"
            value={cellWidthPercent}
            onChange={(e) => setCellWidthPercent(Number(e.target.value))}
            disabled={ghosttyActive}
            className="h-8 text-xs w-full"
          />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">Cell height %</label>
          <Input
            type="number"
            value={cellHeightPercent}
            onChange={(e) => setCellHeightPercent(Number(e.target.value))}
            disabled={ghosttyActive}
            className="h-8 text-xs w-full"
          />
        </div>
      </div>
      <p className="text-[11px] text-muted-foreground -mt-2">
        100% is the font&rsquo;s natural cell; 110% adds 10% breathing room between rows.
      </p>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Theme (Ghostty theme name)</label>
        <Input
          type="text"
          value={theme}
          onChange={(e) => setTheme(e.target.value)}
          placeholder="e.g. Bright Lights — empty keeps ocode's palette"
          disabled={ghosttyActive}
          className="h-8 text-xs w-full"
        />
        <p className="text-[11px] text-muted-foreground">
          Resolved from the same themes Ghostty ships, without reading Ghostty&rsquo;s other
          settings.
        </p>
      </div>

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

      {(keybindReport.bindings.size > 0 || keybindReport.unsupported.length > 0) && (
        <div className="space-y-1.5 border-t border-border pt-4">
          <div className="text-xs font-medium text-foreground">Keybindings from Ghostty</div>
          {keybindReport.bindings.size > 0 && (
            <p className="text-[11px] text-muted-foreground">
              Applied:{" "}
              {[...keybindReport.bindings.keys()]
                .map((chord) => `⌨ ${chord}`)
                .join(" · ")}
            </p>
          )}
          {keybindReport.unsupported.length > 0 && (
            <details className="text-[11px] text-muted-foreground">
              <summary className="cursor-pointer">
                {keybindReport.unsupported.length} binding
                {keybindReport.unsupported.length === 1 ? "" : "s"} not applied
              </summary>
              <ul className="mt-1 space-y-1">
                {keybindReport.unsupported.map((item) => (
                  <li key={`${item.chord}:${item.action}`}>
                    <code>{item.chord}</code> → <code>{item.action}</code> — {item.reason}
                  </li>
                ))}
              </ul>
            </details>
          )}
        </div>
      )}

      {(ghostty?.ignored ?? []).length > 0 && (
        <div className="space-y-1.5 border-t border-border pt-4">
          <div className="text-xs font-medium text-foreground">Not portable from Ghostty</div>
          <ul className="space-y-1 text-[11px] text-muted-foreground">
            {(ghostty?.ignored ?? []).map((item) => (
              <li key={item.key}>
                <code>{item.key}</code>
                {item.value ? <> = {item.value}</> : null} — {item.reason}
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="space-y-2 border-t border-border pt-4">
        <label className="text-xs font-medium text-foreground">Sound alerts</label>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={soundEnabled}
            onChange={(e) => persistSound({ enabled: e.target.checked })}
            className="h-3.5 w-3.5 accent-blue-500"
          />
          Play a sound when a backgrounded terminal rings a bell, or a backgrounded chat finishes,
          stalls, or waits for your approval
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

/** The API reports cell adjustments as DELTAS; the form edits absolute
 *  percentages (100 = natural), which is what a user thinks in. */
function absolutePercent(
  adjust: { percent?: number; px?: number } | undefined,
  fallback: number,
): number {
  if (!adjust) return fallback;
  if (adjust.px) return fallback; // px deltas have no absolute form; keep the default
  return 100 + (adjust.percent ?? 0);
}

function signedPercent(adjust: { percent?: number; px?: number } | undefined): string {
  if (!adjust) return "100%";
  if (adjust.px) return `${adjust.px > 0 ? "+" : ""}${adjust.px}px`;
  const percent = adjust.percent ?? 0;
  return percent === 0 ? "100%" : `${100 + percent}%`;
}
