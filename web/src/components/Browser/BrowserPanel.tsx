import { useCallback, useEffect, useRef, useState } from "react";
import { getBrowseBase, mintBrowseGrant, browseSrc } from "../../api/client";
import { useBrowserStore, useBrowserActions, type StateKey } from "../../lib/browserStore";
import { AddressBar } from "./AddressBar";
import { DevConsole } from "./DevConsole";
import { useBrowserMessages } from "./useBrowserMessages";

export function BrowserPanel({ stateKey, mode }: { stateKey: StateKey; mode: "side" | "full" }) {
  const s = useBrowserStore(stateKey);
  const actions = useBrowserActions();
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const [base, setBase] = useState<string | null>(null);
  const grantedRef = useRef(false); // grant is one-time; cookie carries auth after.

  useEffect(() => {
    getBrowseBase().then(setBase).catch((e) => console.error("browse: base fetch failed:", e));
  }, []);

  useBrowserMessages(stateKey, base, {
    pushConsole: actions.pushConsole,
    pushNetwork: actions.pushNetwork,
  });

  const showIframe = !!s && s.panelOpen && !s.collapsed;

  // Load / reload: mint a grant on the FIRST src set for this panel session,
  // then set the iframe src via ref with replace semantics (no host history).
  const loadInto = useCallback(async (iframe: HTMLIFrameElement, url: string) => {
    if (!base || !url) return;
    let grant: string | undefined;
    if (!grantedRef.current) {
      grant = await mintBrowseGrant(stateKey).catch((e) => { console.error("browse: grant mint failed:", e); return undefined; });
      grantedRef.current = true;
    }
    iframe.src = browseSrc(base, grant ?? null, stateKey, url);
  }, [base, stateKey]);

  useEffect(() => {
    if (!s) return;
    const iframe = iframeRef.current;
    if (showIframe && iframe && base) void loadInto(iframe, s.url);
    // Re-run on url change (navigate/back/forward/reload set a new s.url or bump a nonce).
  }, [showIframe, base, s?.url, loadInto]);

  // Collapse unmounts the iframe but keeps store state; expanding remints a grant.
  useEffect(() => { if (!showIframe) grantedRef.current = false; }, [showIframe]);

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
        onNavigate={(url) => actions.navigate(stateKey, url)}
        onBack={() => actions.back(stateKey)}
        onForward={() => actions.forward(stateKey)}
        onReload={() => actions.navigate(stateKey, s.url)}
        onOpenExternal={() => window.open(s.url, "_blank", "noopener")}
      />
      <div className="flex-1 min-h-0">
        {showIframe && (
          <iframe
            ref={iframeRef}
            title="Embedded browser"
            className="w-full h-full border-0 bg-white"
            sandbox="allow-scripts allow-forms allow-same-origin"
            allow=""
          />
        )}
      </div>
      <div className="h-48 shrink-0">
        <DevConsole
          consoleEvents={s.consoleEvents}
          networkEvents={s.networkEvents}
          onClearConsole={() => actions.clearConsole(stateKey)}
          onClearNetwork={() => actions.clearNetwork(stateKey)}
        />
      </div>
    </div>
  );
}
