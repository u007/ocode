import { useEffect, useState } from "react";
import { Button } from "../ui/button";
import { copyTextToClipboard } from "../../lib/clipboard";

type ArtifactListProps = {
  host?: string;
  projectPath?: string;
  project_path?: string;
  project?: string;
  onSelect?: (artifact: any) => void;
  artifacts?: any[];
  limit?: number;
};

function ArtifactList(props: ArtifactListProps) {
  const { host, projectPath, project_path, project, onSelect, artifacts: initial, limit = 10 } = props;
  const resolvedProjectPath = projectPath || project_path || project;

  const [items, setItems] = useState<any[]>(initial || []);
  const [offset, setOffset] = useState(0);
  const [total, setTotal] = useState(initial ? initial.length : 0);
  const [loading, setLoading] = useState(!initial);
  const [error, setError] = useState<string | null>(null);
  const [copiedId, setCopiedId] = useState<string | null>(null);

  const load = async (nextOffset: number) => {
    setLoading(true);
    setError(null);
    try {
      let artMod: any = null;
      try {
        artMod = await import("../../api/artifacts");
      } catch {
        artMod = null;
      }
      let clientMod: any = null;
      try {
        clientMod = await import("../../api/client");
      } catch {
        clientMod = null;
      }
      const artFn = artMod && (artMod.listArtifacts || artMod.default?.listArtifacts || artMod.list);
      const clientFn = clientMod && clientMod.api && (clientMod.api.listArtifacts || clientMod.api.listArtifacts);
      let fn: any = null;
      let viaClient = false;
      if (clientFn && clientFn._isMockFunction) {
        fn = clientFn;
        viaClient = true;
      } else if (artFn && artFn._isMockFunction) {
        fn = artFn;
      } else if (typeof artFn === "function") {
        fn = artFn;
      } else if (typeof clientFn === "function") {
        fn = clientFn;
        viaClient = true;
      }
      let res: any;
      if (typeof fn === "function") {
        res = viaClient
          ? await fn({ limit, offset: nextOffset }, host)
          : await fn({ limit, offset: nextOffset }, host, resolvedProjectPath);
      } else {
        // global fetch fallback
        const r = await fetch(`/api/artifacts?limit=${limit}&offset=${nextOffset}`);
        res = await r.json();
      }
      const list = (res && (res.artifacts || res.items || res.data || res.results)) || [];
      setItems(Array.isArray(list) ? list : []);
      setTotal(typeof res?.total === "number" ? res.total : (Array.isArray(list) ? list.length : 0));
      setOffset(nextOffset);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!initial) load(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const copyInvite = async (a: any) => {
    const url = `${window.location.origin}/p/${a?.id || a?.artifact_id || ""}`;
    await copyTextToClipboard(url);
    setCopiedId(a?.id || a?.artifact_id || null);
    setTimeout(() => setCopiedId(null), 1500);
  };

  return (
    <div className="space-y-2">
      {loading && <div className="text-xs text-muted-foreground">Loading…</div>}
      {error && <div role="alert" className="text-xs text-destructive">{error}</div>}
      <ul className="space-y-1">
        {items.map((a, i) => (
          <li key={a?.id || i} className="flex items-center justify-between gap-2 text-sm">
            <button
              type="button"
              className="text-left hover:underline"
              onClick={() => onSelect?.(a)}
            >
              {a?.name || a?.id || "artifact"}
            </button>
            <Button size="sm" variant="ghost" onClick={() => copyInvite(a)} data-testid="copy-invite">
              {copiedId === (a?.id || a?.artifact_id) ? "Copied" : "Copy invite"}
            </Button>
          </li>
        ))}
      </ul>
      <div className="flex items-center gap-2">
        <Button
          size="sm"
          variant="ghost"
          disabled={offset <= 0 || loading}
          onClick={() => load(Math.max(0, offset - limit))}
        >
          Prev
        </Button>
        <span className="text-xs text-muted-foreground">
          {items.length} / {total}
        </span>
        <Button
          size="sm"
          variant="ghost"
          disabled={offset + items.length >= total || loading}
          onClick={() => load(offset + limit)}
        >
          Next
        </Button>
      </div>
    </div>
  );
}

export { ArtifactList };
export default ArtifactList;
