import { useCallback, useEffect, useState } from "react";
import { api, type SpeechSummaryConfig } from "../../api/client";
import { DEFAULT_SPEECH_SUMMARY_CONFIG, speechSummaryDisplay } from "../../lib/speechSummaryConfig";
import { Button } from "../ui/button";
import { Loader2 } from "lucide-react";
import ModelDialog from "../Layout/ModelDialog";
import { useSpeechOptional } from "../Speech/SpeechProvider";

/**
 * Settings for the model that shortens assistant text before TTS reads it.
 *
 * Deliberately a separate block from Compact: `compact.enabled` decides whether
 * a turn compacts itself, `speech_summary_enabled` decides what gets read
 * aloud. They happen to both be "a summary model" and are easy to confuse, so
 * this form names the purpose in every label and lives under its own heading.
 *
 * Save writes the WHOLE block (both keys), which is correct here because this
 * form owns both. It writes through SpeechProvider — the runtime owner the speak
 * path reads — when that provider is mounted, and reads/writes the SAME host's
 * block, so a save here takes effect without a reload and cannot disagree with
 * the sidebar or a model pick.
 */
export default function SpeechSummaryForm() {
  // The provider is present wherever Settings is reachable in the app; the
  // fallback keeps the form working in isolated tests.
  const speech = useSpeechOptional();
  const [cfg, setCfg] = useState<SpeechSummaryConfig>(DEFAULT_SPEECH_SUMMARY_CONFIG);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setCfg(await api.getSpeechSummaryConfig(speech?.host));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [speech?.host]);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    setNotice(null);
    try {
      // Through the provider when present: it owns the runtime config the speak
      // path reads, so the change applies immediately instead of waiting for a
      // reload. The return is the server's merged block, which we render from.
      const saved = speech
        ? await speech.updateSummaryConfig(cfg)
        : await api.setSpeechSummaryConfig(cfg);
      setCfg(saved);
      setNotice("Saved");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input
          type="checkbox"
          checked={cfg.enabled}
          onChange={(e) => setCfg({ ...cfg, enabled: e.target.checked })}
        />
        Shorten text before it is spoken
      </label>

      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Speech summary model</label>
        <div className="flex items-center gap-2">
          <div
            className="flex-1 h-8 px-3 rounded-md bg-muted border border-border text-xs text-foreground flex items-center truncate"
            title={cfg.model || undefined}
          >
            {cfg.model || "Not set — uses the small model, then the main model"}
          </div>
          <Button size="sm" variant="outline" type="button" onClick={() => setDialogOpen(true)} className="h-8 text-xs">
            Change…
          </Button>
        </div>
        <p className="text-[11px] text-muted-foreground">
          {speechSummaryDisplay(cfg)}. This model is used only to prepare text for
          speech; it does not affect compaction, which has its own summary model.
        </p>
        <ModelDialog
          open={dialogOpen}
          onClose={() => setDialogOpen(false)}
          purpose="speechsummary"
          // The form owns the write: ModelDialog must not PUT on its own, or this
          // form's checkbox would be bypassed and the two halves desynced.
          onPick={(_, m) => setCfg((prev) => ({ ...prev, model: m }))}
          currentValues={{ speechsummary: cfg.model }}
        />
      </div>

      {error && <p className="text-xs text-destructive">{error}</p>}
      <div className="flex items-center gap-2">
        <Button size="sm" type="button" onClick={save} disabled={saving}>
          {saving ? "Saving…" : "Save"}
        </Button>
        {notice && <span className="text-xs text-muted-foreground">{notice}</span>}
      </div>
    </div>
  );
}
