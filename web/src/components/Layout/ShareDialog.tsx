import { useEffect, useState, useCallback, useRef } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "../ui/dialog";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Copy, Check, Monitor, RotateCcw, Power, Square, Loader2 } from "lucide-react";
import { authToken, isRemoteSession, authedFetch, apiPath, api } from "../../api/client";
import type { ShareStatus } from "../../api/types";
import { copyTextToClipboard } from "../../lib/clipboard";
import { isDesktopShell } from "../../lib/desktopShell";

// Routes owned by internal/desktop.ShareTokenStore. They exist only in the
// desktop shell; a plain `ocode serve` answers 404, which is how the dialog
// falls back to the launch token.
const SHARE_TOKEN_PATH = "/api/desktop/share-token";
const SHARE_TOKEN_RESET_PATH = "/api/desktop/share-token/reset";

function isUsableIP(ip: unknown): ip is string {
  return (
    typeof ip === "string" &&
    ip !== "" &&
    ip !== "localhost" &&
    ip !== "127.0.0.1" &&
    ip !== "::1"
  );
}

function isLoopbackHost(h: string): boolean {
  return h === "localhost" || h === "127.0.0.1" || h === "::1" || h === "[::1]";
}

function joinWithToken(base: string, tokenSuffix: string): string {
  return `${base}/${tokenSuffix}`.replace(/\/\//g, "/").replace(":/", "://");
}

// The credential travels in the query string (the only form EventSource and
// the WS handshake can carry), so the token is what makes a share URL a
// credential rather than a dead link.
function tokenQuery(token: string): string {
  return token ? `?token=${encodeURIComponent(token)}` : "";
}

function buildPrimaryUrl(
  tailscaleUrl: string | null,
  networkIP: string | null,
  tokenSuffix: string,
): string {
  if (tailscaleUrl) {
    return joinWithToken(tailscaleUrl.replace(/\/+$/, ""), tokenSuffix);
  }
  const base = window.location.pathname.match(/^(.*?)\/session\/[^/]+$/)?.[1] ?? "";
  if (networkIP) {
    const port = window.location.port ? `:${window.location.port}` : "";
    const origin = `${window.location.protocol}//${networkIP}${port}`;
    return joinWithToken(`${origin}${base}`, tokenSuffix);
  }
  const host = window.location.hostname;
  if (!isLoopbackHost(host)) {
    return joinWithToken(`${window.location.protocol}//${window.location.host}${base}`, tokenSuffix);
  }
  return "";
}

function buildLanUrl(
  tailscaleUrl: string | null,
  networkIP: string | null,
  tokenSuffix: string,
): string {
  if (!tailscaleUrl || !networkIP) return "";
  const base = window.location.pathname.match(/^(.*?)\/session\/[^/]+$/)?.[1] ?? "";
  const port = window.location.port ? `:${window.location.port}` : "";
  const origin = `${window.location.protocol}//${networkIP}${port}`;
  return joinWithToken(`${origin}${base}`, tokenSuffix);
}

export default function ShareDialog() {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const [copyFailed, setCopyFailed] = useState(false);

  // There is exactly one share URL: full desktop access. The server's auth
  // token authorizes every API route (not just one session), so a
  // "session-only" link would silently grant control over all sessions,
  // projects, files, terminals, and configuration. Until scoped capability
  // tokens exist, do not present session-only sharing.
  const [networkIP, setNetworkIP] = useState<string | null>(null);
  // Live exposure status (running? public funnel or tailnet-only serve?).
  // Reading it is side-effect free; STARTING is an explicit action.
  const [status, setStatus] = useState<ShareStatus | null>(null);
  // Whether auto-share-on-start is enabled, so a stopped share can honestly
  // warn that it comes back on the next launch.
  const [autoShareEnabled, setAutoShareEnabled] = useState(false);
  const [shareLoaded, setShareLoaded] = useState(false);
  const [starting, setStarting] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [stopArmed, setStopArmed] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  // The durable desktop share token. Null means "not resolved yet" (or not the
  // desktop shell); "" means "resolved, but there is none" — which is the
  // server-mode case where the launch token is the only credential. The two
  // are deliberately distinct: only a non-empty token may offer a reset.
  const [shareToken, setShareToken] = useState<string | null>(null);
  const [resetArmed, setResetArmed] = useState(false);
  const [resetting, setResetting] = useState(false);
  const [resetError, setResetError] = useState<string | null>(null);
  const primaryInputRef = useRef<HTMLInputElement | null>(null);
  // Refs mirror state so the headless "Copy Desktop URL" menu handler (which
  // may fire before the dialog ever opens) always sees fresh values without
  // re-subscribing listeners.
  const networkIPRef = useRef<string | null>(null);
  const shareTokenRef = useRef<string | null>(null);
  const statusRef = useRef<ShareStatus | null>(null);

  // loadShareState reads everything the dialog renders WITHOUT starting an
  // exposure: the live share status / auto-share flag, the LAN address, and the
  // durable desktop token. Opening the dialog must never publish the instance,
  // so this is a pure read.
  const loadShareState = useCallback(async () => {
    try {
      const cfg = await api.getAutoShareConfig();
      setStatus(cfg);
      statusRef.current = cfg;
      setAutoShareEnabled(cfg.enabled);
    } catch (e) {
      setActionError(e instanceof Error ? e.message : String(e));
    }
    // LAN fallback comes from the server's interface ranking — never
    // synthesize localhost here. A "localhost" share URL is unreachable
    // from any other device.
    try {
      const r = await authedFetch(apiPath("/api/network-ip"));
      if (r.ok) {
        const data: any = await r.json().catch(() => null);
        if (isUsableIP(data?.ip)) {
          setNetworkIP(data.ip);
          networkIPRef.current = data.ip;
        }
      }
    } catch {}
    // Durable share token (desktop shell only). Preferred over authToken()
    // because it survives a restart, so a link already in someone's hands keeps
    // working; authToken() is this launch's credential and would make every
    // shared URL expire on the next quit. A plain server has no such route and
    // answers 404, which leaves the launch token in place.
    if (isDesktopShell()) {
      let token = "";
      try {
        const r = await authedFetch(apiPath(SHARE_TOKEN_PATH));
        if (r.ok) {
          const data: any = await r.json().catch(() => null);
          if (data && typeof data.token === "string") token = data.token;
        }
      } catch {
        // Leave token as "" — the launch-token fallback below still works.
      }
      setShareToken(token);
      shareTokenRef.current = token;
    }
    setShareLoaded(true);
  }, []);

  useEffect(() => {
    // Resolve lazily on open only. This is a STATUS read: it never starts a
    // tailscale exposure. Remote sessions never resolve: their token is
    // rejected by design and must not be embedded in a URL.
    if (!open || isRemoteSession()) return;
    // Values are idempotent and the component stays mounted when the dialog
    // closes, so no cancelled guard is needed — keep whatever resolves.
    void loadShareState();
  }, [open, loadShareState]);

  const running = !!status?.running;
  // Only a PROVEN running exposure yields a tailscale URL; the server already
  // omits an unproven DNS-name guess, and we re-check here so a stale status
  // can never render a dead link.
  const tailscaleUrl = running ? (status?.url ?? null) : null;
  const tailscaleHint = status?.hint ?? null;
  // Prefer the durable desktop share token; fall back to this launch's token
  // (server mode, or a desktop whose share-token file could not be created).
  const tokenSuffix = tokenQuery(shareToken || authToken());

  // Primary share URL: tailscale first, LAN fallback. Never localhost — a
  // loopback URL shared to another device is dead on arrival.
  const primaryUrl = buildPrimaryUrl(tailscaleUrl, networkIP, tokenSuffix);

  // Secondary LAN URL shown alongside tailscale when both are available.
  const lanUrl = buildLanUrl(tailscaleUrl, networkIP, tokenSuffix);

  const selectPrimaryInput = useCallback(() => {
    const el = primaryInputRef.current;
    if (el) {
      try {
        el.focus();
        el.select();
      } catch {}
    }
  }, []);

  const handleCopy = useCallback(async () => {
    if (!primaryUrl) return;
    setCopyFailed(false);
    const ok = await copyTextToClipboard(primaryUrl);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } else {
      // Clipboard is unavailable (insecure LAN origin, denied permission,
      // headless webview): leave the URL selected so a manual ⌘/Ctrl+C works,
      // and surface the hint instead of silently doing nothing.
      setCopyFailed(true);
      selectPrimaryInput();
    }
  }, [primaryUrl, selectPrimaryInput]);

  const handleStart = useCallback(async () => {
    setStarting(true);
    setActionError(null);
    try {
      const st = await api.startShare();
      setStatus(st);
      statusRef.current = st;
      setCopied(false);
      setStopArmed(false);
    } catch (e) {
      setActionError(`Couldn't start sharing: ${e instanceof Error ? e.message : String(e)}`);
    } finally {
      setStarting(false);
    }
  }, []);

  const handleStop = useCallback(async () => {
    setStopping(true);
    setActionError(null);
    try {
      const st = await api.stopShare();
      setStatus(st);
      statusRef.current = st;
      setCopied(false);
      setStopArmed(false);
      // Refresh the auto-share flag too: the user may have toggled it in
      // Settings while the dialog was open.
      try {
        const cfg = await api.getAutoShareConfig();
        setAutoShareEnabled(cfg.enabled);
      } catch {}
    } catch (e) {
      setActionError(`Couldn't stop sharing: ${e instanceof Error ? e.message : String(e)}`);
    } finally {
      setStopping(false);
    }
  }, []);

  // Rotate the durable share token. Every link already handed out stops
  // working the moment the server answers; this window's own session is
  // unaffected because it authenticates with the launch token, not this one.
  const handleResetToken = useCallback(async () => {
    setResetting(true);
    setResetError(null);
    try {
      const r = await authedFetch(apiPath(SHARE_TOKEN_RESET_PATH), { method: "POST" });
      const data: any = r.ok ? await r.json().catch(() => null) : null;
      const token = data && typeof data.token === "string" ? data.token : "";
      if (!r.ok || !token) {
        setResetError(
          `Reset failed (HTTP ${r.status}). The previous link still works — try again, or reset from the Share menu.`,
        );
        return;
      }
      setShareToken(token);
      shareTokenRef.current = token;
      setResetArmed(false);
      // The on-screen URL now embeds the new token; a stale "Copied" would
      // invite re-sharing the revoked one.
      setCopied(false);
    } catch (e) {
      setResetError(`Reset failed: ${e instanceof Error ? e.message : String(e)}`);
    } finally {
      setResetting(false);
    }
  }, []);

  useEffect(() => {
    const openDialog = () => {
      setOpen(true);
      setCopied(false);
      setCopyFailed(false);
      setStopArmed(false);
    };
    const onShareSession = () => openDialog();
    const onShareDesktop = () => openDialog();
    // The native Share ▸ "Reset Share Token…" item. It opens the dialog and
    // arms the inline confirmation in one step, so the menu item is never a
    // silent revocation — but it also never requires hunting for the button.
    const onResetShareToken = () => {
      setOpen(true);
      setResetError(null);
      setResetArmed(true);
    };
    const onCopyDesktop = async () => {
      // Same leak this dialog itself guards against: desktopUrl carries the
      // bearer token in a query string, which a remote server rejects
      // outright and which shouldn't land in the clipboard regardless.
      if (isRemoteSession()) return;
      // The menu item can fire before the dialog ever opens, in which case
      // nothing has resolved yet. Resolve on demand instead of copying the
      // stale (empty) closure value.
      await loadShareState();
      let st = statusRef.current;
      if (!st?.running) {
        // "Copy Desktop URL" is an explicit share request, so starting the
        // exposure here is the intended behaviour (unlike merely opening the
        // dialog).
        try {
          st = await api.startShare();
          setStatus(st);
          statusRef.current = st;
        } catch {
          setOpen(true);
          return;
        }
      }
      const url = buildPrimaryUrl(
        st?.url ?? null,
        networkIPRef.current,
        tokenQuery(shareTokenRef.current || authToken()),
      );
      if (!url) {
        // Nothing shareable — open the dialog so the user sees why instead
        // of the menu item silently doing nothing.
        setOpen(true);
        return;
      }
      const ok = await copyTextToClipboard(url);
      if (!ok) {
        // Clipboard blocked: open the dialog with the URL selected for a
        // manual copy rather than dropping the request.
        setOpen(true);
        setCopyFailed(true);
        // Selection happens after the dialog paints.
        setTimeout(selectPrimaryInput, 50);
      }
    };
    window.addEventListener("ocode:share-session", onShareSession);
    window.addEventListener("ocode:share-desktop", onShareDesktop);
    window.addEventListener("ocode:copy-desktop-url", onCopyDesktop);
    window.addEventListener("ocode:reset-share-token", onResetShareToken);
    return () => {
      window.removeEventListener("ocode:share-session", onShareSession);
      window.removeEventListener("ocode:share-desktop", onShareDesktop);
      window.removeEventListener("ocode:copy-desktop-url", onCopyDesktop);
      window.removeEventListener("ocode:reset-share-token", onResetShareToken);
    };
  }, [loadShareState, selectPrimaryInput]);

  const kindLabel = status?.kind === "funnel" ? "Public on the internet" : "Tailnet only";

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Monitor className="w-4 h-4" />
            Share Entire Desktop
          </DialogTitle>
          <DialogDescription>
            Anyone with this URL gets full access to this desktop server — all sessions, projects, files, terminals, and settings — because the link carries the server credential. Only share it with people you trust with complete control.
          </DialogDescription>
        </DialogHeader>

        {isRemoteSession() ? (
          <div className="text-sm text-muted-foreground pt-2" data-testid="share-dialog-remote-unavailable">
            Sharing isn't available for a remote session yet — the generated link would carry this
            session's bearer token in a query string, which a remote server rejects outright and
            which could otherwise leak the token via browser history or clipboard managers.
          </div>
        ) : !shareLoaded ? (
          <div className="text-sm text-muted-foreground pt-2" data-testid="share-dialog-loading">
            Resolving share status…
          </div>
        ) : (
          <div className="flex flex-col gap-3 pt-2">
            <div className="flex items-center justify-between gap-2" data-testid="share-dialog-status">
              <div className="flex items-center gap-2 min-w-0">
                <span
                  className={`inline-block w-2 h-2 rounded-full shrink-0 ${
                    running ? "bg-emerald-500" : "bg-muted-foreground/40"
                  }`}
                  aria-hidden="true"
                />
                <span className="text-sm font-medium" data-testid="share-dialog-status-text">
                  {running ? "Sharing" : "Not sharing"}
                </span>
                {running ? (
                  <span
                    className={`text-[11px] ${status?.kind === "funnel" ? "text-amber-500" : "text-muted-foreground"}`}
                    data-testid="share-dialog-kind"
                  >
                    {kindLabel}
                  </span>
                ) : null}
              </div>

              {running ? (
                stopArmed ? (
                  <div className="flex items-center gap-2 shrink-0">
                    <Button
                      size="sm"
                      variant="destructive"
                      onClick={handleStop}
                      disabled={stopping}
                      data-testid="share-dialog-stop-confirm"
                    >
                      {stopping ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Square className="w-3.5 h-3.5" />}
                      {stopping ? "Stopping…" : "Confirm stop"}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setStopArmed(false)}
                      disabled={stopping}
                      data-testid="share-dialog-stop-cancel"
                    >
                      Cancel
                    </Button>
                  </div>
                ) : (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setStopArmed(true)}
                    className="gap-1 shrink-0"
                    data-testid="share-dialog-stop"
                  >
                    <Square className="w-3.5 h-3.5" />
                    Stop
                  </Button>
                )
              ) : (
                <Button
                  size="sm"
                  onClick={handleStart}
                  disabled={starting || status?.available === false}
                  className="gap-1 shrink-0"
                  data-testid="share-dialog-start"
                >
                  {starting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Power className="w-3.5 h-3.5" />}
                  {starting ? "Starting…" : "Start sharing"}
                </Button>
              )}
            </div>

            {actionError ? (
              <p className="text-[11px] text-red-500" data-testid="share-dialog-action-error">
                {actionError}
              </p>
            ) : null}

            {running && primaryUrl ? (
              <>
                <div className="flex gap-2">
                  <Input
                    ref={primaryInputRef as any}
                    value={primaryUrl}
                    readOnly
                    className="font-mono text-xs flex-1"
                    onFocus={(e) => e.currentTarget.select()}
                  />
                  <Button size="sm" onClick={handleCopy} className="shrink-0 gap-1" data-testid="share-dialog-copy">
                    {copied ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                    {copied ? "Copied" : "Copy"}
                  </Button>
                </div>
                {copyFailed ? (
                  <p className="text-[11px] text-amber-500" data-testid="share-dialog-copy-failed">
                    Automatic copy was blocked — the link above is selected, press ⌘C / Ctrl+C to copy it.
                  </p>
                ) : null}

                {status?.kind === "funnel" ? (
                  <p className="text-[11px] text-amber-500" data-testid="share-dialog-public-warning">
                    This share is PUBLIC on the internet (Tailscale funnel), not limited to your tailnet.
                  </p>
                ) : (
                  <p className="text-[11px] text-muted-foreground" data-testid="share-dialog-tailscale">
                    Works anywhere on your tailnet{lanUrl ? "; LAN fallback below" : ""}.
                  </p>
                )}
                {lanUrl ? (
                  <div className="flex gap-2 items-center">
                    <Input value={lanUrl} readOnly className="font-mono text-xs flex-1" onFocus={(e) => e.currentTarget.select()} data-testid="share-dialog-lan-url" />
                  </div>
                ) : null}
                {tailscaleHint ? (
                  <p className="text-[11px] text-muted-foreground">Tailscale setup: {tailscaleHint}</p>
                ) : null}
              </>
            ) : (
              <div className="text-[11px] text-muted-foreground" data-testid="share-dialog-stopped">
                {status?.available === false ? (
                  <>
                    <p data-testid="share-dialog-unavailable">
                      Tailscale isn&apos;t installed or isn&apos;t running, so there&apos;s nothing to start.
                      Install and sign in to Tailscale, then reopen this dialog.
                    </p>
                    {primaryUrl ? (
                      // LAN fallback retained from the pre-start/stop dialog: if
                      // this machine has a routable LAN address the server can
                      // still be reached on the local network without tailscale.
                      <div className="flex gap-2 items-center mt-2">
                        <Input
                          value={primaryUrl}
                          readOnly
                          className="font-mono text-xs flex-1"
                          onFocus={(e) => e.currentTarget.select()}
                          data-testid="share-dialog-lan-url"
                        />
                      </div>
                    ) : null}
                  </>
                ) : (
                  <p data-testid="share-dialog-not-sharing">
                    Nothing is exposed right now. Press <span className="font-medium">Start sharing</span> to
                    publish this desktop server — funnel (public internet) is tried first, tailnet-only serve
                    otherwise.
                  </p>
                )}
                {tailscaleHint ? (
                  <p className="mt-1">Tailscale setup: {tailscaleHint}</p>
                ) : null}
                {autoShareEnabled ? (
                  <p className="mt-1 text-amber-500" data-testid="share-dialog-auto-restart">
                    Auto share on start is enabled — sharing will start again the next time ocode launches.
                  </p>
                ) : null}
              </div>
            )}

            {shareToken ? (
              <div className="border-t pt-3" data-testid="share-dialog-reset">
                {resetArmed ? (
                  <div className="flex flex-col gap-2" data-testid="share-dialog-reset-confirm">
                    <p className="text-[11px] text-amber-500">
                      Reset the share token? Every link already shared with this token stops working
                      immediately — anyone holding one will have to open a fresh one. This window
                      stays connected.
                    </p>
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        onClick={handleResetToken}
                        disabled={resetting}
                        data-testid="share-dialog-reset-confirm-yes"
                      >
                        {resetting ? "Resetting…" : "Yes, reset token"}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setResetArmed(false)}
                        disabled={resetting}
                        data-testid="share-dialog-reset-cancel"
                      >
                        Cancel
                      </Button>
                    </div>
                  </div>
                ) : (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setResetArmed(true)}
                    className="gap-1"
                    data-testid="share-dialog-reset-start"
                  >
                    <RotateCcw className="w-3.5 h-3.5" />
                    Reset share token
                  </Button>
                )}
                {resetError ? (
                  <p className="text-[11px] text-red-500 mt-2" data-testid="share-dialog-reset-error">
                    {resetError}
                  </p>
                ) : null}
              </div>
            ) : null}

            <div className="text-[11px] text-muted-foreground border-t pt-3">
              <p>Desktop shell: <code className="bg-muted px-1 py-0.5 rounded">Share</code> menu → <code className="bg-muted px-1 py-0.5 rounded">⌘⇧S</code> for session.</p>
              <p className="mt-1">TUI equivalent: <code className="bg-muted px-1 py-0.5 rounded">/rc [port]</code> / <code className="bg-muted px-1 py-0.5 rounded">/rc off</code> in the chat input.</p>
              {shareToken ? (
                <p className="mt-1" data-testid="share-dialog-persistent-note">
                  This link survives restarts of ocode. Revoke it with{" "}
                  <code className="bg-muted px-1 py-0.5 rounded">Reset share token</code> above, or{" "}
                  <code className="bg-muted px-1 py-0.5 rounded">Share</code> →{" "}
                  <code className="bg-muted px-1 py-0.5 rounded">Reset Share Token…</code>.
                </p>
              ) : null}
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
