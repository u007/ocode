# Part 06 — Frontend: store mode, `useCdpSocket`, `ChromeViewport`, panel integration

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Checkbox steps. High-level by policy (no code in plans). TDD per task (vitest); commit per task.

**Goal:** Render Chrome-mode pages in the panel as a canvas screencast with forwarded input, using the existing store, address bar, and console drawer unchanged.

**Spec:** `docs/superpowers/specs/2026-08-31-browser-chrome-cdp-design.md` § Frontend, § Transport.

**Global constraints:** mode enum `"local" | "chrome" | null`; nav only via `browse_nav` SSE (`sessionEvents.ts` → `browserActions.applyNavEvent`); no new UI library (project uses Tailwind + hand-rolled components — match `BrowserPanel.tsx` style); `shrink-0`/`min-h-0` discipline on flex children; `bunx vitest run` + `bunx tsc --noEmit -p .` green.

## Context an implementer needs

- `web/src/lib/browserStore.ts`: `StateKey`, `ConsoleEvent{level,text,ts}`, `NetworkEvent{method,url,status,durationMs,ts}`, `NavEvent{state_key,url,status,mode,error?}`, `BrowserTabState{url,status,loading,mode,error,history,historyIndex,panelOpen,collapsed,consoleEvents,networkEvents}`, `normalizeBrowseURL`, `PRIVATE_HOST_RE`, `browserActions{open,close,navigate,back,forward,pushConsole,pushNetwork,clearConsole,clearNetwork,applyNavEvent,setCollapsed,…}`, caps 1000.
- `web/src/components/Browser/BrowserPanel.tsx`: fetches `getBrowseBase()`, mints a grant once per mount via `mintBrowseGrant(stateKey)`, sets `iframe.src = browseSrc(base, grant, stateKey, url)`; `useBrowserMessages` handles capture.js postMessage for local mode; renders `AddressBar` + viewport + `DevConsole`.
- `web/src/components/Browser/AddressBar.tsx`: prop `mode: "local" | "proxied" | null`, chip renders `mode` uppercase.
- `web/src/api/client.ts`: `getBrowseBase()`, `mintBrowseGrant(stateKey) → grant string`, `browseSrc(...)`.
- `web/src/lib/sessionEvents.ts` ~line 249: `browse_nav` → `applyNavEvent`.
- Tests live beside components (`*.test.tsx`, jsdom, `@testing-library/react`); `BrowserPanel.test.tsx` mocks the store + client modules.

## Interfaces consumed (Part 05 wire format)

`ws(s)://<browseBase>/b/{stateKey}/__cdp?__grant=<token>`; binary frames `[u32 BE w][u32 BE h]+JPEG`; JSON `{"t":"console"|"network"|"error",…}`; client JSON `{"t":"nav"|"back"|"forward"|"reload"|"resize"|"mouse"|"key",…}`.

## Interfaces produced

- `isPrivateHost(host: string): boolean` (export from `browserStore.ts`, shares `PRIVATE_HOST_RE`, also `*.localhost`).
- `useCdpSocket(stateKey: StateKey, browseBase: string | null, enabled: boolean): { send(msg: CdpClientMessage): void; status: "connecting" | "open" | "reconnecting" | "closed"; error: string | null; onFrame(cb: (bitmap: ImageBitmap, w: number, h: number) => void): () => void }` in `useCdpSocket.ts`.
- `<ChromeViewport stateKey browseBase url />` in `ChromeViewport.tsx`.
- Type `CdpClientMessage` union in `cdpProtocol.ts`.

---

### Task 1: Store — mode enum + `isPrivateHost`

**Files:**
- Modify: `web/src/lib/browserStore.ts`, `web/src/components/Browser/AddressBar.tsx` (prop type + chip: `CHROME`), `web/src/lib/sessionEvents.ts` (typing only if it names the mode)
- Test: `web/src/lib/browserStore.test.ts`, `web/src/components/Browser/AddressBar.test.tsx`

- [ ] Step 1: Write failing tests: `isPrivateHost("app.localhost")`, `("127.0.0.1:80")`, `("[::1]:5173")` → true; `("example.com")`, `("localhost.evil.com")` → false; `applyNavEvent` with `mode:"chrome"` stores it; AddressBar with `mode="chrome"` renders text `CHROME`.
- [ ] Step 2: Run `cd web && bunx vitest run src/lib/browserStore.test.ts src/components/Browser/AddressBar.test.tsx` → fails (type error / missing export).
- [ ] Step 3: Implement; replace every `"proxied"` literal (`grep -rn proxied web/src`).
- [ ] Step 4: Run tests + `bunx tsc --noEmit -p .` → pass.
- [ ] Step 5: Commit `feat(web/browser): chrome mode enum + isPrivateHost`.

---

### Task 2: `useCdpSocket`

**Files:**
- Create: `web/src/components/Browser/cdpProtocol.ts`, `web/src/components/Browser/useCdpSocket.ts`
- Test: `web/src/components/Browser/useCdpSocket.test.ts` (use a hand-written `FakeWebSocket` installed on `globalThis.WebSocket`; mock `mintBrowseGrant`)

- [ ] Step 1: Write failing tests: (a) when `enabled` and `browseBase` set, mints a grant then opens `ws://127.0.0.1:54321/b/tab:abc/__cdp?__grant=GRANT` (http→ws, https→wss); `binaryType === "arraybuffer"`; (b) JSON `console` → `browserActions.pushConsole(key, {level, text: args.join(" "), ts})`; `network` → `pushNetwork` with `durationMs`; (c) binary message → header decoded big-endian and `onFrame` callbacks receive `(bitmap, w, h)` — stub `createImageBitmap` on `globalThis` for jsdom; (d) `send({t:"nav",url})` serialises JSON; sends before open are queued and flushed on open; (e) close without an `error` message → status `reconnecting`, a **new** grant is minted, backoff 500 ms → 1 s → 2 s → cap 5 s (use fake timers); (f) `error` message → `error` state set, status `closed`, no reconnect; (g) `enabled=false` or unmount → socket closed, no reconnect.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement with `useEffect` + refs; keep the frame path allocation-light (decode straight from the `ArrayBuffer` slice).
- [ ] Step 4: Run → pass; `tsc` clean.
- [ ] Step 5: Commit `feat(web/browser): useCdpSocket`.

---

### Task 3: `ChromeViewport`

**Files:**
- Create: `web/src/components/Browser/ChromeViewport.tsx`
- Test: `web/src/components/Browser/ChromeViewport.test.tsx` (mock `useCdpSocket` to capture `send` calls and expose `onFrame`)

Design (intentional minimalism — the viewport is the page; chrome is only status): a `<canvas>` filling its container (`w-full h-full block`), `tabIndex=0`, `outline-none` with a subtle `ring-1` on `:focus-visible`; a centered thin spinner (reuse the `◌ animate-spin` glyph used by `AddressBar`) until the first frame; `status === "reconnecting"` shows a small top-right `reconnecting…` pill; `error` shows a centered message with an `Open externally ↗` button calling `window.open(url, "_blank", "noopener")`. No toolbar, no borders beyond the panel's own.

- [ ] Step 1: Write failing tests: (a) mounts a canvas with `tabIndex=0` and a spinner; after `onFrame(bitmap, 640, 480)` the spinner is gone and the canvas backing size is `640×480`; (b) `ResizeObserver` (stub) reporting `1000×600` at `devicePixelRatio=2` → `send({t:"resize", w:1000, h:600, dpr:2})`; (c) `pointerdown` at client `(10,20)` inside the canvas → `send({t:"mouse", kind:"down", x:10, y:20, button:"left", clickCount:1, modifiers:0})` and the canvas gets focus; `pointerup` → `up`; `pointermove` twice within 16 ms → one `move` (fake timers); `wheel` with `deltaY:120` → `wheel` message with deltas; (d) `keydown` `{key:"a", code:"KeyA"}` → `key down` then `key char` with `text:"a"`; `keydown` `Enter` → `down` with `text:"\r"`; `keyup` → `up`; modifiers bitmask `alt=1, ctrl=2, meta=4, shift=8` (CDP convention); (e) `contextmenu` default prevented; (f) `status="reconnecting"` pill visible; `error="chrome not found…"` shows message + Open externally.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement; draw with `ctx.drawImage(bitmap, 0, 0)` then `bitmap.close()`; map client coords to CSS pixels relative to the canvas rect (Chrome expects CSS px, not device px).
- [ ] Step 4: Run → pass; `tsc` clean.
- [ ] Step 5: Commit `feat(web/browser): ChromeViewport canvas + input`.

---

### Task 4: `BrowserPanel` integration

**Files:**
- Modify: `web/src/components/Browser/BrowserPanel.tsx`
- Test: `web/src/components/Browser/BrowserPanel.test.tsx` (extend; mock `ChromeViewport` to a `data-testid="chrome-viewport"` stub)

Behaviour: pick the surface from `s.mode`, falling back to `isPrivateHost(host of s.url) ? "local" : "chrome"` when `mode` is `null`. Local path unchanged (grant + iframe). Chrome path mounts `<ChromeViewport>`; **do not** set an iframe. Navigations while in Chrome mode go through the socket: `BrowserPanel` passes `s.url` to `ChromeViewport`, which sends `nav` whenever the prop changes; `onReload` sends `reload`; back/forward still mutate store history (address bar), and `ChromeViewport` sends `nav` for the resulting URL (simple and consistent with the store being authoritative). Collapsing unmounts the viewport (socket closes, screencast stops server-side); expanding remounts and reconnects.

- [ ] Step 1: Write failing tests: (a) state `mode:"chrome"` → `chrome-viewport` present, no `iframe`; (b) `mode:"local"` → iframe (existing test still passes); (c) `mode:null, url:"https://example.com/"` → chrome viewport; `mode:null, url:"http://localhost:3000/"` → iframe; (d) collapsed → neither mounted; (e) `useBrowserMessages` still registered only in local mode (postMessage console from the browse origin is pushed in local mode; in chrome mode it is ignored).
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement.
- [ ] Step 4: Run the whole Browser suite + `tsc` → pass.
- [ ] Step 5: Commit `feat(web/browser): render chrome mode viewport`.

## Verification for the part

- `cd web && bunx vitest run src/components/Browser src/lib/browserStore.test.ts && bunx tsc --noEmit -p .` green.
- Manual with a real server (Parts 01–05 done): `ocode serve`, open panel, type `example.com` → frames appear, clicks/typing work, console tab shows page logs; `localhost:<vite>` still iframe; collapse/expand resumes.
