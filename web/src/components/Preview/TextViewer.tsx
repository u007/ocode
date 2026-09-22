import { useEffect, useState } from "react";
import FileEditor from "../Files/FileEditor";
import { api } from "../../api/client";
import { previewKindForPath } from "../../lib/previewKind";
import { languageForFile } from "../../lib/editorLanguage";
import { Button } from "../ui/button";

export default function TextViewer({
  path,
  projectRoot,
  projectHost,
  revision,
}: {
  path: string;
  projectRoot?: string;
  projectHost?: string;
  /** Live-refresh revision from the sidebar PreviewHost. Refetched SILENTLY
   *  (no loading flash) when it changes — but never while the reader has
   *  unsaved edits, which would clobber their draft. */
  revision?: number;
}) {
  const [content, setContent] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [savedTick, setSavedTick] = useState(false);
  const [isBinary, setIsBinary] = useState(false);
  const [forceEdit, setForceEdit] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setContent(null);
    setDirty(false);
    setError(null);
    setIsBinary(false);
    setForceEdit(false);
    api
      .getFileContent(path, projectRoot, projectHost)
      .then((c) => {
        if (cancelled) return;
        setContent(c.content);
        setDraft(c.content);
        setIsBinary(c.is_binary);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [path, projectRoot, projectHost]);

  // Live refresh on revision bumps (AI edited the file mid-turn). Skipped
  // while `dirty`: overwriting the draft would destroy the reader's unsaved
  // edits. The next revision after they save/reload picks the new disk state.
  useEffect(() => {
    if (revision === undefined || revision === 0 || dirty) return;
    let cancelled = false;
    api
      .getFileContent(path, projectRoot, projectHost)
      .then((c) => {
        if (cancelled || dirty) return;
        setContent(c.content);
        setDraft(c.content);
        setIsBinary(c.is_binary);
        setError(null);
      })
      .catch(() => {
        // Keep the last good content on a transient refresh failure.
      });
    return () => {
      cancelled = true;
    };
  }, [revision, dirty, path, projectRoot, projectHost]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.saveFileContent(path, draft, projectRoot, undefined, undefined, projectHost);
      setContent(draft);
      setDirty(false);
      setSavedTick(true);
      setTimeout(() => setSavedTick(false), 1500);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  };

  const previewSupported = previewKindForPath(path) !== null;
  const unsupported = !isBinary && !previewSupported && !forceEdit;

  if (error && content === null) return <div className="p-4 text-xs text-red-400">Load failed: {error}</div>;
  if (content === null) return <div className="p-4 text-xs text-muted-foreground">Loading file…</div>;

  // Binary file detected: show binary indicator + edit button
  if (isBinary && !forceEdit) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-4 p-8 text-center">
        <div className="text-2xl font-bold text-amber-500">Binary File</div>
        <p className="text-sm text-muted-foreground">This file contains binary data and cannot be previewed as text.</p>
        <Button onClick={() => setForceEdit(true)} variant="outline">Edit as text</Button>
      </div>
    );
  }

  // Unsupported format: show unsupported message + edit button
  if (unsupported && !forceEdit) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-4 p-8 text-center">
        <div className="text-2xl font-bold text-muted-foreground">Format Not Supported</div>
        <p className="text-sm text-muted-foreground">This file format is not supported for preview.</p>
        <Button onClick={() => setForceEdit(true)} variant="outline">Edit via file editor</Button>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      {(dirty || error || savedTick) && (
        <div className="flex shrink-0 items-center gap-2 border-b border-border px-2 py-1 text-xs">
          {dirty && <span className="text-amber-400">Unsaved changes</span>}
          {error && <span className="truncate text-red-400">{error}</span>}
          {savedTick && <span className="text-green-400">Saved</span>}
          <button
            type="button"
            onClick={save}
            disabled={saving || !dirty}
            className="ml-auto rounded bg-primary px-2 py-0.5 text-primary-foreground hover:opacity-90 disabled:opacity-40"
          >
            {saving ? "Saving…" : "Save"}
          </button>
        </div>
      )}
      <div className="min-h-0 flex-1">
        <FileEditor
          path={path}
          projectRoot={projectRoot}
          projectHost={projectHost}
          content={content}
          language={languageForFile(path)}
          onChange={(v) => {
            setDraft(v);
            setDirty(v !== content);
          }}
        />
      </div>
    </div>
  );
}
