import { memo, useEffect, useState } from "react";
import FilePicker from "../Files/FilePicker";
import PreviewSurface from "./PreviewSurface";
import { previewKindForPath, type PreviewOpenRequest } from "../../lib/previewKind";

interface SelectedFile {
  path: string;
  kind: string;
  projectRoot?: string;
  projectHost?: string;
  page?: number;
}

function PreviewTabPage({
  projectRoot,
  projectHost,
  request,
  nonce,
  onConsumeActivation,
}: {
  projectRoot?: string;
  projectHost?: string;
  /** One-shot preview activation, supplied by App on MOBILE only: the side
   *  pane that normally owns it is not rendered at that breakpoint, so this
   *  full-width sub-tab consumes it instead (AI `preview_open` tool /
   *  "Preview in sidebar"). Undefined on desktop. */
  request?: PreviewOpenRequest | null;
  /** Monotonic activation nonce; a fresh value re-applies `request` even when
   *  the path is unchanged. */
  nonce?: number;
  /** Acknowledge the activation so a remount does not replay it. */
  onConsumeActivation?: () => void;
}) {
  const [filePickerOpen, setFilePickerOpen] = useState(false);
  const [selected, setSelected] = useState<SelectedFile | null>(null);

  // Apply a mobile preview activation. Keyed on the monotonic nonce (not the
  // request identity) so re-requesting the same file still re-selects it; the
  // request itself is read at apply time.
  useEffect(() => {
    if (!nonce || !request) return;
    setSelected({
      path: request.path,
      kind: request.kind ?? previewKindForPath(request.path) ?? "text",
      projectRoot: request.projectRoot ?? projectRoot,
      projectHost: request.projectHost ?? projectHost,
      page: request.page,
    });
    onConsumeActivation?.();
    // Only the nonce is a trigger; request/projectRoot/host are read when it fires.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nonce]);

  return (
    <div className="flex h-full w-full min-h-0">
      <aside className="w-72 shrink-0 border-r border-border overflow-y-auto p-3 bg-muted/20">
        <div className="text-xs font-semibold text-muted-foreground mb-2">Files</div>
        <button
          type="button"
          onClick={() => setFilePickerOpen(true)}
          className="w-full rounded border border-border bg-background px-2 py-1.5 text-xs hover:bg-muted transition-colors text-left"
        >
          {selected ? selected.path.split("/").pop() || selected.path : "Select file…"}
        </button>
        <FilePicker
          open={filePickerOpen}
          onClose={() => setFilePickerOpen(false)}
          onOpenFile={(path, root) => {
            setSelected({ path, kind: previewKindForPath(path) || "text", projectRoot: root ?? projectRoot, projectHost });
            setFilePickerOpen(false);
          }}
          projectPath={projectRoot}
          projectHost={projectHost}
        />
      </aside>
      <main className="flex min-h-0 flex-1 flex-col overflow-hidden bg-background">
        {!selected ? (
          <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
            Select a file to preview or edit.
          </div>
        ) : (
          <PreviewSurface
            path={selected.path}
            kind={selected.kind as any}
            projectRoot={selected.projectRoot}
            projectHost={selected.projectHost}
            page={selected.page}
          />
        )}
      </main>
    </div>
  );
}

/** Props are primitives (`projectRoot`, `projectHost`) plus a one-shot
 *  activation (`request`/`nonce`/`onConsumeActivation` — the request identity
 *  is stable while pending and the callback is memoised), so a parent
 *  re-render — e.g. another tab or project becoming active — is a no-op here. */
export default memo(PreviewTabPage);
