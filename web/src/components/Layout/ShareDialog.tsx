import { useEffect, useState, useCallback, useRef } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "../ui/dialog";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Copy, Check, Monitor, RotateCcw } from "lucide-react";
import { authToken, isRemoteSession, authedFetch, apiPath } from "../../api/client";
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
  const [tailscaleUrl, setTailscaleUrl] = useState<string | null>(null);
  const [tailscaleHint, setTailscaleHint] = useState<string | null>(null);
  const [shareLoaded, setShareLoaded] = useState(false);
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
  const tailscaleRef = useRef<string | null>(null);
  const networkIPRef = useRef<string | null>(null);
  const shareTokenRef = useRef<string | null>(null);
  const shareLoadedRef = useRef(false);
  tailscaleRef.current = tailscaleUrl;
  networkIPRef.current = networkIP;
  shareTokenRef.current = shareToken;
  shareLoadedRef.current = shareLoaded;

  const resolveShareInfo = useCallback(async (): Promise<{
    tailscale: string | null;
    lan: string | null;
    token: string | null;
  }> => {
    // Cached: the server caches one tailscale exposure per process, and the
    // LAN IP is stable for the lifetime of the dialog.
    if (shareLoadedRef.current) {
      return {
        tailscale: tailscaleRef.current,
        lan: networkIPRef.current,
        token: shareTokenRef.current,
      };
    }
    let tailscale: string | null = tailscaleRef.current;
    let lan: string | null = networkIPRef.current;
    // Tailscale first: the server caches one exposure per process, so this
    // never spawns a process per dialog open — later opens reuse the URL.
    try {
      const r = await authedFetch(apiPath("/api/tailscale-url"));
      if (r.ok) {
        const data: any = await r.json().catch(() => null);
        if (data && typeof data.url === "string" && data.url !== "") {
          tailscale = data.url.replace(/\/+$/, "");
          setTailscaleUrl(tailscale);
          if (typeof data.hint === "string" && data.hint !== "") {
            setTailscaleHint(data.hint);
          }
        }
      }
    } catch {}
    // LAN fallback comes from the server's interface ranking — never
    // synthesize localhost here. A "localhost" share URL is unreachable
    // from any other device.
    try {
      const r = await authedFetch(apiPath("/api/network-ip"));
      if (r.ok) {
        const data: any = await r.json().catch(() => null);
        if (isUsableIP(data?.ip)) {
          lan = data.ip;
          setNetworkIP(lan);
        }
      }
    } catch {}
    // Durable share token (desktop shell only). Preferred over authToken()
    // because it survives a restart, so a link already in someone's hands keeps
    // working; authToken() is this launch's credential and would make every
    // shared URL expire on the next quit. A plain server has no such route and
    // answers 404, which leaves the launch token in place.
    let token: string | null = shareTokenRef.current;
    if (isDesktopShell()) {
      token = "";
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
    shareLoadedRef.current = true;
    return { tailscale, lan, token };
  }, []);

  useEffect(() => {
    // Resolve lazily on open only: fetching /api/tailscale-url starts the
    // tailscale serve/funnel exposure, so resolving at mount (this dialog is
    // mounted unconditionally) would expose the authenticated server on every
    // launch even when the user never shares. Remote sessions never resolve:
    // their token is rejected by design and must not be embedded in a URL.
    if (!open || isRemoteSession()) return;
    // Values are idempotent and the component stays mounted when the dialog
    // closes, so no cancelled guard is needed — keep whatever resolves.
    void resolveShareInfo();
  }, [open, resolveShareInfo]);

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
      // tailscale/LAN haven't resolved yet (the webview sits on 127.0.0.1,
      // so the loopback fallback yields ""). Resolve on demand instead of
      // copying the stale (empty) closure value.
      const info = await resolveShareInfo();
      const url = buildPrimaryUrl(info.tailscale, info.lan, tokenQuery(info.token || authToken()));
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
  }, [resolveShareInfo, selectPrimaryInput]);

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
            Resolving share URLs…
          </div>
        ) : primaryUrl === "" ? (
          <div className="text-sm text-muted-foreground pt-2" data-testid="share-dialog-unavailable">
            Sharing isn't available — no tailscale URL and no LAN address were found. Check that
            tailscale is running or that this machine has a LAN connection, then reopen this dialog.
          </div>
        ) : (
          <div className="flex flex-col gap-3 pt-2">
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

            {tailscaleUrl ? (
              <p className="text-[11px] text-muted-foreground" data-testid="share-dialog-tailscale">
                Tailscale URL (works anywhere on your tailnet{lanUrl ? "; LAN fallback below" : ""}).
              </p>
            ) : (
              <p className="text-[11px] text-muted-foreground" data-testid="share-dialog-lan">
                Tailscale isn't available — showing the LAN URL instead. It only works on your local network.
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
