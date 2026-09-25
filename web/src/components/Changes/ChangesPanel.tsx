import { memo, useCallback, useEffect, useState } from "react";
import { api } from "@/api/client";
import type { FileChange } from "@/api/types";
import ChangesFileList from "./ChangesFileList";
import ChangesDiffView from "./ChangesDiffView";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { useKeyedLoad, type LoadingEventHandler } from "@/hooks/useKeyedLoad";

const REFRESH_INTERVAL = 10_000;

interface Props {
  session?: string;
  /** SSH/WSL host of the session's project (undefined for local). Routes the
   *  list/diff/undo calls through /api/remote/{host} so a remote session's
   *  changes come from that host's server instead of the local one. */
  host?: string;
  /** True while this panel's session tab + sub-tab is frontmost. One of
   *  these is mounted per open session tab (hidden via CSS), so background
   *  instances must not poll — N hidden tabs would hammer /api/changes. */
  active?: boolean;
  loadingKey?: string;
  onLoadingEvent?: LoadingEventHandler;
}

type PendingUndo = { path: string; kind: "file" | "block" } | null;

function ChangesPanel({ session, host, active = true, loadingKey, onLoadingEvent }: Props) {
  const runKeyedLoad = useKeyedLoad(loadingKey, onLoadingEvent);
  const [files, setFiles] = useState<FileChange[]>([]);
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pendingUndo, setPendingUndo] = useState<PendingUndo>(null);

  const refresh = useCallback(() => {
    setLoading(true);
    return runKeyedLoad(
      () => api.listChanges(session, host),
      {
        empty: (files) => files.length === 0,
        retry: () => {
          void refresh();
        },
      },
    ).then((result) => {
      if (result.status === "success" || result.status === "empty") {
        setFiles(result.value);
        setError(null);
        setLoading(false);
      } else if (result.status === "error") {
        console.error("Changes load failed:", result.error);
        setError(result.message);
        setLoading(false);
      }
    });
  }, [session, host, loadingKey, runKeyedLoad]);

  useEffect(() => {
    refresh();
    // Poll only while frontmost; activation refreshes once so re-opening a
    // tab shows current data before the next tick.
    if (!active) return;
    const interval = setInterval(refresh, REFRESH_INTERVAL);
    return () => clearInterval(interval);
  }, [refresh, active]);

  const confirmUndo = useCallback(async () => {
    if (!pendingUndo) return;
    try {
      if (pendingUndo.kind === "file") {
        await api.undoChangeFile(session, pendingUndo.path, host);
      } else {
        await api.undoChangeBlock(session, pendingUndo.path, host);
      }
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Undo failed");
    } finally {
      setPendingUndo(null);
    }
  }, [pendingUndo, session, host, refresh]);

  if (loading && files.length === 0) {
    return <div className="p-3 text-xs text-muted-foreground">Loading changes…</div>;
  }

  return (
    <div className="flex flex-col h-full">
      <div className="p-3 border-b border-border">
        <label className="text-xs text-muted-foreground uppercase tracking-wider">Changes</label>
        {error && <div className="mt-1 text-xs text-red-400">{error}</div>}
      </div>
      <div className="flex-1 overflow-y-auto">
        <ChangesFileList
          files={files}
          selectedPath={selectedPath}
          onSelect={(path) => setSelectedPath(path === selectedPath ? null : path)}
          onUndoFile={(path) => setPendingUndo({ path, kind: "file" })}
          onUndoBlock={(path) => setPendingUndo({ path, kind: "block" })}
        />
      </div>
      {selectedPath && (
        <div className="border-t border-border max-h-[40vh] overflow-y-auto">
          <ChangesDiffView session={session} host={host} path={selectedPath} />
        </div>
      )}
      <Dialog open={pendingUndo !== null} onOpenChange={(open) => !open && setPendingUndo(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {pendingUndo?.kind === "file"
                ? `Undo ${pendingUndo.path} to pre-session state?`
                : `Undo the most recent change to ${pendingUndo?.path}?`}
            </DialogTitle>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setPendingUndo(null)}
              data-dialog-default-action
            >
              Cancel
            </Button>
            <Button variant="destructive" onClick={confirmUndo}>
              Undo
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/** Session/key props are primitives and the event callback is stable, so a
 *  parent re-render — e.g. another tab becoming active — does not re-render a
 *  hidden ChangesPanel. */
export default memo(ChangesPanel);
