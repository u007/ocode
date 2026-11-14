import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";

interface CloudEntry {
  id: string;
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
}

interface CloudConnector {
  id: string;
  provider: string;
}

export default function CloudFileTree({ onOpenFile }: { onOpenFile?: (path: string, connectorId: string) => void }) {
  const [connectors, setConnectors] = useState<CloudConnector[]>([]);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [children, setChildren] = useState<Record<string, CloudEntry[]>>({});
  const [loading, setLoading] = useState<Record<string, boolean>>({});

  useEffect(() => {
    (async () => {
      try {
        const res: any = await api.listCloudConnectors();
        const list = Array.isArray(res) ? res : res?.connectors ?? [];
        setConnectors(list);
      } catch {
        setConnectors([]);
      }
    })();
  }, []);

  const loadChildren = useCallback(async (connectorId: string, path: string) => {
    const key = `${connectorId}::${path}`;
    setLoading((l) => ({ ...l, [key]: true }));
    try {
      const res: any = await api.browseCloudConnector(connectorId, path);
      const entries = Array.isArray(res) ? res : res?.entries ?? [];
      setChildren((c) => ({ ...c, [key]: entries }));
    } catch {
      setChildren((c) => ({ ...c, [key]: [] }));
    } finally {
      setLoading((l) => ({ ...l, [key]: false }));
    }
  }, []);

  const handleUpload = async (connectorId: string, dirPath: string, file: File) => {
    try {
      const target = dirPath ? `${dirPath}/${file.name}` : file.name;
      await api.uploadCloudConnector(connectorId, target, file);
      await loadChildren(connectorId, dirPath);
    } catch (e: any) {
      alert(e?.message || "upload failed");
    }
  };

  const handleDelete = async (connectorId: string, path: string) => {
    if (!confirm(`Delete ${path}?`)) return;
    try {
      await api.deleteCloudConnector(connectorId, path);
      const parent = path.includes("/") ? path.substring(0, path.lastIndexOf("/")) : "";
      await loadChildren(connectorId, parent);
    } catch (e: any) {
      alert(e?.message || "delete failed");
    }
  };

  const toggle = (connectorId: string, path: string) => {
    const key = `${connectorId}::${path}`;
    const next = !expanded[key];
    setExpanded((e) => ({ ...e, [key]: next }));
    if (next && !children[key]) {
      loadChildren(connectorId, path);
    }
  };

  const renderEntries = (connectorId: string, path: string, depth: number): any[] => {
    const key = `${connectorId}::${path}`;
    const list = children[key] || [];
    return list.map((e) => {
      const childKey = `${connectorId}::${e.path}`;
      const isExpanded = !!expanded[childKey];
      return (
        <div key={e.path} style={{ paddingLeft: depth * 12 }}>
          <div
            className="flex items-center gap-1 cursor-pointer hover:bg-accent/40 rounded px-1 py-0.5 text-sm group"
            onClick={() => {
              if (e.is_dir) toggle(connectorId, e.path);
              else onOpenFile?.(e.path, connectorId);
            }}
          >
            <span>{e.is_dir ? (isExpanded ? "📂" : "📁") : "📄"}</span>
            <span className="truncate flex-1">{e.name}</span>
            {e.is_dir && (
              <label
                className="opacity-0 group-hover:opacity-100 text-blue-500 text-xs px-1 cursor-pointer"
                onClick={(ev) => ev.stopPropagation()}
                title="Upload"
              >
                ↑
                <input
                  type="file"
                  className="hidden"
                  onChange={(ev) => {
                    const f = ev.target.files?.[0];
                    if (f) handleUpload(connectorId, e.path, f);
                    ev.target.value = "";
                  }}
                />
              </label>
            )}
            {!e.is_dir && (
              <button
                className="opacity-0 group-hover:opacity-100 text-red-500 text-xs px-1"
                onClick={(ev) => { ev.stopPropagation(); handleDelete(connectorId, e.path); }}
                title="Delete"
              >
                ✕
              </button>
            )}
          </div>
          {e.is_dir && isExpanded && (
            <div>
              {loading[childKey] ? (
                <div style={{ paddingLeft: (depth + 1) * 12 }} className="text-xs text-gray-400">Loading…</div>
              ) : (
                renderEntries(connectorId, e.path, depth + 1)
              )}
            </div>
          )}
        </div>
      );
    });
  };

  if (connectors.length === 0) return null;

  return (
    <div className="border-t mt-2 pt-2">
      <div className="text-xs font-semibold text-gray-500 px-2 mb-1">Cloud Storage</div>
      {connectors.map((c) => {
        const rootKey = `${c.id}::`;
        const isExpanded = !!expanded[rootKey];
        return (
          <div key={c.id}>
            <div
              className="flex items-center gap-1 cursor-pointer hover:bg-accent/40 rounded px-2 py-0.5 text-sm font-medium"
              onClick={() => toggle(c.id, "")}
            >
              <span>{isExpanded ? "📂" : "📁"}</span>
              <span className="truncate flex-1">{c.id}</span>
              <span className="text-xs text-gray-400 ml-1">({c.provider})</span>
              <label
                className="text-blue-500 text-xs px-1 cursor-pointer"
                onClick={(ev) => ev.stopPropagation()}
                title="Upload to root"
              >
                ↑
                <input
                  type="file"
                  className="hidden"
                  onChange={(ev) => {
                    const f = ev.target.files?.[0];
                    if (f) handleUpload(c.id, "", f);
                    ev.target.value = "";
                  }}
                />
              </label>
            </div>
            {isExpanded && (
              <div>
                {loading[`${c.id}::`] ? (
                  <div className="text-xs text-gray-400 px-4">Loading…</div>
                ) : (
                  renderEntries(c.id, "", 1)
                )}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
