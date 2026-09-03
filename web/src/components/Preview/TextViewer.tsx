import { useEffect, useState } from "react";
import FileEditor from "../Files/FileEditor";
import { api } from "../../api/client";

function languageFor(path: string): string {
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();
  const map: Record<string, string> = {
    ts: "typescript", tsx: "typescript", js: "javascript", jsx: "javascript",
    go: "go", py: "python", json: "json", yaml: "yaml", yml: "yaml",
    html: "html", css: "css", md: "markdown", sh: "shell", sql: "sql",
  };
  return map[ext] ?? "plaintext";
}

/**
 * Editable text/code file inside the sidebar preview (Monaco — the same
 * editor component the Files tab uses). Save writes through
 * PUT /api/files/content with the project's save guard.
 */
export default function TextViewer({ path, projectRoot }: { path: string; projectRoot?: string }) {
  const [content, setContent] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [savedTick, setSavedTick] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setContent(null);
    setDirty(false);
    setError(null);
    api
      .getFileContent(path, projectRoot)
      .then((c) => {
        if (cancelled) return;
        setContent(c);
        setDraft(c);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [path, projectRoot]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await api.saveFileContent(path, draft, projectRoot);
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

  if (error && content === null) return <div className="p-4 text-xs text-red-400">Load failed: {error}</div>;
  if (content === null) return <div className="p-4 text-xs text-muted-foreground">Loading file…</div>;

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
          content={content}
          language={languageFor(path)}
          onChange={(v) => {
            setDraft(v);
            setDirty(v !== content);
          }}
        />
      </div>
    </div>
  );
}
