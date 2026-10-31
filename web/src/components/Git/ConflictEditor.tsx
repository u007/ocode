import { useCallback, useEffect, useRef, useState } from "react";
import Editor, { type OnMount } from "@monaco-editor/react";
import type { editor } from "monaco-editor";
import { api } from "@/api/client";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import "../../lib/monaco-setup";
import { conflictSideLabels } from "./conflictSideLabels";
import { parseBlocks, resolveBlockText, type Block, type Choice } from "./conflictBlocks";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Repo-relative path of the conflicted file. */
  path: string;
  project?: string;
  operationKind?: string;
  /** Stages the saved file (the whole-file "mark" resolution). */
  onMarkResolved: (path: string) => Promise<void>;
}

/** Full-document conflict editor: the whole file in Monaco, every conflict
 *  block highlighted with its own Keep buttons, prev/next to walk the blocks.
 *  Choices edit the buffer (one undo step each), so hand edits and block
 *  choices combine; Save writes the file and "Save & mark resolved" stages it
 *  once no markers remain. Local projects only, like the block list. */
export function ConflictEditor({
  open,
  onOpenChange,
  path,
  project,
  operationKind,
  onMarkResolved,
}: Props) {
  const labels = conflictSideLabels(operationKind);
  const labelsRef = useRef(labels);
  labelsRef.current = labels;

  const edRef = useRef<editor.IStandaloneCodeEditor | null>(null);
  const monacoRef = useRef<typeof import("monaco-editor") | null>(null);
  const decoRef = useRef<editor.IEditorDecorationsCollection | null>(null);
  const zonesRef = useRef<string[]>([]);
  const blocksRef = useRef<Block[]>([]);

  const [initial, setInitial] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [blocks, setBlocks] = useState<Block[]>([]);
  const [current, setCurrent] = useState(0);
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setInitial(null);
    setLoadError(null);
    setDirty(false);
    setSaveError(null);
    api.getFileContent(path, project).then(
      (res) => {
        if (cancelled) return;
        if (res.is_binary) setLoadError("Binary file: use the whole-file buttons.");
        else setInitial(res.content);
      },
      (e: unknown) => {
        if (cancelled) return;
        setLoadError(e instanceof Error ? e.message : "could not read file");
      },
    );
    return () => {
      cancelled = true;
    };
  }, [open, path, project]);

  const choose = useCallback((block: Block, choice: Choice) => {
    const ed = edRef.current;
    const model = ed?.getModel();
    const monaco = monacoRef.current;
    if (!ed || !model || !monaco) return;
    const lines = model.getValue().split("\n");
    const replacement = resolveBlockText(lines, block, choice);
    const range = new monaco.Range(block.start, 1, block.end, model.getLineMaxColumn(block.end));
    ed.executeEdits("conflict-choice", [{ range, text: replacement.join("\n") }]);
    ed.pushUndoStop();
  }, []);

  // Decorations + the Keep-button strip above each block, rebuilt on every
  // buffer change. Zones are DOM, not React: Monaco owns their layout.
  const refresh = useCallback(() => {
    const ed = edRef.current;
    const monaco = monacoRef.current;
    const model = ed?.getModel();
    if (!ed || !monaco || !model) return;
    const found = parseBlocks(model.getValue());
    blocksRef.current = found;
    setBlocks(found);
    setCurrent((c) => Math.min(c, Math.max(found.length - 1, 0)));

    const decos: editor.IModelDeltaDecoration[] = [];
    for (const b of found) {
      const oursEnd = (b.base || b.sep) - 1;
      decos.push(
        { range: new monaco.Range(b.start, 1, b.start, 1), options: { isWholeLine: true, className: "conflict-marker-line" } },
        { range: new monaco.Range(b.start + 1, 1, oursEnd, 1), options: { isWholeLine: true, className: "conflict-ours-line" } },
        { range: new monaco.Range(b.sep + 1, 1, b.end - 1, 1), options: { isWholeLine: true, className: "conflict-theirs-line" } },
        { range: new monaco.Range(b.sep, 1, b.sep, 1), options: { isWholeLine: true, className: "conflict-marker-line" } },
        { range: new monaco.Range(b.end, 1, b.end, 1), options: { isWholeLine: true, className: "conflict-marker-line" } },
      );
    }
    decoRef.current?.set(decos);

    ed.changeViewZones((accessor) => {
      for (const id of zonesRef.current) accessor.removeZone(id);
      zonesRef.current = found.map((b, i) => {
        const node = document.createElement("div");
        node.className = "flex items-center gap-1 px-2 text-[11px]";
        node.style.height = "22px";
        const tag = document.createElement("span");
        tag.className = "text-muted-foreground mr-1";
        tag.textContent = `Block ${i + 1} of ${found.length}`;
        node.appendChild(tag);
        const l = labelsRef.current;
        for (const [choice, text, tip] of [
          ["ours", l.ours, l.oursTip],
          ["theirs", l.theirs, l.theirsTip],
          ["both", "Keep both", "Keep both sides, ours first, then theirs"],
        ] as const) {
          const btn = document.createElement("button");
          btn.type = "button";
          btn.textContent = text;
          btn.title = tip;
          btn.className = "px-1.5 rounded border border-border hover:bg-muted/60";
          btn.onclick = () => choose(b, choice);
          node.appendChild(btn);
        }
        return accessor.addZone({ afterLineNumber: b.start - 1, heightInPx: 22, domNode: node });
      });
    });
  }, [choose]);

  const handleMount: OnMount = useCallback(
    (ed, monaco) => {
      edRef.current = ed;
      monacoRef.current = monaco;
      decoRef.current = ed.createDecorationsCollection();
      refresh();
      const first = blocksRef.current[0];
      if (first) ed.revealLineInCenter(first.start);
    },
    [refresh],
  );

  const go = (delta: number) => {
    if (blocks.length === 0) return;
    const next = (current + delta + blocks.length) % blocks.length;
    setCurrent(next);
    edRef.current?.revealLineInCenter(blocks[next].start);
  };

  const save = async (): Promise<boolean> => {
    const ed = edRef.current;
    if (!ed) return false;
    setBusy(true);
    setSaveError(null);
    try {
      await api.saveFileContent(path, ed.getValue(), project);
      setDirty(false);
      return true;
    } catch (e) {
      console.error(`conflict editor: saving ${path} failed`, e);
      setSaveError(e instanceof Error ? e.message : "save failed");
      return false;
    } finally {
      setBusy(false);
    }
  };

  const saveAndMark = async () => {
    if (!(await save())) return;
    await onMarkResolved(path);
    onOpenChange(false);
  };

  const dark =
    typeof document !== "undefined" && document.documentElement.classList.contains("dark");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-5xl w-[92vw] h-[85vh] flex flex-col gap-2 p-3">
        <DialogHeader>
          <DialogTitle className="font-mono text-sm">{path}</DialogTitle>
          <DialogDescription className="text-xs">
            Pick a side for each block, or edit the text directly. Nothing is staged until you
            save and mark resolved.
          </DialogDescription>
        </DialogHeader>
        <style>{`
          .conflict-ours-line { background: rgba(34,197,94,0.14); }
          .conflict-theirs-line { background: rgba(59,130,246,0.14); }
          .conflict-marker-line { background: rgba(148,163,184,0.22); }
        `}</style>
        <div className="flex items-center gap-2 text-xs">
          <span data-testid="conflict-editor-count">
            {blocks.length === 0 ? "No conflict blocks left" : `Block ${current + 1} of ${blocks.length}`}
          </span>
          <Button size="sm" variant="outline" className="h-6 px-2 text-[11px]" disabled={blocks.length === 0} onClick={() => go(-1)}>
            Previous
          </Button>
          <Button size="sm" variant="outline" className="h-6 px-2 text-[11px]" disabled={blocks.length === 0} onClick={() => go(1)}>
            Next
          </Button>
          <span className="flex-1" />
          {saveError && <span role="alert" className="text-red-400">{saveError}</span>}
          <Button size="sm" variant="outline" className="h-6 px-2 text-[11px]" disabled={busy || !dirty} onClick={() => void save()}>
            Save
          </Button>
          <Button
            size="sm"
            className="h-6 px-2 text-[11px]"
            disabled={busy || blocks.length > 0 || initial === null}
            title={blocks.length > 0 ? "Resolve every block first" : "Save the file and stage it"}
            onClick={() => void saveAndMark()}
          >
            Save &amp; mark resolved
          </Button>
        </div>
        <div className="flex-1 min-h-0 border border-border rounded overflow-hidden">
          {loadError ? (
            <div role="alert" className="p-3 text-xs text-red-400">{loadError}</div>
          ) : initial === null ? (
            <div className="p-3 text-xs text-muted-foreground">Loading file…</div>
          ) : (
            <Editor
              key={`${project ?? ""}::${path}`}
              defaultValue={initial}
              onMount={handleMount}
              onChange={() => {
                setDirty(true);
                refresh();
              }}
              theme={dark ? "vs-dark" : "light"}
              options={{ minimap: { enabled: false }, scrollBeyondLastLine: false, automaticLayout: true }}
            />
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
