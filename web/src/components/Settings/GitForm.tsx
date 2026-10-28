import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Loader2 } from "lucide-react";

// Settings → Git. The Git tab (web/desktop and TUI) always runs `git pull` with
// an explicit reconcile flag, so a divergent branch never fails with
// "Need to specify how to reconcile divergent branches" — a bare pull dies that
// way whenever the repo (or, for a remote project, the host) has no
// pull.rebase/pull.ff configured. Rebase is the default; the checkbox opts into
// a merge commit.
export default function GitForm() {
  const [mergeInsteadOfRebase, setMergeInsteadOfRebase] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getGitConfig();
      // Anything other than an explicit "merge" reads as rebase, so an older
      // server that omits the key shows the actual default rather than lying.
      setMergeInsteadOfRebase(cfg.pull_strategy === "merge");
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
      await api.setGitConfig(mergeInsteadOfRebase ? "merge" : "rebase");
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
      <h2 className="text-sm font-semibold text-foreground">Git</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input
          type="checkbox"
          checked={mergeInsteadOfRebase}
          onChange={(e) => setMergeInsteadOfRebase(e.target.checked)}
        />
        Merge instead of rebase (passes --no-rebase)
      </label>
      <p className="text-xs text-muted-foreground/80">
        Pull rebases your local commits onto the upstream branch by default.
        Tick this to create a merge commit instead, which keeps your local
        commits untouched (and is the friendlier choice when the working tree
        has uncommitted changes).
      </p>
      <p className="text-xs text-muted-foreground/80">
        The flag is always passed explicitly, so neither this repository&apos;s
        git config nor a remote host&apos;s can override it. The TUI Git tab uses
        the same setting.
      </p>
      <p className="text-xs text-muted-foreground/60">
        Effective: <code>git pull {mergeInsteadOfRebase ? "--no-rebase" : "--rebase"}</code>
      </p>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
