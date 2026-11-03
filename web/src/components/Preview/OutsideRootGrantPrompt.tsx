import { useState } from "react";
import { Button } from "../ui/button";
import { api } from "../../api/client";
import { anchorForGrant } from "../../lib/outOfProjectFiles";

/** Where a granted file opens: the granted root and the path relative to it. */
export interface GrantAnchor {
  projectRoot: string;
  path: string;
}

/**
 * Shown in place of a preview when the server refuses the file as outside the
 * project root. Nothing is granted until the user clicks: the click is the
 * confirmation.
 */
export function OutsideRootGrantPrompt({
  path,
  host,
  onGrant,
}: {
  path: string;
  host?: string;
  onGrant: (anchor: GrantAnchor) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const grant = async () => {
    setBusy(true);
    setError(null);
    try {
      const result = await api.grantFilePath(path, host);
      const anchor = anchorForGrant(result);
      if (anchor === null) {
        setError(`${path} is not inside the granted folder ${result.root}.`);
        return;
      }
      onGrant(anchor);
    } catch (err) {
      console.error("Failed to grant access to path:", { path, host, err });
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-4 text-center">
      <div className="max-w-full truncate font-mono text-xs" title={path}>
        {path}
      </div>
      <p className="max-w-[260px] text-[11px] leading-relaxed text-muted-foreground">
        This file is outside the project. Access is editor-only and lasts until the server restarts.
      </p>
      <Button size="sm" variant="outline" onClick={grant} disabled={busy}>
        Grant access to its folder
      </Button>
      {error && (
        <div role="alert" className="max-w-[260px] text-[11px] text-destructive">
          {error}
        </div>
      )}
    </div>
  );
}
