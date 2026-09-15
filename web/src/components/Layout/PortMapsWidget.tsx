import { useEffect, useState } from "react";
import { Network, Plus, Trash2 } from "lucide-react";
import { api, isPortMapsAvailable, ApiError } from "../../api/client";
import type { PortMapView } from "../../api/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

/** Top-nav "Ports" button + dialog for user-added SSH port forwards on a
 *  connected remote workspace (desktop app only — see internal/desktop/
 *  portmaps.go). Renders nothing when not connected to a remote workspace,
 *  or when viewing a plain remote-server SPA directly (`ocode remote --web`
 *  has no local process to back these routes; see isPortMapsAvailable). */
export default function PortMapsWidget() {
  const [available, setAvailable] = useState(false);
  const [open, setOpen] = useState(false);
  const [maps, setMaps] = useState<PortMapView[]>([]);
  const [error, setError] = useState("");
  const [remotePort, setRemotePort] = useState("");
  const [localPort, setLocalPort] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    isPortMapsAvailable().then(setAvailable);
  }, []);

  const refresh = () => {
    api
      .listPortMaps()
      .then(setMaps)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  };

  useEffect(() => {
    if (open) refresh();
  }, [open]);

  if (!available) return null;

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault();
    const rp = parseInt(remotePort, 10);
    const lp = localPort.trim() === "" ? rp : parseInt(localPort, 10);
    if (!Number.isInteger(rp) || rp <= 0 || rp > 65535 || !Number.isInteger(lp) || lp <= 0 || lp > 65535) {
      setError("Enter a port between 1 and 65535.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const updated = await api.addPortMap(rp, lp);
      setMaps(updated);
      setRemotePort("");
      setLocalPort("");
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const handleRemove = async (port: number) => {
    setBusy(true);
    setError("");
    try {
      setMaps(await api.removePortMap(port));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const handleToggle = async (port: number, enabled: boolean) => {
    setBusy(true);
    setError("");
    try {
      setMaps(await api.setPortMapEnabled(port, enabled));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        className="flex items-center justify-center w-8 h-8 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors shrink-0"
        title="Port forwards"
      >
        <Network className="w-4 h-4" />
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md bg-card border-border">
          <DialogHeader>
            <DialogTitle className="text-foreground">Port Forwards</DialogTitle>
            <DialogDescription>
              Extra SSH forwards to this remote workspace, on top of the API/browse tunnel.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleAdd} className="flex items-end gap-2">
            <div className="flex-1 space-y-1">
              <label className="text-xs text-muted-foreground">Remote port</label>
              <Input
                type="number"
                min={1}
                max={65535}
                value={remotePort}
                onChange={(e) => setRemotePort(e.target.value)}
                placeholder="3000"
              />
            </div>
            <div className="flex-1 space-y-1">
              <label className="text-xs text-muted-foreground">Local port (optional)</label>
              <Input
                type="number"
                min={1}
                max={65535}
                value={localPort}
                onChange={(e) => setLocalPort(e.target.value)}
                placeholder={remotePort || "same"}
              />
            </div>
            <Button type="submit" disabled={busy || !remotePort} size="icon" title="Add">
              <Plus className="w-4 h-4" />
            </Button>
          </form>

          {error && <p className="text-sm text-destructive">{error}</p>}

          <div className="space-y-1 max-h-64 overflow-y-auto">
            {maps.length === 0 && (
              <p className="text-sm text-muted-foreground">No extra port forwards yet.</p>
            )}
            {maps.map((m) => (
              <div
                key={m.remote_port}
                className="flex items-center gap-2 rounded-md border border-border px-2 py-1.5"
              >
                <span className="flex-1 text-sm font-mono">
                  localhost:{m.local_port} → remote:{m.remote_port}
                </span>
                <span
                  className={`text-xs ${m.live ? "text-emerald-500" : "text-muted-foreground"}`}
                >
                  {m.live ? "live" : m.enabled ? "enabled" : "disabled"}
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  onClick={() => handleToggle(m.remote_port, !m.enabled)}
                >
                  {m.enabled ? "Disable" : "Enable"}
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  disabled={busy}
                  onClick={() => handleRemove(m.remote_port)}
                  title="Remove"
                >
                  <Trash2 className="w-4 h-4" />
                </Button>
              </div>
            ))}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
