import { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { FolderOpen, Loader2 } from "lucide-react";
import { looksLikeAbsolutePath, normalizeUserPath } from "@/lib/outOfProjectFiles";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /**
   * Called with the trimmed path the user typed. The caller is responsible for
   * granting the path's directory and opening the file — the dialog only
   * collects and validates the input.
   */
  onSubmit: (path: string) => Promise<void> | void;
  /** The project the dialog was opened from, shown as context. */
  projectPath?: string;
  /** Registered remote target (SSH/WSL). A remote project's paths live on the
   *  remote host, so the dialog says so. */
  projectHost?: string;
  /** Pre-fills the input — used when the dialog opens from a chat file link
   *  that named a path, so the user does not have to retype it. */
  initialPath?: string;
}

/**
 * OpenPathDialog asks for an absolute path so the user can open a file that
 * lives OUTSIDE the active project (a sibling repo, ~/Downloads, /etc/hosts).
 *
 * The server confines the file surfaces to the project roots, the extra
 * allowed paths, and directories the user has explicitly granted. Opening such
 * a file therefore means GRANTING its directory first — an editor-only grant
 * that never widens the agent's file scope. The dialog explains that, because
 * a user granting access to a directory outside the project deserves to know
 * exactly what they are granting.
 */
export default function OpenPathDialog({
  open,
  onOpenChange,
  onSubmit,
  projectPath,
  projectHost,
  initialPath,
}: Props) {
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (open) {
      setValue(initialPath ?? "");
      setError("");
      setBusy(false);
    }
  }, [open, initialPath]);

  const submit = async () => {
    const path = normalizeUserPath(value);
    if (!path) {
      setError("Enter a path");
      return;
    }
    if (!looksLikeAbsolutePath(path)) {
      setError("Enter an absolute path (starting with / or ~)");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await onSubmit(path);
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Open a file outside the project</DialogTitle>
          <DialogDescription>
            {projectHost ? (
              <>
                The path is resolved on the remote host{" "}
                <span className="font-mono">{projectHost}</span>.
              </>
            ) : (
              <>
                The path is resolved on the machine running this server
                {projectPath ? (
                  <>
                    {" "}
                    (project: <span className="font-mono">{projectPath}</span>)
                  </>
                ) : null}
                .
              </>
            )}{" "}
            Its directory is granted to the <strong>file editor only</strong> — the
            agent is not given access to it, and the grant lasts only as long as
            this server runs.
          </DialogDescription>
        </DialogHeader>

        <div className="flex items-center gap-2">
          <FolderOpen className="h-4 w-4 shrink-0 text-muted-foreground" />
          <Input
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
              if (error) setError("");
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !busy) {
                e.preventDefault();
                void submit();
              }
            }}
            placeholder="/Users/you/Downloads/notes.md"
            aria-label="File path"
            disabled={busy}
            autoFocus
          />
        </div>

        {error && (
          <p role="alert" className="text-xs text-destructive">
            {error}
          </p>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={() => void submit()} disabled={busy}>
            {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            Open
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
