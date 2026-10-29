import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Network, Plus, Trash2 } from "lucide-react";
import { api, isPortMapsAvailable, ApiError } from "../../api/client";
import type { HtrForwardStatus, PortMapTarget, PortMapView } from "../../api/types";
import { useProjectState } from "../../stores/projectStore";
import { remoteForwardTarget } from "../../lib/trustedProject";
import {
  LIVE_POLL_INTERVAL_MS,
  shouldPollForLive,
} from "../../lib/portMapsLivePoll";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

/** Top-nav "Ports" button + dialog for user-added SSH port forwards.
 *
 *  Two backings, both keyed to the active project:
 *  - A remote SSH project is active → the project-scoped family served by
 *    internal/server (`/api/portmaps?host=&project=`), one `ssh -N -L` child per
 *    forward under the server's process supervisor. This is what makes the
 *    panel follow the project (kakiit, aimsai2, …) instead of the whole app.
 *  - No remote project → the desktop remote-workspace's single-tunnel family
 *    (`/api/desktop/portmaps`, internal/desktop/portmaps.go), reached only in
 *    desktop remote-workspace mode.
 *
 *  Renders nothing when neither route exists (a plain local session, or a
 *  remote-server SPA with no local process to back the desktop family — see
 *  isPortMapsAvailable), and nothing for a WSL project (WSL2 shares the Windows
 *  loopback, so the server refuses forwards for it). */
export default function PortMapsWidget() {
  const { state: projectState } = useProjectState();
  const activePath = projectState.activeProject?.path ?? "";
  const target = useMemo(
    () => remoteForwardTarget(projectState.projects, activePath),
    [projectState.projects, activePath],
  );
  // Stable dep key so switching project re-probes without object identity churn.
  const targetKey = target ? `${target.host}\u0000${target.path}` : "";

  const [available, setAvailable] = useState(false);
  const [open, setOpen] = useState(false);
  const [maps, setMaps] = useState<PortMapView[]>([]);
  const [error, setError] = useState("");
  const [remotePort, setRemotePort] = useState("");
  const [localPort, setLocalPort] = useState("");
  // Direction of the NEXT forward to add. false = -L (this machine reaches the
  // remote's port); true = -R (the remote reaches this machine's port).
  // The server owns the truth once a row exists (the row carries its own
  // direction), so this only describes the row being composed.
  const [reverse, setReverse] = useState(false);
  const [busy, setBusy] = useState(false);
  const [htr, setHtr] = useState<HtrForwardStatus | null>(null);
  // The squatting process the user is being asked to kill: the htrcli relay's
  // remote holder (scope "htr") or a -L row's local holder (scope "local").
  const [kill, setKill] = useState<{
    scope: "htr" | "local";
    pid: number;
    command: string;
    remotePort?: number;
  } | null>(null);
  // Bounded re-fetch budget for the background auto-start; reset whenever the
  // dialog opens or the project changes, so each open gets a fresh budget.
  const livePollAttempts = useRef(0);

  // The live target for handlers that fire after a project switch (the dialog is
  // closed on switch, but an in-flight request can still resolve).
  const targetRef = useRef<PortMapTarget | undefined>(undefined);
  targetRef.current = target ?? undefined;

  // Re-probe (and drop stale rows) whenever the active project changes: a
  // forward belongs to exactly one remote project, so a leftover list or an open
  // dialog from the previous project must never be acted on.
  useEffect(() => {
    let cancelled = false;
    setAvailable(false);
    setMaps([]);
    setHtr(null);
    setError("");
    setOpen(false);
    livePollAttempts.current = 0;
    isPortMapsAvailable(targetRef.current).then((ok) => {
      if (!cancelled) setAvailable(ok);
    });
    return () => {
      cancelled = true;
    };
  }, [targetKey]);

  const refresh = useCallback(() => {
    api
      .listPortMaps(targetRef.current)
      .then(setMaps)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  }, []);

  const refreshHtr = useCallback(() => {
    const t = targetRef.current;
    if (!t) return;
    api
      .getHtrForward(t)
      .then(setHtr)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  }, []);

  useEffect(() => {
    if (open) {
      refresh();
      refreshHtr();
    }
  }, [open, targetKey, refresh, refreshHtr]);

  // The htrcli forward opens in the background on list, so poll its status
  // while it is expected to come up (auto-start on, daemon running, not live).
  useEffect(() => {
    if (!open || !htr) return;
    if (!(htr.auto_start && htr.local_running && !htr.live)) return;
    if (livePollAttempts.current >= 5) return;
    const timer = setTimeout(() => {
      livePollAttempts.current += 1;
      refreshHtr();
    }, LIVE_POLL_INTERVAL_MS);
    return () => clearTimeout(timer);
  }, [open, htr, refreshHtr]);

  const handleHtrStart = async () => {
    const t = targetRef.current;
    if (!t) return;
    setBusy(true);
    setError("");
    try {
      setHtr(await api.startHtrForward(t));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
      refreshHtr();
    } finally {
      setBusy(false);
    }
  };

  const handleReclaim = async () => {
    const t = targetRef.current;
    if (!t || !kill) return;
    setBusy(true);
    setError("");
    try {
      if (kill.scope === "htr") {
        setHtr(await api.reclaimHtrForward(kill.pid, t));
      } else {
        setMaps(await api.reclaimPortMap(kill.remotePort!, kill.pid, t));
      }
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
      if (kill.scope === "htr") refreshHtr();
      else refresh();
    } finally {
      setKill(null);
      setBusy(false);
    }
  };

  const handleHtrAutoStart = async (enabled: boolean) => {
    const t = targetRef.current;
    if (!t) return;
    setBusy(true);
    setError("");
    try {
      setHtr(await api.setHtrForwardAutoStart(enabled, t));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  // The server opens this project's forwards in the BACKGROUND, so the first
  // list reports live=false for anything still coming up. Re-fetch while an
  // enabled forward is pending; see shouldPollForLive for the two rules that
  // keep this bounded.
  useEffect(() => {
    if (!open) return;
    if (!shouldPollForLive(maps, livePollAttempts.current)) return;
    const timer = setTimeout(() => {
      livePollAttempts.current += 1;
      refresh();
    }, LIVE_POLL_INTERVAL_MS);
    return () => clearTimeout(timer);
  }, [open, maps, refresh]);

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
      const updated = await api.addPortMap(rp, lp, targetRef.current, reverse);
      setMaps(updated);
      setRemotePort("");
      setLocalPort("");
      // Reset the direction too: a second forward is almost always the same
      // shape as the first, but leaving a reverse box ticked would silently
      // invert the next add, which is the one mistake here that can expose a
      // local port to the remote host.
      setReverse(false);
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
      setMaps(await api.removePortMap(port, targetRef.current));
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
      setMaps(await api.setPortMapEnabled(port, enabled, targetRef.current));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <button
        onClick={() => {
          livePollAttempts.current = 0;
          setOpen(true);
        }}
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
              {target?.host
                ? `Extra SSH forwards from this machine to ${target.host}, on top of the built-in tunnels.`
                : "Extra SSH forwards to this remote workspace, on top of the API/browse tunnel."}
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleAdd} className="space-y-2">
            <div className="flex items-end gap-2">
              <div className="flex-1 space-y-1">
                <label className="text-xs text-muted-foreground">Remote port</label>
                <Input
                  type="number"
                  min={1}
                  max={65535}
                  value={remotePort}
                  onChange={(e) => setRemotePort(e.target.value)}
                  placeholder="3000"
                  autoFocus
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
            </div>

            {/* Direction. The port NUMBERS keep their meaning in both
                directions — "remote port" is always the port on the remote
                host, "local port" always the port on this machine — so only
                the arrow between them flips. */}
            <label className="flex items-start gap-2 text-xs text-muted-foreground">
              <input
                type="checkbox"
                className="mt-0.5"
                checked={reverse}
                onChange={(e) => setReverse(e.target.checked)}
                data-testid="portmap-reverse-toggle"
              />
              <span>
                Reverse
                <span className="block mt-0.5 text-[11px] text-muted-foreground/80">
                  {reverse
                    ? "The remote host reaches this machine's local port (ssh -R). Leave off to reach the remote's port instead (ssh -L)."
                    : "This machine reaches the remote host's port (ssh -L). Tick to expose a local port to the remote instead (ssh -R)."}
                </span>
              </span>
            </label>

            {reverse && (
              <p
                role="alert"
                className="rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-[11px] text-amber-200"
                data-testid="portmap-reverse-warning"
              >
                Anything that can reach the remote host's loopback can then reach
                this machine's local port. A debugging port such as 9222 (Chrome
                CDP) hands over full control of the browser — remove the forward
                when you are done.
              </p>
            )}
          </form>

          {error && <p className="text-sm text-destructive">{error}</p>}

          {htr && (
            <div
              className="rounded-md border border-border p-2 space-y-1.5"
              data-testid="htr-forward"
            >
              <div className="flex items-center gap-2">
                <span className="flex-1 text-sm">
                  htrcli browser relay
                  <span className="block text-[11px] text-muted-foreground">
                    {htr.local_running
                      ? `remote:${htr.port} → localhost:${htr.port}`
                      : "Local htrcli daemon is not running."}
                  </span>
                </span>
                <span
                  className={`text-xs ${htr.live ? "text-emerald-500" : "text-muted-foreground"}`}
                >
                  {htr.live ? "live" : htr.opening ? "opening…" : "not mapped"}
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy || htr.live || !htr.local_running}
                  onClick={handleHtrStart}
                >
                  Start
                </Button>
              </div>
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input
                  type="checkbox"
                  checked={htr.auto_start}
                  disabled={busy}
                  onChange={(e) => handleHtrAutoStart(e.target.checked)}
                  data-testid="htr-autostart-toggle"
                />
                Auto-start when this panel opens or the mapping drops
              </label>
              {htr.last_error && !htr.live && (
                <p className="text-[11px] text-destructive break-words">{htr.last_error}</p>
              )}
              {htr.holder && !htr.live && (
                <div className="flex items-center gap-2" data-testid="htr-holder">
                  <p className="flex-1 text-[11px] text-muted-foreground break-all">
                    Remote port {htr.port} is held by pid {htr.holder.pid}
                    {htr.holder.command ? `: ${htr.holder.command}` : ""}
                  </p>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={busy}
                    onClick={() =>
                      setKill({ scope: "htr", pid: htr.holder!.pid, command: htr.holder!.command })
                    }
                    data-testid="htr-kill-holder"
                  >
                    Kill and retry
                  </Button>
                </div>
              )}
            </div>
          )}

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
                  {m.reverse
                    ? `remote:${m.remote_port} → localhost:${m.local_port} (reverse)`
                    : `localhost:${m.local_port} → remote:${m.remote_port}`}
                </span>
                <span
                  className={`text-xs ${m.live ? "text-emerald-500" : "text-muted-foreground"}`}
                >
                  {m.live ? "live" : m.enabled ? "enabled" : "disabled"}
                </span>
                {m.holder && !m.live && (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={busy}
                    title={`localhost:${m.local_port} is held by pid ${m.holder.pid}: ${m.holder.command}`}
                    onClick={() =>
                      setKill({
                        scope: "local",
                        pid: m.holder!.pid,
                        command: m.holder!.command,
                        remotePort: m.remote_port,
                      })
                    }
                    data-testid={`portmap-kill-holder-${m.remote_port}`}
                  >
                    Kill holder
                  </Button>
                )}
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
      <Dialog open={kill !== null} onOpenChange={(o) => !o && setKill(null)}>
        <DialogContent data-testid="htr-kill-confirm">
          <DialogHeader>
            <DialogTitle>
              {kill?.scope === "local" ? "Kill process on this machine?" : "Kill process on remote?"}
            </DialogTitle>
            <DialogDescription>
              {kill?.scope === "local"
                ? `This sends SIGTERM to pid ${kill.pid} on this machine, then reopens the forward.`
                : `This sends SIGTERM to pid ${kill?.pid} on the remote host, then reopens the htrcli relay on port ${htr?.port}.`}
            </DialogDescription>
          </DialogHeader>
          {kill?.command && (
            <p className="font-mono text-xs break-all rounded-md border border-border p-2">
              {kill.command}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="outline" disabled={busy} onClick={() => setKill(null)}>
              Cancel
            </Button>
            <Button variant="destructive" disabled={busy} onClick={handleReclaim}>
              Kill and retry
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
