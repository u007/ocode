import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AlertCircle,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Cloud,
  CloudOff,
  Database,
  ExternalLink,
  FolderOpen,
  HardDrive,
  Link2,
  Loader2,
  RefreshCw,
  Trash2,
  Unplug,
} from "lucide-react";
import { api } from "../../api/client";
import ConfirmDialog from "../common/ConfirmDialog";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Separator } from "../ui/separator";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "../ui/tooltip";

interface HubConnection {
  provider: string;
  connection_id: string;
  root_path?: string;
  expires_at?: string;
}

interface LegacyConnector {
  id: string;
  provider: string;
  root_path?: string;
}

interface CloudEntry {
  id: string;
  name: string;
  path: string;
  is_dir: boolean;
}

type ProviderStatus = "active" | "expired" | "local-only" | "not-connected";
type HubStatus = "connected" | "not-logged-in" | "local-only";

interface ProviderRow {
  provider: string;
  status: ProviderStatus;
  rootPath?: string;
  expiresAt?: string;
  connectionID?: string;
  localID?: string;
}

const HUB_CONNECTOR_ID = "kakiit";
const CONNECT_PROVIDERS = ["dropbox", "google-drive", "onedrive", "gcs", "s3"];

const PROVIDER_LABELS: Record<string, string> = {
  dropbox: "Dropbox",
  "google-drive": "Google Drive",
  onedrive: "OneDrive",
  gcs: "Google Cloud Storage",
  s3: "S3",
};

const STATUS_BADGES: Record<
  ProviderStatus,
  {
    label: string;
    variant: "default" | "destructive" | "secondary" | "outline";
    icon: typeof Cloud;
  }
> = {
  active: { label: "Active", variant: "default", icon: CheckCircle2 },
  expired: { label: "Expired", variant: "destructive", icon: AlertCircle },
  "local-only": { label: "Local-only", variant: "secondary", icon: HardDrive },
  "not-connected": {
    label: "Not connected",
    variant: "outline",
    icon: CloudOff,
  },
};

function providerLabel(provider: string): string {
  return (
    PROVIDER_LABELS[provider] ??
    provider.replace(/-/g, " ").replace(/\b\w/g, (c) => c.toUpperCase())
  );
}

function ProviderIcon({ provider }: { provider: string }) {
  if (provider === "gcs") return <Database className="h-4 w-4" />;
  if (provider === "s3") return <HardDrive className="h-4 w-4" />;
  return <Cloud className="h-4 w-4" />;
}

function StatusBadge({ status }: { status: ProviderStatus }) {
  const { label, variant, icon: Icon } = STATUS_BADGES[status];
  return (
    <Badge variant={variant} className="gap-1">
      <Icon className="h-3 w-3" aria-hidden="true" />
      <span>{label}</span>
    </Badge>
  );
}

function base64url(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

function randomToken(byteLength: number): string {
  return base64url(crypto.getRandomValues(new Uint8Array(byteLength)));
}

async function pkceChallenge(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(verifier),
  );
  return base64url(new Uint8Array(digest));
}

function normalizeHub(value: unknown): HubConnection[] {
  if (Array.isArray(value)) return value as HubConnection[];
  return ((value as { connections?: HubConnection[] })?.connections ??
    []) as HubConnection[];
}

function normalizeLocal(value: unknown): LegacyConnector[] {
  if (Array.isArray(value)) return value as LegacyConnector[];
  return ((value as { connectors?: LegacyConnector[] })?.connectors ??
    []) as LegacyConnector[];
}

/**
 * `project` and `host` are accepted as props but omitted by SettingsPanel on
 * purpose: Settings is a global surface by convention. A project-scoped
 * surface passes them, and both are threaded into every hub call — dropping
 * either would route a remote project's token work to the wrong machine.
 */
export default function CloudConnectorsForm({
  project = "",
  host,
}: {
  project?: string;
  host?: string;
}) {
  const [hubConnections, setHubConnections] = useState<HubConnection[]>([]);
  const [localConnectors, setLocalConnectors] = useState<LegacyConnector[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [connecting, setConnecting] = useState(false);

  const [browseRow, setBrowseRow] = useState<ProviderRow | null>(null);
  const [browsePath, setBrowsePath] = useState("");
  const [entries, setEntries] = useState<CloudEntry[]>([]);

  const [disconnectTarget, setDisconnectTarget] = useState<ProviderRow | null>(
    null,
  );
  const [removeTarget, setRemoveTarget] = useState<LegacyConnector | null>(
    null,
  );

  const [showLegacy, setShowLegacy] = useState(false);
  const [newId, setNewId] = useState("");
  const [provider, setProvider] = useState("dropbox");
  const [rootPath, setRootPath] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [refreshToken, setRefreshToken] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    const [hubResult, localResult] = await Promise.allSettled([
      api.listHubCloudConnections(project, host),
      api.listCloudConnectors(host),
    ]);
    if (hubResult.status === "fulfilled") {
      setHubConnections(normalizeHub(hubResult.value));
    } else {
      setHubConnections([]);
      setError("Kakiit is unreachable — showing local connections only.");
    }
    if (localResult.status === "fulfilled") {
      setLocalConnectors(normalizeLocal(localResult.value));
    }
    setLoading(false);
  }, [project, host]);

  useEffect(() => {
    void load();
  }, [load]);

  const rows = useMemo<ProviderRow[]>(() => {
    const byProvider = new Map<string, ProviderRow>();
    const now = Date.now();
    for (const connection of hubConnections) {
      const expiry = connection.expires_at
        ? Date.parse(connection.expires_at)
        : NaN;
      byProvider.set(connection.provider, {
        provider: connection.provider,
        status: Number.isNaN(expiry) || expiry > now ? "active" : "expired",
        rootPath: connection.root_path,
        expiresAt: connection.expires_at,
        connectionID: connection.connection_id,
      });
    }
    for (const connector of localConnectors) {
      if (byProvider.has(connector.provider)) continue;
      byProvider.set(connector.provider, {
        provider: connector.provider,
        status: "local-only",
        rootPath: connector.root_path,
        localID: connector.id,
      });
    }
    return [...byProvider.values()].sort((a, b) =>
      a.provider.localeCompare(b.provider),
    );
  }, [hubConnections, localConnectors]);

  const hubStatus: HubStatus =
    hubConnections.length > 0
      ? "connected"
      : localConnectors.length > 0
        ? "local-only"
        : "not-logged-in";

  const statusLabel =
    hubStatus === "connected"
      ? "Connected via kakiit"
      : hubStatus === "local-only"
        ? "Local only"
        : "Not logged in to kakiit";

  const handleConnect = async () => {
    setError(null);
    setConnecting(true);
    try {
      const verifier = randomToken(32);
      // shortcut: the verifier is paired with the callback's redeem call by
      // client_state in the OAuth handler path; this UI only starts the flow.
      const codeChallenge = await pkceChallenge(verifier);
      const clientState = `${encodeURIComponent(project)}.${host ? encodeURIComponent(host) : ""}.${randomToken(12)}`;
      const res = await api.startHubCloudOAuth(
        project,
        {
          provider: "dropbox",
          connector_id: HUB_CONNECTOR_ID,
          client_state: clientState,
          code_challenge: codeChallenge,
        },
        host,
      );
      if (res?.auth_url) window.open(res.auth_url, "_blank");
    } catch (e: any) {
      setError(e?.message || "failed to start kakiit connection");
    } finally {
      setConnecting(false);
    }
  };

  const handleRefresh = async (row: ProviderRow) => {
    if (!row.connectionID) return;
    setError(null);
    try {
      await api.refreshHubCloudToken(project, row.connectionID, host);
      await load();
    } catch (e: any) {
      setError(e?.message || "failed to refresh token");
    }
  };

  const handleBrowse = async (row: ProviderRow, path = "") => {
    const id = row.connectionID ?? row.localID;
    if (!id) return;
    setBrowseRow(row);
    setBrowsePath(path);
    try {
      const res: any = await api.browseCloudConnector(id, path, host);
      setEntries(Array.isArray(res) ? res : (res?.entries ?? []));
    } catch (e: any) {
      setError(e?.message || "failed to browse");
    }
  };

  const confirmDisconnect = async () => {
    const row = disconnectTarget;
    if (!row?.connectionID) return;
    // Reject on failure so ConfirmDialog keeps the dialog open and shows why.
    await api.disconnectHubCloud(project, row.connectionID, host);
    setDisconnectTarget(null);
    await load();
  };

  const confirmRemoveLocal = async () => {
    const connector = removeTarget;
    if (!connector) return;
    await api.removeCloudConnector(connector.id, host);
    setRemoveTarget(null);
    if (browseRow?.localID === connector.id) {
      setBrowseRow(null);
      setEntries([]);
    }
    await load();
  };

  const handleAdd = async () => {
    if (!newId.trim()) {
      setError("id required");
      return;
    }
    setError(null);
    try {
      await api.registerCloudConnector(
        {
          id: newId.trim(),
          provider,
          root_path: rootPath.trim(),
          secrets: {
            client_id: clientId.trim(),
            client_secret: clientSecret.trim(),
            refresh_token: refreshToken.trim(),
          },
        },
        host,
      );
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

  const handleLocalOAuth = async () => {
    if (!newId.trim() || !clientId.trim()) {
      setError("id and client_id required for OAuth");
      return;
    }
    setError(null);
    try {
      const res: any = await api.startCloudOAuth(
        {
          provider,
          connector_id: newId.trim(),
          client_id: clientId.trim(),
          client_secret: clientSecret.trim(),
        },
        host,
      );
      if (res?.auth_url) window.open(res.auth_url, "_blank");
    } catch (e: any) {
      setError(e?.message || "oauth start failed");
    }
  };

  return (
    <TooltipProvider delayDuration={200}>
      <div className="space-y-5">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Cloud
              className="h-4 w-4 text-muted-foreground"
              aria-hidden="true"
            />
            <div>
              <h3 className="text-sm font-semibold">
                Cloud storage connectors
              </h3>
              <p
                className="text-xs text-muted-foreground"
                data-testid="cloud-hub-status"
              >
                {statusLabel}
              </p>
            </div>
          </div>
          {hubStatus !== "local-only" && (
            <Button size="sm" onClick={handleConnect} disabled={connecting}>
              {connecting ? (
                <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" />
              ) : (
                <Link2 className="h-3 w-3" aria-hidden="true" />
              )}
              Connect via kakiit
            </Button>
          )}
        </header>

        <Separator />

        {error && (
          <div
            role="alert"
            className="flex items-center gap-2 text-sm text-destructive"
          >
            <AlertCircle className="h-4 w-4" aria-hidden="true" />
            {error}
          </div>
        )}
        {loading && (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" />
            Loading…
          </div>
        )}

        <section className="space-y-2">
          <h4 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Connections
          </h4>
          {rows.length === 0 ? (
            <div
              data-testid="cloud-connectors-empty"
              className="flex flex-col items-center gap-2 rounded-md border border-dashed border-border p-8 text-center text-muted-foreground"
            >
              <CloudOff className="h-8 w-8" aria-hidden="true" />
              <p className="text-sm">
                No cloud connections yet. Connect a provider via kakiit to get
                started.
              </p>
            </div>
          ) : (
            <ul className="space-y-2">
              {rows.map((row) => (
                <li
                  key={row.provider}
                  data-testid={`cloud-provider-row-${row.provider}`}
                  data-provider={row.provider}
                  className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border p-3"
                >
                  <div className="flex min-w-0 items-center gap-3">
                    <span className="text-muted-foreground">
                      <ProviderIcon provider={row.provider} />
                    </span>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium">
                          {providerLabel(row.provider)}
                        </span>
                        <StatusBadge status={row.status} />
                      </div>
                      <div className="truncate text-xs text-muted-foreground">
                        {row.rootPath
                          ? `Root: ${row.rootPath}`
                          : "Root: default"}
                        {row.expiresAt ? ` · Expires ${row.expiresAt}` : ""}
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-1">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button
                          size="sm"
                          variant="ghost"
                          aria-label={`Browse ${providerLabel(row.provider)}`}
                          onClick={() => handleBrowse(row)}
                        >
                          <FolderOpen className="h-3 w-3" aria-hidden="true" />
                          Browse
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>Browse this connection</TooltipContent>
                    </Tooltip>
                    {row.connectionID && (
                      <>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              size="sm"
                              variant="ghost"
                              aria-label={`Refresh ${providerLabel(row.provider)}`}
                              onClick={() => handleRefresh(row)}
                            >
                              <RefreshCw
                                className="h-3 w-3"
                                aria-hidden="true"
                              />
                              Refresh
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            Fetch a fresh access token
                          </TooltipContent>
                        </Tooltip>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              size="sm"
                              variant="ghost"
                              aria-label={`Disconnect ${providerLabel(row.provider)}`}
                              onClick={() => setDisconnectTarget(row)}
                            >
                              <Unplug className="h-3 w-3" aria-hidden="true" />
                              Disconnect
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            Revoke this connection everywhere
                          </TooltipContent>
                        </Tooltip>
                      </>
                    )}
                    {row.localID && (
                      <Button
                        size="sm"
                        variant="ghost"
                        aria-label={`Remove ${providerLabel(row.provider)}`}
                        onClick={() =>
                          setRemoveTarget({
                            id: row.localID!,
                            provider: row.provider,
                            root_path: row.rootPath,
                          })
                        }
                      >
                        <Trash2 className="h-3 w-3" aria-hidden="true" />
                        Remove
                      </Button>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>

        {browseRow && (
          <section className="space-y-2 rounded-md border border-border p-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-sm font-medium">
                <FolderOpen
                  className="h-4 w-4 text-muted-foreground"
                  aria-hidden="true"
                />
                Browsing {providerLabel(browseRow.provider)}
                {browsePath ? ` / ${browsePath}` : ""}
              </div>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setBrowseRow(null);
                  setEntries([]);
                }}
              >
                Close
              </Button>
            </div>
            <ul className="space-y-1">
              {entries.map((entry) => (
                <li
                  key={entry.id}
                  className="flex items-center justify-between text-sm"
                >
                  <span className="flex items-center gap-2">
                    {entry.is_dir ? (
                      <FolderOpen
                        className="h-3 w-3 text-muted-foreground"
                        aria-hidden="true"
                      />
                    ) : (
                      <Cloud
                        className="h-3 w-3 text-muted-foreground"
                        aria-hidden="true"
                      />
                    )}
                    {entry.name}
                  </span>
                  {entry.is_dir ? (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => handleBrowse(browseRow, entry.path)}
                    >
                      Open
                    </Button>
                  ) : (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() =>
                        window.open(
                          `/api/cloud/read?id=${encodeURIComponent(browseRow.connectionID ?? browseRow.localID ?? "")}&path=${encodeURIComponent(entry.path)}`,
                          "_blank",
                        )
                      }
                    >
                      <ExternalLink className="h-3 w-3" aria-hidden="true" />
                      Download
                    </Button>
                  )}
                </li>
              ))}
              {entries.length === 0 && (
                <li className="text-sm text-muted-foreground">
                  This folder is empty.
                </li>
              )}
            </ul>
          </section>
        )}

        <section className="space-y-3">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="px-0"
            aria-expanded={showLegacy}
            onClick={() => setShowLegacy((value) => !value)}
          >
            {showLegacy ? (
              <ChevronDown className="h-3 w-3" aria-hidden="true" />
            ) : (
              <ChevronRight className="h-3 w-3" aria-hidden="true" />
            )}
            Advanced — local-only connection
          </Button>
          {showLegacy && (
            <div className="space-y-2 rounded-md border border-border p-3">
              <p className="text-xs text-muted-foreground">
                Legacy per-project OAuth with credentials stored locally. Only
                use this when you cannot log in to kakiit.
              </p>
              <LabeledInput
                label="Connector id"
                value={newId}
                onChange={setNewId}
                placeholder="e.g. gdrive-main"
              />
              <div className="space-y-1">
                <label
                  className="text-xs font-medium"
                  htmlFor="cloud-legacy-provider"
                >
                  Provider
                </label>
                <Select value={provider} onValueChange={setProvider}>
                  <SelectTrigger
                    id="cloud-legacy-provider"
                    className="h-9 text-sm"
                  >
                    <SelectValue placeholder="Select provider" />
                  </SelectTrigger>
                  <SelectContent>
                    {CONNECT_PROVIDERS.map((option) => (
                      <SelectItem
                        key={option}
                        value={option}
                        className="text-sm"
                      >
                        {providerLabel(option)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <LabeledInput
                label="Root path (optional)"
                value={rootPath}
                onChange={setRootPath}
                placeholder="root folder id"
              />
              <LabeledInput
                label="Client id"
                value={clientId}
                onChange={setClientId}
                placeholder="client_id"
              />
              <LabeledInput
                label="Client secret"
                value={clientSecret}
                onChange={setClientSecret}
                placeholder="client_secret"
              />
              <LabeledInput
                label="Refresh token"
                value={refreshToken}
                onChange={setRefreshToken}
                placeholder="refresh_token"
              />
              <div className="flex gap-2">
                <Button size="sm" onClick={handleAdd}>
                  Add
                </Button>
                <Button size="sm" variant="outline" onClick={handleLocalOAuth}>
                  Connect via OAuth
                </Button>
              </div>
            </div>
          )}
        </section>

        <ConfirmDialog
          open={disconnectTarget != null}
          title="Disconnect cloud provider"
          description={
            disconnectTarget
              ? `Disconnecting ${providerLabel(disconnectTarget.provider)} revokes this connection globally — all your projects and machines will lose access. This cannot be undone.`
              : undefined
          }
          confirmLabel="Disconnect"
          pendingLabel="Disconnecting…"
          onConfirm={confirmDisconnect}
          onCancel={() => setDisconnectTarget(null)}
        />
        <ConfirmDialog
          open={removeTarget != null}
          title="Remove local connector"
          description={
            removeTarget
              ? `Remove the local connector ${removeTarget.id}? Stored credentials for this project will be deleted.`
              : undefined
          }
          confirmLabel="Remove"
          pendingLabel="Removing…"
          onConfirm={confirmRemoveLocal}
          onCancel={() => setRemoveTarget(null)}
        />
      </div>
    </TooltipProvider>
  );
}

function LabeledInput({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  const id = `cloud-legacy-${label.toLowerCase().replace(/[^a-z]+/g, "-")}`;
  return (
    <div className="space-y-1">
      <label className="text-xs font-medium" htmlFor={id}>
        {label}
      </label>
      <Input
        id={id}
        className="h-9 text-sm"
        placeholder={placeholder}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
}
