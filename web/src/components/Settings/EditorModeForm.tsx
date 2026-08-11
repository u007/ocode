import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function EditorModeForm() {
  const [editor, setEditor] = useState("");
  const [editorMode, setEditorMode] = useState("");
  const [ideMode, setIdeMode] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getEditorConfig();
      setEditor(cfg.editor);
      setEditorMode(cfg.editor_mode);
      setIdeMode(cfg.ide_mode);
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
      await api.setEditorConfig(editor, editorMode, ideMode);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Editor Mode</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Editor</label>
        <Input value={editor} onChange={(e) => setEditor(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Editor mode</label>
        <Input value={editorMode} onChange={(e) => setEditorMode(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">IDE mode</label>
        <Input value={ideMode} onChange={(e) => setIdeMode(e.target.value)} className="h-8 text-xs" />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
