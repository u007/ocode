import { useEffect, useRef, useState } from "react";
import { CloudCog } from "lucide-react";
import { api } from "../../api/client";
import type { SyncStatusResponse } from "../../api/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";

const POLL_INTERVAL_MS = 2000;

function formatSyncedAt(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  const mins = Math.round(ms / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.round(hours / 24)}d ago`;
}

export default function SyncStatusWidget() {
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState<SyncStatusResponse | null>(null);
  const [loginState, setLoginState] = useState<
    | { phase: "idle" }
    | { phase: "starting" }
    | { phase: "waiting"; userCode: string; verifyUrl: string; deviceCode: string }
    | { phase: "error"; message: string }
  >({ phase: "idle" });
  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null);

  const refreshStatus = () => {
    api.getSyncStatus().then(setStatus).catch(() => {});
  };

  useEffect(() => {
    refreshStatus();
  }, []);

  useEffect(() => {
    return () => {
      if (pollTimer.current) clearInterval(pollTimer.current);
    };
  }, []);

  const stopPolling = () => {
    if (pollTimer.current) {
      clearInterval(pollTimer.current);
      pollTimer.current = null;
    }
  };

  const startLogin = async () => {
    setLoginState({ phase: "starting" });
    try {
      const result = await api.syncLoginStart();
      setLoginState({
        phase: "waiting",
        userCode: result.userCode,
        verifyUrl: result.verifyUrl,
        deviceCode: result.deviceCode,
      });
      window.open(result.verifyUrl, "_blank", "noopener,noreferrer");

      pollTimer.current = setInterval(async () => {
        try {
          const poll = await api.syncLoginPoll(result.deviceCode);
          if (poll.status === "approved") {
            stopPolling();
            setLoginState({ phase: "idle" });
            refreshStatus();
          } else if (poll.status === "expired") {
            stopPolling();
            setLoginState({ phase: "error", message: "Code expired — try again." });
          }
        } catch {
          // transient network error while polling; keep trying until expiry
        }
      }, POLL_INTERVAL_MS);
    } catch (err) {
      setLoginState({
        phase: "error",
        message: err instanceof Error ? err.message : "Failed to start login",
      });
    }
  };

  const logout = async () => {
    stopPolling();
    try {
      await api.syncLogout();
    } catch {
      // best-effort; local token is cleared server-side regardless
    }
    setLoginState({ phase: "idle" });
    refreshStatus();
  };

  const loggedIn = status?.loggedIn ?? false;

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        className="relative flex items-center justify-center w-8 h-8 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors shrink-0"
        title={loggedIn ? "Config sync active" : "Sync not connected"}
      >
        <CloudCog className="w-4 h-4" />
        <span
          className={`absolute top-1 right-1 w-1.5 h-1.5 rounded-full ${
            loggedIn ? "bg-emerald-500" : "bg-accent"
          }`}
        />
      </button>

      <Dialog
        open={open}
        onOpenChange={(isOpen) => {
          setOpen(isOpen);
          if (!isOpen) {
            stopPolling();
            setLoginState({ phase: "idle" });
          }
        }}
      >
        <DialogContent className="sm:max-w-md bg-card border-border">
          <DialogHeader>
            <DialogTitle className="text-foreground">Account Sync</DialogTitle>
            <DialogDescription>
              Encrypted config sync across your machines.
            </DialogDescription>
          </DialogHeader>

          {!loggedIn && loginState.phase === "idle" && (
            <div className="space-y-3">
              <p className="text-sm text-muted-foreground">
                Log in to sync your settings and provider credentials to other
                machines.
              </p>
              <Button onClick={startLogin} className="w-full">
                Log In
              </Button>
            </div>
          )}

          {loginState.phase === "starting" && (
            <p className="text-sm text-muted-foreground">Starting login…</p>
          )}

          {loginState.phase === "waiting" && (
            <div className="space-y-3 text-center">
              <p className="text-sm text-muted-foreground">
                A browser tab opened — enter this code to authorize:
              </p>
              <div className="font-mono text-lg tracking-widest bg-muted rounded-md py-3">
                {loginState.userCode}
              </div>
              <a
                href={loginState.verifyUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs text-blue-400 hover:underline break-all"
              >
                {loginState.verifyUrl}
              </a>
              <p className="text-xs text-muted-foreground">Waiting for approval…</p>
            </div>
          )}

          {loginState.phase === "error" && (
            <div className="space-y-3">
              <p className="text-sm text-red-400">{loginState.message}</p>
              <Button onClick={startLogin} className="w-full">
                Try Again
              </Button>
            </div>
          )}

          {loggedIn && loginState.phase === "idle" && status && (
            <div className="space-y-4">
              <div className="space-y-2 text-sm">
                <div className="flex justify-between text-muted-foreground">
                  <span>Settings</span>
                  <span>
                    {status.config.synced
                      ? `v${status.config.version} · ${formatSyncedAt(status.config.syncedAt)}`
                      : "not synced yet"}
                  </span>
                </div>
                <div className="flex justify-between text-muted-foreground">
                  <span>Credentials</span>
                  <span>
                    {status.auth.synced
                      ? `v${status.auth.version} · ${formatSyncedAt(status.auth.syncedAt)}`
                      : "not synced yet"}
                  </span>
                </div>
              </div>
              <Button onClick={logout} variant="outline" className="w-full">
                Log Out
              </Button>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
