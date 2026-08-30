# Part 09 — BrowserPanel Component (address bar, status, viewport, dev console)

**Spec:** `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md` (§ Frontend, § Security model — postMessage / address bar).

**Goal:** Build the shared `BrowserPanel` React component and its two children (`AddressBar`, `DevConsole`) rendered in both host modes (`side`, `full`). The panel owns the sandboxed cross-origin iframe (one live iframe at a time), the grant-on-first-load flow, and the origin-checked `postMessage` listener that feeds the dev console. The address bar renders **only** store state fed by server nav events — never a page-reported URL.

**Prereqs / consumed interfaces:**
- Part 08 (`web/src/lib/browserStore.ts`): `useBrowserStore` selector hook + actions `open/close/setCollapsed/navigate/back/forward/pushConsole/pushNetwork/clearConsole/clearNetwork/applyNavEvent`; types `StateKey`, `BrowserTabState`, `ConsoleEvent`, `NetworkEvent`. Store is built on `@tanstack/react-store` (`Store` + `useSelector`), matching `web/src/stores/terminalStore.tsx`.
- Part 08 (`web/src/api/client.ts`): `getBrowseBase(): Promise<string>` (cached), `mintBrowseGrant(stateKey: string): Promise<string>`, `browseSrc(base: string, stateKey: string, url: string, grant?: string): string` (builds `${base}/b/${stateKey}/${scheme}/${host}/${path}` with `?__grant=` appended only when `grant` is passed).
- Part 05 postMessage event types (payload `event.data.type`): `"ocode:browse:console"` `{level,args,ts}`, `"ocode:browse:network"` `{method,url,status,duration,ts}`, `"ocode:browse:nav"` (display-untrusted hint — **ignored** here).

**Files:**
- Create: `web/src/components/Browser/AddressBar.tsx`, `web/src/components/Browser/DevConsole.tsx`, `web/src/components/Browser/BrowserPanel.tsx`, `web/src/components/Browser/useBrowserMessages.ts`
- Test: `web/src/components/Browser/AddressBar.test.tsx`, `DevConsole.test.tsx`, `BrowserPanel.test.tsx`

**Interfaces:**
- Consumes: Part 08 store + client exports; Part 05 message types.
- Produces: `export function BrowserPanel({ stateKey, mode }: { stateKey: StateKey; mode: "side" | "full" })` — consumed by Part 10 (side panel + browser tab).

**Test conventions** (from `web/src/components/Chat/ChatPanel.test.tsx`): vitest + `@testing-library/react`; mock `../../api/client` and the store module with `vi.mock`; run with `cd web && npx vitest run <path>`.

---

## AddressBar

- [ ] **Step 1: Write the failing AddressBar test**

Create `web/src/components/Browser/AddressBar.test.tsx`:

```tsx
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { AddressBar } from "./AddressBar";

const base = {
  url: "https://example.com/",
  status: 200,
  mode: "proxied" as const,
  error: "",
  canBack: false,
  canForward: false,
  onNavigate: vi.fn(),
  onBack: vi.fn(),
  onForward: vi.fn(),
  onReload: vi.fn(),
  onOpenExternal: vi.fn(),
};

describe("AddressBar", () => {
  it("navigates on Enter", () => {
    const onNavigate = vi.fn();
    render(<AddressBar {...base} onNavigate={onNavigate} />);
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "https://foo.dev/" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onNavigate).toHaveBeenCalledWith("https://foo.dev/");
  });

  it("disables back/forward at history ends", () => {
    render(<AddressBar {...base} canBack={false} canForward={false} />);
    expect(screen.getByLabelText("Back")).toBeDisabled();
    expect(screen.getByLabelText("Forward")).toBeDisabled();
  });

  it("shows the mode chip and status from props, not the iframe", () => {
    render(<AddressBar {...base} mode="local" status={304} />);
    expect(screen.getByText(/local/i)).toBeInTheDocument();
    expect(screen.getByText("304")).toBeInTheDocument();
  });

  it("shows an error badge when nav failed", () => {
    render(<AddressBar {...base} status={0} error="connection refused" />);
    expect(screen.getByText(/connection refused/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/components/Browser/AddressBar.test.tsx`
Expected: FAIL — cannot resolve `./AddressBar`.

- [ ] **Step 3: Implement AddressBar**

Create `web/src/components/Browser/AddressBar.tsx`:

```tsx
import { useEffect, useState } from "react";

export interface AddressBarProps {
  url: string;
  status: number; // 0 = loading/no response yet
  mode: "local" | "proxied";
  error: string;
  canBack: boolean;
  canForward: boolean;
  onNavigate: (url: string) => void;
  onBack: () => void;
  onForward: () => void;
  onReload: () => void;
  onOpenExternal: () => void;
}

// The displayed URL is authoritative store state (fed by server nav events),
// never a value reported by the proxied page — page JS could spoof it.
export function AddressBar(props: AddressBarProps) {
  const { url, status, mode, error, canBack, canForward } = props;
  const [draft, setDraft] = useState(url);
  useEffect(() => setDraft(url), [url]);

  const loading = status === 0 && !error;

  return (
    <div className="flex items-center gap-1 px-2 py-1 border-b border-neutral-200 dark:border-neutral-800 text-sm">
      <button aria-label="Back" disabled={!canBack} onClick={props.onBack}
        className="px-1 disabled:opacity-30">‹</button>
      <button aria-label="Forward" disabled={!canForward} onClick={props.onForward}
        className="px-1 disabled:opacity-30">›</button>
      <button aria-label="Reload" onClick={props.onReload} className="px-1">⟳</button>
      <input
        role="textbox"
        aria-label="Address"
        className="flex-1 min-w-0 rounded bg-neutral-100 dark:bg-neutral-900 px-2 py-1"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => { if (e.key === "Enter") props.onNavigate(draft.trim()); }}
      />
      {loading && <span aria-label="Loading" className="animate-spin px-1">◌</span>}
      {status > 0 && (
        <span className={"px-1 tabular-nums " + (status >= 400 ? "text-red-500" : "text-neutral-500")}>
          {status}
        </span>
      )}
      <span className="px-1 text-xs uppercase text-neutral-400">{mode}</span>
      {error && <span className="px-1 text-xs text-red-500 truncate max-w-[12rem]">{error}</span>}
      <button aria-label="Open externally" onClick={props.onOpenExternal} className="px-1">↗</button>
    </div>
  );
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/components/Browser/AddressBar.test.tsx`
Expected: PASS.

## DevConsole

- [ ] **Step 5: Write the failing DevConsole test**

Create `web/src/components/Browser/DevConsole.test.tsx`:

```tsx
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { DevConsole } from "./DevConsole";

const consoleEvents = [
  { level: "log", text: "hello world", ts: 1 },
  { level: "error", text: "boom failure", ts: 2 },
];
const networkEvents = [
  { method: "GET", url: "https://a.dev/x", status: 200, duration: 12, ts: 1 },
];

describe("DevConsole", () => {
  const base = {
    consoleEvents,
    networkEvents,
    onClearConsole: vi.fn(),
    onClearNetwork: vi.fn(),
  };

  it("filters console entries", () => {
    render(<DevConsole {...base} />);
    expect(screen.getByText(/hello world/)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Filter"), { target: { value: "boom" } });
    expect(screen.queryByText(/hello world/)).not.toBeInTheDocument();
    expect(screen.getByText(/boom failure/)).toBeInTheDocument();
  });

  it("clears the console", () => {
    const onClearConsole = vi.fn();
    render(<DevConsole {...base} onClearConsole={onClearConsole} />);
    fireEvent.click(screen.getByLabelText("Clear"));
    expect(onClearConsole).toHaveBeenCalled();
  });

  it("switches to the Network tab", () => {
    render(<DevConsole {...base} />);
    fireEvent.click(screen.getByRole("tab", { name: /network/i }));
    expect(screen.getByText("https://a.dev/x")).toBeInTheDocument();
  });
});
```

- [ ] **Step 6: Run to verify it fails**

Run: `cd web && npx vitest run src/components/Browser/DevConsole.test.tsx`
Expected: FAIL — cannot resolve `./DevConsole`.

- [ ] **Step 7: Implement DevConsole**

Create `web/src/components/Browser/DevConsole.tsx`:

```tsx
import { useMemo, useState } from "react";
import type { ConsoleEvent, NetworkEvent } from "../../lib/browserStore";

export interface DevConsoleProps {
  consoleEvents: ConsoleEvent[];
  networkEvents: NetworkEvent[];
  onClearConsole: () => void;
  onClearNetwork: () => void;
}

const levelColor: Record<string, string> = {
  error: "text-red-500",
  warn: "text-amber-500",
  info: "text-blue-400",
  log: "text-neutral-300",
  debug: "text-neutral-500",
};

// Entries are appended in arrival order (oldest→newest); store ring-buffers at
// 1000 each, so no pagination is needed — the cap bounds the list.
export function DevConsole(props: DevConsoleProps) {
  const [tab, setTab] = useState<"console" | "network">("console");
  const [filter, setFilter] = useState("");

  const consoleRows = useMemo(
    () => props.consoleEvents.filter((e) => e.text.toLowerCase().includes(filter.toLowerCase())),
    [props.consoleEvents, filter],
  );
  const netRows = useMemo(
    () => props.networkEvents.filter((e) => e.url.toLowerCase().includes(filter.toLowerCase())),
    [props.networkEvents, filter],
  );

  return (
    <div className="flex flex-col h-full text-xs font-mono border-t border-neutral-200 dark:border-neutral-800">
      <div className="flex items-center gap-2 px-2 py-1 shrink-0">
        <button role="tab" aria-selected={tab === "console"} onClick={() => setTab("console")}
          className={tab === "console" ? "font-bold" : "opacity-60"}>Console</button>
        <button role="tab" aria-selected={tab === "network"} onClick={() => setTab("network")}
          className={tab === "network" ? "font-bold" : "opacity-60"}>Network</button>
        <input aria-label="Filter" value={filter} onChange={(e) => setFilter(e.target.value)}
          placeholder="filter" className="ml-auto rounded bg-neutral-100 dark:bg-neutral-900 px-2 py-0.5" />
        <button aria-label="Clear"
          onClick={tab === "console" ? props.onClearConsole : props.onClearNetwork}>Clear</button>
      </div>
      <div className="flex-1 overflow-auto px-2 pb-2">
        {tab === "console"
          ? consoleRows.map((e, i) => (
              <div key={i} className={levelColor[e.level] ?? ""}>{e.text}</div>
            ))
          : (
            <table className="w-full">
              <tbody>
                {netRows.map((e, i) => (
                  <tr key={i} className={e.status >= 400 ? "text-red-500" : ""}>
                    <td className="pr-2">{e.method}</td>
                    <td className="pr-2 truncate max-w-[24rem]">{e.url}</td>
                    <td className="pr-2 tabular-nums">{e.status}</td>
                    <td className="tabular-nums">{e.duration}ms</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
      </div>
    </div>
  );
}
```

- [ ] **Step 8: Run to verify pass**

Run: `cd web && npx vitest run src/components/Browser/DevConsole.test.tsx`
Expected: PASS.

## useBrowserMessages (origin-checked listener)

- [ ] **Step 9: Implement the listener hook (tested via BrowserPanel)**

Create `web/src/components/Browser/useBrowserMessages.ts`:

```ts
import { useEffect } from "react";
import type { StateKey } from "../../lib/browserStore";

interface Handlers {
  pushConsole: (key: StateKey, ev: { level: string; text: string; ts: number }) => void;
  pushNetwork: (key: StateKey, ev: { method: string; url: string; status: number; duration: number; ts: number }) => void;
}

// Accepts messages ONLY from the browse origin. Everything else — including the
// SPA's own origin and any other frame — is dropped. "ocode:browse:nav" is
// intentionally ignored: the address bar is driven by server nav events, so a
// page-reported URL is never trusted for display.
export function useBrowserMessages(stateKey: StateKey, browseBase: string | null, h: Handlers) {
  useEffect(() => {
    if (!browseBase) return;
    const origin = new URL(browseBase).origin;
    function onMessage(e: MessageEvent) {
      if (e.origin !== origin) return; // hard origin gate
      const d = e.data;
      if (!d || typeof d !== "object") return;
      if (d.stateKey !== stateKey) return;
      switch (d.type) {
        case "ocode:browse:console":
          h.pushConsole(stateKey, { level: String(d.level ?? "log"), text: String((d.args ?? []).join(" ")), ts: Number(d.ts) || Date.now() });
          break;
        case "ocode:browse:network":
          h.pushNetwork(stateKey, { method: String(d.method), url: String(d.url), status: Number(d.status) || 0, duration: Number(d.duration) || 0, ts: Number(d.ts) || Date.now() });
          break;
        // "ocode:browse:nav" intentionally not handled: display-untrusted.
        default:
          break;
      }
    }
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [stateKey, browseBase, h]);
}
```

## BrowserPanel

- [ ] **Step 10: Write the failing BrowserPanel test**

Create `web/src/components/Browser/BrowserPanel.test.tsx`:

```tsx
import { render, screen, waitFor, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const state = {
  url: "https://example.com/",
  history: ["https://example.com/"],
  historyIndex: 0,
  panelOpen: true,
  collapsed: false,
  status: 200,
  mode: "proxied" as const,
  error: "",
  consoleEvents: [],
  networkEvents: [],
};
const actions = {
  navigate: vi.fn(), back: vi.fn(), forward: vi.fn(),
  pushConsole: vi.fn(), pushNetwork: vi.fn(),
  clearConsole: vi.fn(), clearNetwork: vi.fn(),
  setCollapsed: vi.fn(), close: vi.fn(),
};
vi.mock("../../lib/browserStore", () => ({
  useBrowserStore: (key: string) => ({ ...state }),
  useBrowserActions: () => actions,
}));
vi.mock("../../api/client", () => ({
  getBrowseBase: vi.fn(async () => "http://127.0.0.1:54321"),
  mintBrowseGrant: vi.fn(async () => "GRANT123"),
  browseSrc: (base: string, key: string, url: string, grant?: string) =>
    `${base}/b/${key}/https/example.com/${grant ? "?__grant=" + grant : ""}`,
}));

import { BrowserPanel } from "./BrowserPanel";

describe("BrowserPanel", () => {
  beforeEach(() => vi.clearAllMocks());

  it("mounts the iframe with the grant only on first load", async () => {
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    const iframe = await waitFor(() => container.querySelector("iframe")!);
    expect(iframe).toBeTruthy();
    await waitFor(() => expect(iframe.getAttribute("src")).toContain("__grant=GRANT123"));
    expect(iframe.getAttribute("sandbox")).toBe("allow-scripts allow-forms allow-same-origin");
  });

  it("does not mount an iframe when collapsed", () => {
    state.collapsed = true;
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="side" />);
    expect(container.querySelector("iframe")).toBeNull();
    state.collapsed = false;
  });

  it("routes console messages only from the browse origin", async () => {
    render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    await waitFor(() => expect(true).toBe(true));
    act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        origin: "http://127.0.0.1:54321",
        data: { type: "ocode:browse:console", stateKey: "tab:abc", level: "log", args: ["hi"], ts: 1 },
      }));
    });
    expect(actions.pushConsole).toHaveBeenCalled();
    act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        origin: "https://evil.example",
        data: { type: "ocode:browse:console", stateKey: "tab:abc", level: "log", args: ["x"], ts: 2 },
      }));
    });
    expect(actions.pushConsole).toHaveBeenCalledTimes(1); // foreign origin dropped
  });
});
```

- [ ] **Step 11: Run to verify it fails**

Run: `cd web && npx vitest run src/components/Browser/BrowserPanel.test.tsx`
Expected: FAIL — cannot resolve `./BrowserPanel`.

- [ ] **Step 12: Implement BrowserPanel**

Create `web/src/components/Browser/BrowserPanel.tsx`:

```tsx
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

  useEffect(() => { getBrowseBase().then(setBase).catch((e) => console.error("browse: base fetch failed:", e)); }, []);

  useBrowserMessages(stateKey, base, {
    pushConsole: actions.pushConsole,
    pushNetwork: actions.pushNetwork,
  });

  const showIframe = s.panelOpen && !s.collapsed;

  // Load / reload: mint a grant on the FIRST src set for this panel session,
  // then set the iframe src via ref with replace semantics (no host history).
  const loadInto = useCallback(async (iframe: HTMLIFrameElement, url: string) => {
    if (!base) return;
    let grant: string | undefined;
    if (!grantedRef.current) {
      grant = await mintBrowseGrant(stateKey).catch((e) => { console.error("browse: grant mint failed:", e); return undefined; });
      grantedRef.current = true;
    }
    iframe.src = browseSrc(base, stateKey, url, grant);
  }, [base, stateKey]);

  useEffect(() => {
    const iframe = iframeRef.current;
    if (showIframe && iframe && base) void loadInto(iframe, s.url);
    // Re-run on url change (navigate/back/forward/reload set a new s.url or bump a nonce).
  }, [showIframe, base, s.url, loadInto]);

  // Collapse unmounts the iframe but keeps store state; expanding remints a grant.
  useEffect(() => { if (!showIframe) grantedRef.current = false; }, [showIframe]);

  return (
    <div className="flex flex-col h-full min-w-0" data-testid={`browser-${mode}`}>
      <AddressBar
        url={s.url}
        status={s.status}
        mode={s.mode}
        error={s.error}
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
```

Notes for the executor:
- `sandbox` deliberately keeps `allow-same-origin` — "same-origin" here means the **browse** origin only, which is already cross-origin to the SPA, so page JS still cannot reach `window.parent`. Removing it would break cookies/storage the proxied site needs.
- One live iframe: Part 10 renders only the *active* panel/tab's `BrowserPanel` with `showIframe` true; inactive ones keep chrome mounted-hidden but their `showIframe` is false, so no iframe exists. Document that switching tabs reloads the page (in-page form state lost) — this is the accepted cost of per-tab isolation.

- [ ] **Step 13: Run to verify pass**

Run: `cd web && npx vitest run src/components/Browser/BrowserPanel.test.tsx`
Expected: PASS.

- [ ] **Step 14: Typecheck + full Browser suite**

Run: `cd web && npx vitest run src/components/Browser/ && npm run typecheck`
Expected: all PASS, no type errors. (If `useBrowserStore`/`useBrowserActions`/`browseSrc` signatures differ from Part 08 as built, reconcile to Part 08 — it is the source of truth for the store API.)

- [ ] **Step 15: Commit**

```bash
git add web/src/components/Browser/
git commit -m "feat(browse): BrowserPanel with address bar, status, sandboxed viewport, dev console"
```
