import { useEffect, useState } from "react";
import { api } from "@/api/client";
import type { GitConflictChunkChoice, GitConflictChunks } from "@/api/types";
import { Button } from "@/components/ui/button";
import { conflictSideLabels } from "./conflictSideLabels";

interface Props {
  /** Repo-relative path of the conflicted file. */
  path: string;
  /** Project root for the requests; undefined is the default project. */
  project?: string;
  /** The operation the panel shows. Drives the rebase wording below. */
  operationKind?: string;
  busy: boolean;
  /** Bumped by the parent after each chunk action so the list is re-read. */
  refreshKey: number;
  /** Keeps one side of one chunk. The parent runs it through its shared
   *  error/notice contract and bumps refreshKey when it settles. */
  onChoose: (
    index: number,
    choice: GitConflictChunkChoice,
    fingerprint: string,
  ) => Promise<void>;
}

/** Block-by-block resolution for one conflicted file. The list is read lazily
 *  (only when the row is expanded) and re-read after every action, because
 *  each action rewrites the file and renumbers the blocks that follow it.
 *
 *  Button and pane wording comes from `conflictSideLabels`, the same source as
 *  the whole-file buttons, so a rebase shows "Keep upstream" and "Keep my
 *  commit" here too. The pane headers name the side the same way, so the
 *  marker label `HEAD` is never the only hint of which side is which. */
export function ConflictChunks({
  path,
  project,
  operationKind,
  busy,
  refreshKey,
  onChoose,
}: Props) {
  const [data, setData] = useState<GitConflictChunks | null>(null);
  // The refreshKey the current `data` was read for. Until it matches the prop,
  // the buttons would carry a fingerprint the file may no longer have.
  const [loadedFor, setLoadedFor] = useState<number | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const labels = conflictSideLabels(operationKind);

  useEffect(() => {
    let cancelled = false;
    api.gitConflictChunks(path, project).then(
      (next) => {
        if (cancelled) return;
        setData(next);
        setLoadedFor(refreshKey);
        setLoadError(null);
      },
      (e: unknown) => {
        if (cancelled) return;
        setLoadError(e instanceof Error ? e.message : "could not read conflict blocks");
      },
    );
    return () => {
      cancelled = true;
    };
  }, [path, project, refreshKey]);

  if (loadError) {
    return (
      <div role="alert" className="px-3 pb-2 text-xs text-red-400">
        {loadError}
      </div>
    );
  }
  if (!data) {
    return <div className="px-3 pb-2 text-xs text-muted-foreground">Loading blocks…</div>;
  }
  if (data.chunks.length === 0) {
    return (
      <div
        data-testid="git-conflict-chunks-empty"
        className="px-3 pb-2 text-xs text-muted-foreground"
      >
        No conflict blocks in this file. Use the whole-file buttons above.
      </div>
    );
  }

  const count = data.chunks.length;
  const current = loadedFor === refreshKey;
  const actionsOff = busy || !current;
  return (
    <div data-testid="git-conflict-chunks" className="px-3 pb-2 space-y-2">
      {data.chunks.map((chunk, index) => (
        <div
          key={chunk.start_line}
          data-testid="git-conflict-chunk"
          className="border border-border rounded text-xs"
        >
          <div className="px-2 py-1 text-muted-foreground border-b border-border">
            Block {index + 1} of {count} · lines {chunk.start_line}–{chunk.end_line}
          </div>
          <ChunkPane
            title={labels.oursPane}
            marker={chunk.ours_label}
            body={chunk.ours}
          />
          <ChunkPane
            title={labels.theirsPane}
            marker={chunk.theirs_label}
            body={chunk.theirs}
          />
          <div className="flex flex-wrap gap-1 p-2">
            <Button
              size="sm"
              variant="outline"
              className="h-6 px-2 text-[11px]"
              disabled={actionsOff}
              title={labels.oursTip}
              onClick={() => void onChoose(index, "ours", data.fingerprint)}
            >
              {labels.ours}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-6 px-2 text-[11px]"
              disabled={actionsOff}
              title={labels.theirsTip}
              onClick={() => void onChoose(index, "theirs", data.fingerprint)}
            >
              {labels.theirs}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-6 px-2 text-[11px]"
              disabled={actionsOff}
              title="Keep both sides, ours first, then theirs"
              onClick={() => void onChoose(index, "both", data.fingerprint)}
            >
              Keep both
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
}

function ChunkPane({ title, marker, body }: { title: string; marker: string; body: string }) {
  return (
    <div className="px-2 py-1">
      <div className="text-[11px] text-muted-foreground">
        {marker ? `${title} · ${marker}` : title}
      </div>
      <pre className="font-mono whitespace-pre-wrap break-words text-[11px] max-h-40 overflow-auto">
        {body === "" ? "(no lines)" : body}
      </pre>
    </div>
  );
}
