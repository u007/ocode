import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";
import ModelDialog from "../Layout/ModelDialog";

export default function SecurityForm() {
  const [enabled, setEnabled] = useState(false);
  const [mode, setMode] = useState("lenient");
  const [model, setModel] = useState("");
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [baseUrl, setBaseUrl] = useState("");
  const [failMode, setFailMode] = useState("warn");
  const [allowRemoteTier2, setAllowRemoteTier2] = useState(false);
  const [customWords, setCustomWords] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getMaskAdvanced();
      setEnabled(cfg.enabled);
      setMode(cfg.mode);
      setModel(cfg.model);
      setBaseUrl(cfg.base_url);
      setFailMode(cfg.fail_mode);
      setAllowRemoteTier2(cfg.allow_remote_tier2);
      setCustomWords((cfg.custom_words ?? []).join(", "));
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
      await api.setMaskEnabled(enabled);
      await api.setMaskMode(mode);
      await api.setMaskModel(model);
      await api.setMaskAdvanced({
        base_url: baseUrl,
        fail_mode: failMode,
        allow_remote_tier2: allowRemoteTier2,
        custom_words: customWords.split(",").map((w) => w.trim()).filter(Boolean),
      });
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
      <h2 className="text-sm font-semibold text-foreground">Security & Redaction</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}

      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        Enabled
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Mode (lenient / full)</label>
        <Input value={mode} onChange={(e) => setMode(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Model</label>
        <div className="flex items-center gap-2">
          <div className="flex-1 h-8 px-3 rounded-md bg-muted border border-border text-xs text-foreground flex items-center truncate" title={model || undefined}>
            {model || "Not set"}
          </div>
          <Button size="sm" variant="outline" type="button" onClick={() => setModelDialogOpen(true)} className="h-8 text-xs">
            Change…
          </Button>
        </div>
        <ModelDialog
          open={modelDialogOpen}
          onClose={() => setModelDialogOpen(false)}
          purpose="mask"
          onPick={(_, m) => {
            setModel(m);
            // Mirror the TUI: a local provider with no base URL yet gets the
            // default local-server endpoint so the tier-2 scanner can reach it.
            const provider = m.split("/")[0];
            if (provider === "lmstudio" && baseUrl.trim() === "") {
              setBaseUrl("http://localhost:1234/v1");
            }
          }}
          currentValues={{ mask: model }}
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Base URL</label>
        <Input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Fail mode (block / warn)</label>
        <Input value={failMode} onChange={(e) => setFailMode(e.target.value)} className="h-8 text-xs" />
      </div>
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input type="checkbox" checked={allowRemoteTier2} onChange={(e) => setAllowRemoteTier2(e.target.checked)} />
        Allow remote tier-2 scanner endpoints
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Custom words (comma-separated)</label>
        <Input value={customWords} onChange={(e) => setCustomWords(e.target.value)} className="h-8 text-xs" />
      </div>

      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>

      <RekeySection />
    </div>
  );
}

// RekeySection changes the passphrase protecting the project's age key
// (internal/secretfile.RekeyProjectKey re-wraps the same key, so files
// already encrypted with it stay valid without needing to be re-encrypted).
function RekeySection() {
  const [oldPassphrase, setOldPassphrase] = useState("");
  const [newPassphrase, setNewPassphrase] = useState("");
  const [confirmPassphrase, setConfirmPassphrase] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  const submit = async () => {
    if (!oldPassphrase || !newPassphrase) return;
    if (newPassphrase !== confirmPassphrase) {
      setError("Passphrases did not match");
      return;
    }
    setSaving(true);
    setError(null);
    setSuccess(false);
    try {
      await api.secretRekey("", oldPassphrase, newPassphrase, confirmPassphrase);
      setOldPassphrase("");
      setNewPassphrase("");
      setConfirmPassphrase("");
      setSuccess(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="pt-4 mt-4 border-t border-border space-y-3">
      <h2 className="text-sm font-semibold text-foreground">Project Passphrase</h2>
      <p className="text-xs text-muted-foreground">
        Change the passphrase protecting this project's encrypted-file key. Files already
        encrypted stay valid — only the passphrase changes.
      </p>
      {error && <div className="text-xs text-red-400">{error}</div>}
      {success && <div className="text-xs text-emerald-400">Passphrase updated.</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Current passphrase</label>
        <Input
          type="password"
          value={oldPassphrase}
          onChange={(e) => setOldPassphrase(e.target.value)}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">New passphrase</label>
        <Input
          type="password"
          value={newPassphrase}
          onChange={(e) => setNewPassphrase(e.target.value)}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Confirm new passphrase</label>
        <Input
          type="password"
          value={confirmPassphrase}
          onChange={(e) => setConfirmPassphrase(e.target.value)}
          className="h-8 text-xs"
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
      </div>
      <Button
        size="sm"
        variant="outline"
        onClick={submit}
        disabled={saving || !oldPassphrase || !newPassphrase || !confirmPassphrase}
        className="h-8 text-xs"
      >
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Change Passphrase
      </Button>
    </div>
  );
}
