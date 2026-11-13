import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";

interface CloudConnector {
  id: string;
  provider: string;
  root_path?: string;
  project_slug?: string;
  max_cache_bytes?: number;
}

interface CloudEntry {
  id: string;
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  mime_type: string;
}

export default function CloudConnectorsForm() {
  const [connectors, setConnectors] = useState<CloudConnector[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Add form
  const [newId, setNewId] = useState("");
  const [provider, setProvider] = useState("google-drive");
  const [rootPath, setRootPath] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [refreshToken, setRefreshToken] = useState("");

  // Browse state
  const [browseId, setBrowseId] = useState<string | null>(null);
  const [browsePath, setBrowsePath] = useState("");
  const [entries, setEntries] = useState<CloudEntry[]>([]);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res: any = await api.listCloudConnectors();
      setConnectors(Array.isArray(res) ? res : res?.connectors ?? []);
    } catch (e: any) {
      setError(e?.message || "failed to list connectors");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const handleAdd = async () => {
    if (!newId.trim()) {
      setError("id required");
      return;
    }
    setError(null);
    try {
      await api.registerCloudConnector({
        id: newId.trim(),
        provider,
        root_path: rootPath.trim(),
        secrets: {
          client_id: clientId.trim(),
          client_secret: clientSecret.trim(),
          refresh_token: refreshToken.trim(),
        },
      });
      setNewId("");
      setClientId("");
      setClientSecret("");
      setRefreshToken("");
      setRootPath("");
      await load();
    } catch (e: any) {
      setError(e?.message || "failed to register");
    }
  };

  const handleOAuthConnect = async () => {
    if (!newId.trim() || !clientId.trim()) {
      setError("id and client_id required for OAuth");
      return;
    }
    setError(null);
    try {
      const res: any = await api.startCloudOAuth({
        provider,
        connector_id: newId.trim(),
        client_id: clientId.trim(),
        client_secret: clientSecret.trim(),
      });
      if (res?.auth_url) {
        window.open(res.auth_url, "_blank");
      }
    } catch (e: any) {
      setError(e?.message || "oauth start failed");
    }
  };

  const handleRemove = async (id: string) => {
    if (!confirm(`Remove connector ${id}?`)) return;
    try {
      await api.removeCloudConnector(id);
      await load();
      if (browseId === id) {
        setBrowseId(null);
        setEntries([]);
      }
    } catch (e: any) {
      setError(e?.message || "failed to remove");
    }
  };

  const handleBrowse = async (id: string, path: string) => {
    setBrowseId(id);
    setBrowsePath(path);
    try {
      const res: any = await api.browseCloudConnector(id, path);
      setEntries(Array.isArray(res) ? res : res?.entries ?? []);
    } catch (e: any) {
      setError(e?.message || "failed to browse");
    }
  };

  return (
    <div className="space-y-4">
      <h3 className="text-sm font-semibold">Cloud storage connectors (Phase 1: Google Drive read-only)</h3>
      {error && <div className="text-red-500 text-sm">{error}</div>}
      {loading && <div className="text-sm text-gray-500">Loading…</div>}

      <div className="border rounded p-3 space-y-2">
        <div className="text-sm font-medium">Registered connectors</div>
        {connectors.length === 0 && <div className="text-sm text-gray-500">None.</div>}
        <ul className="space-y-1">
          {connectors.map((c) => (
            <li key={c.id} className="flex items-center justify-between gap-2">
              <span className="text-sm">
                {c.id} — {c.provider} {c.root_path ? `(${c.root_path})` : ""}
              </span>
              <span className="flex gap-2">
                <Button size="sm" onClick={() => handleBrowse(c.id, "")}>Browse</Button>
                <Button size="sm" variant="destructive" onClick={() => handleRemove(c.id)}>Remove</Button>
              </span>
            </li>
          ))}
        </ul>
      </div>

      {browseId && (
        <div className="border rounded p-3 space-y-2">
          <div className="flex items-center justify-between">
            <div className="text-sm font-medium">Browsing {browseId} {browsePath ? `/ ${browsePath}` : ""}</div>
            <Button size="sm" onClick={() => { setBrowseId(null); setEntries([]); }}>Close</Button>
          </div>
          <ul className="space-y-1">
            {entries.map((e) => (
              <li key={e.id} className="text-sm flex items-center justify-between">
                <span>
                  {e.is_dir ? "📁" : "📄"} {e.name}
                </span>
                {!e.is_dir && (
                  <Button size="sm" onClick={() => window.open(`/api/cloud/read?id=${encodeURIComponent(browseId!)}&path=${encodeURIComponent(e.path)}`, "_blank")}>
                    Download
                  </Button>
                )}
                {e.is_dir && (
                  <Button size="sm" onClick={() => handleBrowse(browseId!, e.path)}>Open</Button>
                )}
              </li>
            ))}
            {entries.length === 0 && <li className="text-sm text-gray-500">Empty.</li>}
          </ul>
        </div>
      )}

      <div className="border rounded p-3 space-y-2">
        <div className="text-sm font-medium">Add Google Drive connector</div>
        <Input placeholder="id (e.g. gdrive-main)" value={newId} onChange={(e) => setNewId(e.target.value)} />
        <select className="border rounded px-2 py-1 text-sm" value={provider} onChange={(e) => setProvider(e.target.value)}>
          <option value="google-drive">google-drive</option>
          <option value="onedrive">onedrive</option>
          <option value="gcs">gcs</option>
          <option value="s3">s3</option>
          <option value="dropbox">dropbox</option>
        </select>
        <Input placeholder="root folder id (optional, default root)" value={rootPath} onChange={(e) => setRootPath(e.target.value)} />
        <Input placeholder="client_id" value={clientId} onChange={(e) => setClientId(e.target.value)} />
        <Input placeholder="client_secret" value={clientSecret} onChange={(e) => setClientSecret(e.target.value)} />
        <Input placeholder="refresh_token" value={refreshToken} onChange={(e) => setRefreshToken(e.target.value)} />
        <Button onClick={handleAdd}>Add</Button>
        <Button onClick={handleOAuthConnect}>Connect via OAuth</Button>
      </div>
    </div>
  );
}
