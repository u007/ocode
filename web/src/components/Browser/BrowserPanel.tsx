import { useCallback, useEffect, useRef, useState } from "react";
import { getBrowseBase, mintBrowseGrant, browseSrc, normalizeBrowseURL, bypassBrowseTLS } from "../../api/client";
import { useBrowserStore, useBrowserActions, isPrivateHost, type StateKey } from "../../lib/browserStore";
import { AddressBar } from "./AddressBar";
import { DevConsole } from "./DevConsole";
import { useBrowserMessages } from "./useBrowserMessages";
import { ChromeViewport } from "./ChromeViewport";

// Frontend parity of Go's isLoopbackHost — handles [::1]:port via URL.hostname
function isLoopbackHost(hostname: string): boolean {
  const lower = hostname.toLowerCase().replace(/\.$/, "").trim();
  if (lower === "localhost" || lower.endsWith(".localhost")) return true;
  if (lower === "127.0.0.1" || lower === "::1") return true;
  if (/^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(lower)) return true;
  return false;
}

export function BrowserPanel({ stateKey, mode }: { stateKey: StateKey; mode: "side" | "full" }) {
  const s = useBrowserStore(stateKey);
  const actions = useBrowserActions();
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const [base, setBase] = useState<string | null>(null);
  const [bypassing, setBypassing] = useState(false);
  const [dismissedLoopbackWarning, setDismissedLoopbackWarning] = useState(false);
  const loadGeneration = useRef(0);

  useEffect(() => {
    getBrowseBase().then(setBase).catch((e) => console.error("browse: base fetch failed:", e));
  }, []);

  useBrowserMessages(stateKey, base, {
    pushConsole: actions.pushConsole,
    pushNetwork: actions.pushNetwork,
  });

  const showIframe = !!s && s.panelOpen && !s.collapsed;

  // Surface selection: the server nav event's mode is authoritative once one
  // has arrived; before that (mode null, first mount) fall back to the host
  // predicate — the same rule the Go router uses, so the SPA mounts the right
  // viewport before the first browse_nav lands.
  const effectiveMode: "local" | "chrome" = (() => {
    if (!s) return "local";
    if (s.mode) return s.mode;
    try {
      const u = new URL(normalizeBrowseURL(s.url));
      return isPrivateHost(u.host) ? "local" : "chrome";
    } catch {
      return "chrome";
    }
  })();

  // Every top-level iframe load carries a fresh grant. Besides authenticating
  // the browse origin, this is the one-use proof that a local/private
  // document was initiated by the SPA rather than by an external page link.
  const loadInto = useCallback(async (iframe: HTMLIFrameElement, url: string) => {
    if (!base || !url) return;
    const generation = ++loadGeneration.current;
    try {
      const grant = await mintBrowseGrant(stateKey);
      if (generation !== loadGeneration.current) return;
      iframe.src = browseSrc(base, grant, stateKey, url);
    } catch (e) {
      if (generation !== loadGeneration.current) return;
      console.error("browse: load failed:", e);
      actions.setError(stateKey, e instanceof Error ? e.message : String(e));
    }
  }, [actions, base, stateKey]);

  useEffect(() => {
    if (!s) return;
    const iframe = iframeRef.current;
    if (showIframe && iframe && base) void loadInto(iframe, s.url);
    // Re-run on url change (navigate/back/forward/reload set a new s.url or bump a nonce).
  }, [showIframe, base, s?.url, s?.historyIndex, loadInto]);

  // Reset the dismiss flag on navigation.
  useEffect(() => {
    setDismissedLoopbackWarning(false);
  }, [s?.url]);

  // Only local/private navigations can show the bypass interstitial — a
  // Chrome-mode error must never offer to bypass a public host.
  const isTLSBypassable = !!s?.error && s.error.includes("TLS certificate") && s.mode === "local";
  const bypassHost = (() => {
    if (!s?.url) return "";
    try {
      return new URL(normalizeBrowseURL(s.url)).host;
    } catch {
      return "";
    }
  })();

  const isLoopbackHttps = (() => {
    if (!s?.url || s.error || s.status === 0) return false;
    if (s.mode !== "local") return false;
    try {
      const u = new URL(normalizeBrowseURL(s.url));
      if (u.protocol !== "https:") return false;
      return isLoopbackHost(u.hostname);
    } catch {
      return false;
    }
  })();

  const handleBypass = useCallback(async () => {
    if (!bypassHost) return;
    const urlAtBypass = s?.url;
    const genAtBypass = loadGeneration.current;
    setBypassing(true);
    try {
      await bypassBrowseTLS(stateKey, bypassHost);
      // Guard the race: if the user navigated elsewhere while the bypass
      // POST was in flight, don't reload the stale URL.
      if (s?.url !== urlAtBypass || genAtBypass !== loadGeneration.current) return;
      const iframe = iframeRef.current;
      if (iframe && s?.url && base) await loadInto(iframe, s.url);
    } catch (e) {
      actions.setError(stateKey, e instanceof Error ? e.message : String(e));
    } finally {
      setBypassing(false);
    }
  }, [bypassHost, stateKey, s?.url, base, loadInto, actions, s]);

  if (!s) return null;

  return (
    <div className="flex flex-col h-full min-w-0" data-testid={`browser-${mode}`}>
      <AddressBar
        url={s.url}
        status={s.status}
        mode={s.mode}
        error={s.error ?? ""}
        canBack={s.historyIndex > 0}
        canForward={s.historyIndex < s.history.length - 1}
        onNavigate={(url) => {
          try {
            actions.navigate(stateKey, normalizeBrowseURL(url));
          } catch (e) {
            actions.setError(stateKey, e instanceof Error ? e.message : String(e));
          }
        }}
        onBack={() => actions.back(stateKey)}
        onForward={() => actions.forward(stateKey)}
        onReload={() => actions.navigate(stateKey, s.url)}
        onOpenExternal={() => window.open(s.url, "_blank", "noopener")}
      />
      {isTLSBypassable && (
        <div className="px-3 py-2 bg-amber-50 dark:bg-amber-950 border-b border-amber-200 dark:border-amber-800 text-sm flex items-center gap-3" role="alert" data-testid="tls-bypass-banner">
          <span className="flex-1">
            <span className="font-medium">Certificate isn’t trusted</span>
            <span className="text-neutral-600 dark:text-neutral-400"> — {s.error}</span>
            <span className="text-neutral-600 dark:text-neutral-400">. This is expected for self-signed dev certs. Continue only if you trust {bypassHost || "this host"}.</span>
          </span>
          <button
            onClick={handleBypass}
            disabled={bypassing}
            className="shrink-0 rounded bg-amber-600 hover:bg-amber-700 disabled:opacity-50 text-white px-3 py-1 text-xs font-medium"
          >
            {bypassing ? "Continuing…" : "Continue anyway"}
          </button>
        </div>
      )}
      {isLoopbackHttps && !isTLSBypassable && !dismissedLoopbackWarning && (
        <div className="px-3 py-1.5 bg-amber-50/70 dark:bg-amber-950/50 border-b border-amber-200/50 dark:border-amber-800/50 text-xs flex items-center gap-2" role="status" data-testid="loopback-insecure-banner">
          <span className="text-amber-700 dark:text-amber-300">⚠︎ Not secure</span>
          <span className="text-neutral-600 dark:text-neutral-400">— self-signed certificate, auto-allowed for localhost.</span>
          <button
            aria-label="Dismiss"
            onClick={() => setDismissedLoopbackWarning(true)}
            className="ml-auto text-neutral-500 hover:text-neutral-700 dark:text-neutral-400 dark:hover:text-neutral-200 px-1"
          >
            ×
          </button>
        </div>
      )}
      <div className="flex-1 min-h-0">
        {showIframe && effectiveMode === "local" && (
          <iframe
            ref={iframeRef}
            title="Embedded browser"
            className="w-full h-full border-0 bg-white"
            sandbox="allow-scripts allow-forms allow-same-origin"
            allow=""
          />
        )}
        {showIframe && effectiveMode === "chrome" && (
          <ChromeViewport stateKey={stateKey} browseBase={base} url={s.url} />
        )}
      </div>
      <DevConsole
        consoleEvents={s.consoleEvents}
        networkEvents={s.networkEvents}
        onClearConsole={() => actions.clearConsole(stateKey)}
        onClearNetwork={() => actions.clearNetwork(stateKey)}
      />
    </div>
  );
}
