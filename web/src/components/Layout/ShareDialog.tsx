import { useEffect, useState, useCallback, useRef } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "../ui/dialog";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Copy, Check, Monitor } from "lucide-react";
import { authToken, isRemoteSession, authedFetch, apiPath } from "../../api/client";

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

// Best-effort clipboard write that works outside secure contexts (plain
// http://<lan-ip> origins, WKWebView/WebKitGTK shells) where
// navigator.clipboard is undefined or rejects. Returns true on success.
async function copyTextToClipboard(text: string): Promise<boolean> {
  if (text === "") return false;
  try {
    if (
      typeof navigator !== "undefined" &&
      (navigator as any).clipboard &&
      typeof (navigator as any).clipboard.writeText === "function"
    ) {
      await (navigator as any).clipboard.writeText(text);
      return true;
    }
  } catch {
    // Fall through to the execCommand fallback below.
  }
  try {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    // Keep it out of view and out of layout.
    ta.style.position = "fixed";
    ta.style.top = "-9999px";
    ta.style.left = "-9999px";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    // iOS needs an explicit range.
    try {
      ta.setSelectionRange(0, ta.value.length);
    } catch {}
    const ok = document.execCommand("copy");
    document.body.removeChild(ta);
    if (ok) return true;
  } catch {}
  return false;
}

function joinWithToken(base: string, tokenSuffix: string): string {
  return `${base}/${tokenSuffix}`.replace(/\/\//g, "/").replace(":/", "://");
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
  const primaryInputRef = useRef<HTMLInputElement | null>(null);
  // Refs mirror state so the headless "Copy Desktop URL" menu handler (which
  // may fire before the dialog ever opens) always sees fresh values without
  // re-subscribing listeners.
  const tailscaleRef = useRef<string | null>(null);
  const networkIPRef = useRef<string | null>(null);
  const shareLoadedRef = useRef(false);
  tailscaleRef.current = tailscaleUrl;
  networkIPRef.current = networkIP;
  shareLoadedRef.current = shareLoaded;

  const resolveShareInfo = useCallback(async (): Promise<{
    tailscale: string | null;
    lan: string | null;
  }> => {
    // Cached: the server caches one tailscale exposure per process, and the
    // LAN IP is stable for the lifetime of the dialog.
    if (shareLoadedRef.current) {
      return { tailscale: tailscaleRef.current, lan: networkIPRef.current };
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
    setShareLoaded(true);
    shareLoadedRef.current = true;
    return { tailscale, lan };
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

  const tokenSuffix = (() => {
    const token = authToken();
    return token ? `?token=${encodeURIComponent(token)}` : "";
  })();

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

  useEffect(() => {
    const openDialog = () => {
      setOpen(true);
      setCopied(false);
      setCopyFailed(false);
    };
    const onShareSession = () => openDialog();
    const onShareDesktop = () => openDialog();
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
      const token = authToken();
      const suffix = token ? `?token=${encodeURIComponent(token)}` : "";
      const url = buildPrimaryUrl(info.tailscale, info.lan, suffix);
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
    return () => {
      window.removeEventListener("ocode:share-session", onShareSession);
      window.removeEventListener("ocode:share-desktop", onShareDesktop);
      window.removeEventListener("ocode:copy-desktop-url", onCopyDesktop);
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

            <div className="text-[11px] text-muted-foreground border-t pt-3">
              <p>Desktop shell: <code className="bg-muted px-1 py-0.5 rounded">Share</code> menu → <code className="bg-muted px-1 py-0.5 rounded">⌘⇧S</code> for session.</p>
              <p className="mt-1">TUI equivalent: <code className="bg-muted px-1 py-0.5 rounded">/rc [port]</code> / <code className="bg-muted px-1 py-0.5 rounded">/rc off</code> in the chat input.</p>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
