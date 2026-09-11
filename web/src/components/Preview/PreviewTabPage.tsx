import { useState } from "react";
import FilePicker from "../Files/FilePicker";
import PreviewSurface from "./PreviewSurface";
import { previewKindForPath } from "../../lib/previewKind";

export default function PreviewTabPage() {
  const [filePickerOpen, setFilePickerOpen] = useState(false);
  const [selected, setSelected] = useState<{ path: string; kind: string; projectRoot?: string } | null>(null);

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
            setSelected({ path, kind: previewKindForPath(path) || "text", projectRoot: root });
            setFilePickerOpen(false);
          }}
        />
      </aside>
      <main className="flex-1 min-w-0 min-h-0 bg-background">
        {!selected ? (
          <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
            Select a file to preview or edit.
          </div>
        ) : (
          <PreviewSurface
            path={selected.path}
            kind={selected.kind as any}
            projectRoot={selected.projectRoot}
          />
        )}
      </main>
    </div>
  );
}
