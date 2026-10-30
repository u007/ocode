import { useEffect, useState } from "react";
import { Check, Download, Loader2 } from "lucide-react";
import { api } from "../../api/client";
import { INTENT_PACKAGE_NAME, type IntentPackageInfo } from "../../api/types";
import { reloadDynamicCommands } from "../Chat/commands";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "../ui/dialog";
import { Button } from "../ui/button";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// IntentSkillsDialog lists the skills of the latest @tanstack/intent and
// installs the checked ones into ~/.claude/skills. It is the web twin of the
// TUI picker opened by `/plugin install @tanstack/intent`.
export default function IntentSkillsDialog({ open, onOpenChange }: Props) {
  const [pkg, setPkg] = useState<IntentPackageInfo | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [checked, setChecked] = useState<Set<string>>(() => new Set());
  const [installing, setInstalling] = useState(false);
  const [report, setReport] = useState<string | null>(null);
  const [installError, setInstallError] = useState<string | null>(null);

  // Each opening fetches the latest release again and starts from a clean slate.
  // Closing aborts the request, so the server's registry fetch is cancelled too
  // instead of holding a browser connection slot until the client timeout.
  useEffect(() => {
    if (!open) return;
    const ctrl = new AbortController();
    setPkg(null);
    setLoadError(null);
    setChecked(new Set());
    setReport(null);
    setInstallError(null);
    api.listIntentSkills(ctrl.signal).then(
      (p) => setPkg(p),
      (err) => {
        if (ctrl.signal.aborted) return;
        console.error("failed to list @tanstack/intent skills", err);
        setLoadError(err instanceof Error ? err.message : String(err));
      },
    );
    return () => ctrl.abort();
  }, [open]);

  const toggle = (name: string) =>
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });

  const install = async () => {
    const names = [...checked].sort();
    if (names.length === 0) return;
    setInstalling(true);
    setInstallError(null);
    setReport(null);
    try {
      const res = await api.installIntentSkills(names);
      setReport(res.report);
      // The TUI refreshes its slash list after an install; do the same here so
      // a new skill is usable without a reload. No scope: keep the current one.
      void reloadDynamicCommands().catch((err) =>
        console.error("failed to reload slash commands after skill install", err),
      );
    } catch (err) {
      console.error("failed to install @tanstack/intent skills", err);
      setInstallError(err instanceof Error ? err.message : String(err));
    } finally {
      setInstalling(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[80vh] p-0 overflow-hidden gap-0 !flex flex-col">
        <DialogHeader className="px-4 py-3 border-b border-border">
          <DialogTitle className="flex items-center gap-2 text-sm">
            Install {INTENT_PACKAGE_NAME} skills
            {pkg && (
              <span className="text-xs font-normal text-muted-foreground">v{pkg.version}</span>
            )}
          </DialogTitle>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto">
          {loadError && (
            <div className="px-4 py-3 text-xs text-destructive">
              Could not fetch {INTENT_PACKAGE_NAME}: {loadError}
            </div>
          )}
          {!pkg && !loadError && (
            <div className="flex items-center gap-2 px-4 py-3 text-xs text-muted-foreground">
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
              Fetching the latest release from npm…
            </div>
          )}
          {pkg &&
            pkg.skills.map((s) => {
              const on = checked.has(s.name);
              return (
                <button
                  key={s.name}
                  type="button"
                  role="checkbox"
                  aria-checked={on}
                  onClick={() => toggle(s.name)}
                  className="flex w-full items-start gap-3 border-b border-border px-4 py-2.5 text-left hover:bg-accent/40"
                >
                  <span
                    className={
                      "mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded border " +
                      (on ? "border-primary bg-primary text-primary-foreground" : "border-border")
                    }
                  >
                    {on && <Check className="w-3 h-3" />}
                  </span>
                  <span className="min-w-0">
                    <span className="block text-xs font-medium">{s.name}</span>
                    <span className="block text-xs text-muted-foreground">{s.description}</span>
                  </span>
                </button>
              );
            })}
        </div>

        {(report || installError) && (
          <div className="px-4 py-2 border-t border-border text-xs">
            {installError && <div className="text-destructive">{installError}</div>}
            {report && <pre className="whitespace-pre-wrap font-mono">{report}</pre>}
          </div>
        )}

        <div className="flex items-center justify-between gap-2 px-4 py-3 border-t border-border">
          <span className="text-xs text-muted-foreground">
            Installs to ~/.claude/skills · {checked.size} selected
          </span>
          <Button
            size="sm"
            className="h-8 gap-1.5 text-xs"
            onClick={install}
            disabled={installing || !pkg || checked.size === 0}
          >
            {installing ? (
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
            ) : (
              <Download className="w-3.5 h-3.5" />
            )}
            Install selected
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
