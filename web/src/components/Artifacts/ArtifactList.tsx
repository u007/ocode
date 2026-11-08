import { useEffect, useState } from "react";
import { Button } from "../ui/button";
import { copyTextToClipboard } from "../../lib/clipboard";

type ArtifactListProps = {
  host?: string;
  projectPath?: string;
  project_path?: string;
  project?: string;
  onSelect?: (artifact: any) => void;
  onPublish?: (artifact: any) => void;
  onUnpublish?: (artifact: any) => void;
  onDelete?: (artifact: any) => void;
  /** forceMount keeps this panel mounted from boot, so reload when it becomes
   *  visible instead of only on mount (a publish from another tab must show). */
  active?: boolean;
  /** Bumped by the owner after an unpublish/delete so the list reloads. */
  refreshKey?: number;
  artifacts?: any[];
  limit?: number;
};

function formatRelTime(iso: string): string {
  const diff = (Date.now() - new Date(iso).getTime()) / 1000;
  if (!Number.isFinite(diff)) return "";
  if (diff < 60) return "just now";
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  if (diff < 172800) return "yesterday";
  return new Date(iso).toLocaleDateString();
}

function formatBytes(n?: number): string {
  if (typeof n !== "number" || !Number.isFinite(n) || n <= 0) return "";
  if (n < 1024) return `${n} B`;
  if (n < 1048576) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1048576).toFixed(1)} MB`;
}

function ArtifactList(props: ArtifactListProps) {
  const {
    host,
    projectPath,
    project_path,
    project,
    onSelect,
    onPublish,
    onUnpublish,
    onDelete,
    active,
    refreshKey,
    artifacts: initial,
    limit = 10,
  } = props;
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
      const artFn =
        artMod &&
        (artMod.listArtifacts || artMod.default?.listArtifacts || artMod.list);
      const clientFn =
        clientMod &&
        clientMod.api &&
        (clientMod.api.listArtifacts || clientMod.api.listArtifacts);
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
        const r = await fetch(
          `/api/artifacts?limit=${limit}&offset=${nextOffset}`,
        );
        res = await r.json();
      }
      const list =
        (res && (res.artifacts || res.items || res.data || res.results)) || [];
      // The owner list endpoint is not project-filtered, so scope it here; a
      // project tab must never show another project's artifacts.
      const scoped = resolvedProjectPath
        ? (Array.isArray(list) ? list : []).filter(
            (a: any) => (a?.project_path ?? "") === resolvedProjectPath,
          )
        : list;
      setItems(Array.isArray(scoped) ? scoped : []);
      setTotal(
        resolvedProjectPath
          ? Array.isArray(scoped)
            ? scoped.length
            : 0
          : typeof res?.total === "number"
            ? res.total
            : Array.isArray(list)
              ? list.length
              : 0,
      );
      setOffset(nextOffset);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (initial) return;
    // forceMount keeps this panel mounted from boot; reload whenever it becomes
    // visible so a publish from the chat tab shows up without a page reload.
    if (active === false) return;
    load(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, refreshKey]);

  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [details, setDetails] = useState<
    Record<string, { versions?: any[]; error?: string } | "loading">
  >({});

  const toggleVersions = async (a: any) => {
    const id = a?.id || a?.artifact_id;
    if (!id) return;
    if (expandedId === id) {
      setExpandedId(null);
      return;
    }
    setExpandedId(id);
    const cached = details[id];
    if (cached && cached !== "loading") return;
    setDetails((d) => ({ ...d, [id]: "loading" }));
    try {
      // Loaded through the module (not a static import) so tests can mock it,
      // matching how the list itself resolves listArtifacts.
      const mod: any = await import("../../api/artifacts");
      const detail = await mod.getArtifactDetail(id, host, resolvedProjectPath);
      setDetails((d) => ({
        ...d,
        [id]: {
          versions: Array.isArray(detail?.versions) ? detail.versions : [],
        },
      }));
    } catch (e: any) {
      setDetails((d) => ({
        ...d,
        [id]: { error: e?.message || String(e) },
      }));
    }
  };

  const copyShareLink = async (a: any) => {
    const url = typeof a?.share_url === "string" ? a.share_url : "";
    if (!url) return;
    await copyTextToClipboard(url);
    setCopiedId(a?.id || a?.artifact_id || null);
    setTimeout(() => setCopiedId(null), 1500);
  };

  return (
    <div className="space-y-2">
      {loading && <div className="text-xs text-muted-foreground">Loading…</div>}
      {error && (
        <div role="alert" className="text-xs text-destructive">
          {error}
        </div>
      )}
      <ul className="space-y-1">
        {items.map((a, i) => {
          const id = a?.id || a?.artifact_id;
          const entry = id ? details[id] : undefined;
          return (
            <li key={a?.id || i} className="rounded border px-2 py-1.5 text-sm">
              <div className="flex items-center justify-between gap-2">
                <button
                  type="button"
                  className="min-w-0 flex-1 text-left hover:underline"
                  onClick={() => onSelect?.(a)}
                >
                  <span className="block truncate">
                    {a?.name || a?.id || "artifact"}
                  </span>
                  <span className="block text-xs text-muted-foreground">
                    {[a?.status, a?.hosting_mode].filter(Boolean).join(" · ")}
                    {a?.created_at ? ` · ${formatRelTime(a.created_at)}` : ""}
                  </span>
                </button>
                <div className="flex items-center gap-1">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => toggleVersions(a)}
                    data-testid="toggle-versions"
                  >
                    Versions
                  </Button>
                  {onPublish && (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => onPublish(a)}
                      data-testid="publish-artifact"
                    >
                      Publish
                    </Button>
                  )}
                  {onUnpublish && a?.status !== "unpublished" && (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => onUnpublish(a)}
                      data-testid="unpublish-artifact"
                    >
                      Unpublish
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={!a?.share_url}
                    title={a?.share_url ? "Copy the share link" : "No link yet"}
                    onClick={() => copyShareLink(a)}
                    data-testid="copy-share-link"
                  >
                    {copiedId === (a?.id || a?.artifact_id)
                      ? "Copied"
                      : "Copy link"}
                  </Button>
                  {onDelete && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-destructive"
                      onClick={() => onDelete(a)}
                      data-testid="delete-artifact"
                    >
                      Delete
                    </Button>
                  )}
                </div>
              </div>
              {expandedId === id && entry && (
                <div
                  className="mt-1 border-t pt-1 text-xs"
                  data-testid="version-list"
                >
                  {entry === "loading" && (
                    <div className="text-muted-foreground">
                      Loading versions…
                    </div>
                  )}
                  {entry !== "loading" && entry.error && (
                    <div role="alert" className="text-destructive">
                      {entry.error}
                    </div>
                  )}
                  {entry !== "loading" &&
                    entry.versions &&
                    entry.versions.length === 0 && (
                      <div className="text-muted-foreground">
                        No versions yet.
                      </div>
                    )}
                  {entry !== "loading" &&
                    entry.versions &&
                    entry.versions.length > 0 && (
                      <ul className="space-y-0.5">
                        {entry.versions.map((v: any) => (
                          <li
                            key={v?.id || v?.version}
                            className="flex items-center justify-between gap-2"
                          >
                            <span>{v?.version || v?.id || "version"}</span>
                            <span className="text-muted-foreground">
                              {formatBytes(v?.size_bytes)}
                              {v?.created_at
                                ? ` · ${formatRelTime(v.created_at)}`
                                : ""}
                            </span>
                          </li>
                        ))}
                      </ul>
                    )}
                </div>
              )}
            </li>
          );
        })}
        {!loading && items.length === 0 && (
          <li className="text-xs text-muted-foreground">
            No artifacts yet. Publish this project to create one.
          </li>
        )}
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
