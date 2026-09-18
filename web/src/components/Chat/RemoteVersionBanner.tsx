import { useEffect, useState } from "react";
import { AlertTriangle, ChevronDown, ChevronUp } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useRemoteHostStatus } from "@/hooks/useRemoteHostStatus";

/**
 * Hosts whose banner the user expanded. Module-level so the choice survives the
 * banner unmounting on a project/tab switch for the rest of the page session.
 * Default is collapsed: the one-line warning + Update stay visible, and
 * expanding only reveals the "what will happen" explanation.
 */
const expandedHosts = new Set<string>();

/** Test-only: clear the remembered expanded state between cases. */
export function __resetRemoteVersionBannerForTests() {
  expandedHosts.clear();
}

/**
 * RemoteVersionBanner — a slim, collapsible warning above the chat transcript
 * when the active chat tab's remote (SSH/WSL) server runs an older version than
 * this app.
 *
 * The fix is a server restart at this app's version, so the banner performs the
 * same `restart()` the sidebar exposes — but behind a confirm, because a
 * restart interrupts running turns and terminals. Status is shared with the
 * sidebar via useRemoteHostStatus, so an update from either surface clears both
 * immediately.
 */
export function RemoteVersionBanner({ host }: { host?: string }) {
  const { status, busy, error, restart } = useRemoteHostStatus(host, !!host);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [expanded, setExpanded] = useState(() => (host ? expandedHosts.has(host) : false));

  // Reset expansion when this instance is reused for a different host.
  useEffect(() => {
    setExpanded(host ? expandedHosts.has(host) : false);
  }, [host]);

  const connected = status?.connected ?? false;
  const outdated = status?.outdated ?? false;
  if (!host || !connected || !outdated) return null;

  const from = status?.version || "unknown";
  const to = status?.local_version ?? "";
  const updating = busy === "restarting";

  const toggleExpanded = () => {
    setExpanded((prev) => {
      const next = !prev;
      if (next) expandedHosts.add(host);
      else expandedHosts.delete(host);
      return next;
    });
  };

  return (
    <>
      <div
        role="status"
        data-testid="remote-version-banner"
        className="flex flex-wrap items-center gap-x-2 gap-y-1 border-b border-amber-500/40 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-700 dark:text-amber-300"
      >
        <AlertTriangle className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
        <span className={expanded ? "min-w-0 flex-1" : "min-w-0 flex-1 truncate"}>
          Remote server <span className="font-medium">v{from}</span> is older than this app{" "}
          <span className="font-medium">v{to}</span>.
          {expanded && " Updating restarts the remote server and interrupts running turns and terminals."}
        </span>
        {error && (
          <span className="min-w-0 max-w-[40%] truncate text-destructive" title={error}>
            {error}
          </span>
        )}
        <Button
          variant="outline"
          size="sm"
          className="h-6 shrink-0 px-2 text-[11px]"
          disabled={updating}
          onClick={() => setConfirmOpen(true)}
        >
          {updating ? "Updating…" : "Update"}
        </Button>
        <button
          type="button"
          aria-label={expanded ? "Collapse version warning" : "Expand version warning"}
          aria-expanded={expanded}
          onClick={toggleExpanded}
          className="shrink-0 rounded p-0.5 text-amber-700 hover:bg-amber-500/20 dark:text-amber-300"
        >
          {expanded ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
        </button>
      </div>

      <Dialog open={confirmOpen} onOpenChange={(open) => !open && setConfirmOpen(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Update remote server?</DialogTitle>
            <DialogDescription>
              This restarts the server on <span className="font-medium">{host}</span>, replacing v{from} with this
              app's v{to}. Running turns and terminal sessions on that host will be interrupted.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            {/* Destructive confirm: default focus belongs on Cancel, not Update. */}
            <Button variant="outline" onClick={() => setConfirmOpen(false)} data-dialog-default-action>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                setConfirmOpen(false);
                restart();
              }}
            >
              Update server
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

export default RemoteVersionBanner;
