# Web/Desktop Agents Tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a top-level "Agents" tab to the web/desktop UI (React app in
`web/`, embedded unchanged by `cmd/ocode-desktop` via Wails) that lists every
subagent run for the current session and lets the user open a full-screen
transcript view for any run — parity with the TUI's dedicated agents tab
(`internal/tui/tabs.go`, `internal/tui/detail_view.go`).

**Architecture:** No backend/API changes. `useAgentRuns(sessionId)`
(`web/src/hooks/useAgentRuns.ts`) already streams the full run tree
(including nested sub-agent children) over SSE. The existing recursive
tree renderer in `AgentPreview.tsx` (the small chat-rail preview) is
extracted into a shared `RunNode` component so the new full-height tab and
the rail render runs identically. A new `AgentsPanel` component owns
list/detail navigation state for the tab; clicking a run's name in the rail
also opens that run's detail view in the new tab.

**Tech Stack:** React 18 + TypeScript, Tailwind CSS, lucide-react icons,
Vite. Testing: Vitest + React Testing Library (new — first test infra in
`web/`).

## Global Constraints

- Session-scoped only — no cross-session/project-wide run history (spec:
  "Scope").
- No new backend/HTTP/SSE endpoints — reuse `useAgentRuns` /
  `connectAgentRunsSSE` as-is (spec: "Out of scope").
- Do not change the rail's existing chevron/row inline-toggle-to-expand
  behavior (spec: "Out of scope"). Only the run **name** gets new click
  behavior.
- Pin exact major versions for any new npm dependency — no `latest`/unpinned
  ranges (global instruction: Dependency Version Pinning).
- `web/` currently has **zero** test infrastructure (confirmed: no vitest,
  no test script, no `.test.tsx` files). This plan adds it from scratch.
- Desktop (`cmd/ocode-desktop`) needs no separate changes — it embeds the
  `web/` build unchanged.

---

### Task 1: Add Vitest + React Testing Library test infrastructure

**Files:**
- Modify: `web/package.json`
- Create: `web/vitest.config.ts`
- Create: `web/src/test/setup.ts`
- Test: `web/src/test/smoke.test.tsx`

**Interfaces:**
- Produces: `npm run test` (in `web/`) runs Vitest once (`vitest run`); test
  files matching `**/*.test.tsx` under `web/src` run in a `jsdom`
  environment with `@testing-library/jest-dom` matchers globally available.

- [ ] **Step 1: Install new devDependencies**

Run in `web/`:
```bash
cd web && npm install --save-dev vitest@^3 @testing-library/react@^16 @testing-library/jest-dom@^6 jsdom@^25
```

- [ ] **Step 2: Add the `test` script to `web/package.json`**

Edit the `scripts` block:
```json
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "preview": "vite preview",
    "test": "vitest run"
  },
```

- [ ] **Step 3: Create `web/vitest.config.ts`**

```typescript
import path from "path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    globals: true,
  },
});
```

- [ ] **Step 4: Create `web/src/test/setup.ts`**

```typescript
import "@testing-library/jest-dom/vitest";
```

- [ ] **Step 5: Write a smoke test to verify the setup works**

Create `web/src/test/smoke.test.tsx`:
```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("test infrastructure smoke test", () => {
  it("renders a component and asserts on the DOM", () => {
    render(<div>hello agents tab</div>);
    expect(screen.getByText("hello agents tab")).toBeInTheDocument();
  });
});
```

- [ ] **Step 6: Run the test suite and verify it passes**

Run: `cd web && npm run test`
Expected: 1 test file, 1 test, PASS.

- [ ] **Step 7: Commit**

```bash
git add web/package.json web/package-lock.json web/vitest.config.ts web/src/test/setup.ts web/src/test/smoke.test.tsx
git commit -m "test: add Vitest + React Testing Library infra to web/"
```

---

### Task 2: Extract `RunNode` into a shared component

**Files:**
- Create: `web/src/components/Agents/RunNode.tsx`
- Modify: `web/src/components/Chat/AgentPreview.tsx`
- Test: `web/src/components/Agents/RunNode.test.tsx`

**Interfaces:**
- Consumes: `AgentRun`, `AgentRunMessage` types from `web/src/api/types.ts`
  (existing, unchanged: `AgentRun.id/name/status/result/err/model/
  startedAt/endedAt/messages/children`, `AgentRunMessage.role/content/
  toolCalls/toolCallId/reasoningContent`).
- Produces (for Task 3/4): from `web/src/components/Agents/RunNode.tsx`:
  - `export function statusStyles(status: string): { dot: string; bar: string; text: string }`
  - `export function elapsed(startedAt: string, endedAt?: string): string`
  - `export function childSummary(children: AgentRun[]): string`
  - `export default function RunNode(props: { run: AgentRun; depth: number; onOpenDetail?: (runId: string) => void }): JSX.Element`

This is a pure move of existing code out of `AgentPreview.tsx` (no visual
changes) plus one addition: an optional `onOpenDetail` prop that, when
given, renders the run's name as its own clickable element (stopping event
propagation so it doesn't also toggle the row's expand/collapse).

- [ ] **Step 1: Create `web/src/components/Agents/RunNode.tsx`**

Move `statusStyles`, `elapsed`, `childSummary`, `messageLine`, `RunNodeProps`,
and `RunNode` out of `AgentPreview.tsx` verbatim, export the helpers, and add
the `onOpenDetail` prop:

```tsx
import { useState } from "react";
import { ChevronRight } from "lucide-react";
import type { AgentRun, AgentRunMessage } from "../../api/types";

// statusStyles maps a run status to its dot, glow and accent-bar treatment.
export function statusStyles(status: string): { dot: string; bar: string; text: string } {
  switch (status) {
    case "running":
      return {
        dot: "bg-amber-400 shadow-[0_0_0_3px_rgba(251,191,36,0.18)] animate-pulse",
        bar: "bg-amber-400/70",
        text: "text-amber-300/90",
      };
    case "done":
      return { dot: "bg-emerald-400", bar: "bg-emerald-500/40", text: "text-emerald-300/80" };
    case "failed":
      return { dot: "bg-red-400", bar: "bg-red-500/50", text: "text-red-300/90" };
    default:
      return { dot: "bg-zinc-500", bar: "bg-zinc-700", text: "text-zinc-400" };
  }
}

// elapsed renders a compact run duration like "1.4s" or "2m" from ISO stamps.
export function elapsed(startedAt: string, endedAt?: string): string {
  const start = Date.parse(startedAt);
  if (Number.isNaN(start)) return "";
  const end = endedAt ? Date.parse(endedAt) : Date.now();
  const ms = Math.max(0, end - start);
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  return `${m}m${Math.round(s % 60)}s`;
}

// childSummary mirrors the TUI's "N sub · M running" badge.
export function childSummary(children: AgentRun[]): string {
  if (children.length === 0) return "";
  let running = 0;
  let done = 0;
  let failed = 0;
  for (const c of children) {
    if (c.status === "running") running++;
    else if (c.status === "done") done++;
    else if (c.status === "failed") failed++;
  }
  const parts = [`${children.length} sub`];
  if (running) parts.push(`${running}·run`);
  if (done) parts.push(`${done}·ok`);
  if (failed) parts.push(`${failed}·err`);
  return parts.join(" ");
}

const roleChip: Record<string, string> = {
  user: "bg-blue-500/15 text-blue-300",
  assistant: "bg-emerald-500/15 text-emerald-300",
  tool: "bg-amber-500/15 text-amber-300",
  system: "bg-zinc-700/40 text-zinc-400",
};

// messageLine renders one transcript entry as a chip-prefixed row.
function messageLine(msg: AgentRunMessage, i: number) {
  const label = msg.role === "user" ? "task" : msg.role === "assistant" ? "agent" : msg.role;
  return (
    <div key={i} className="space-y-1 text-xs leading-relaxed">
      <div className="flex gap-2">
        <span
          className={`mt-px h-fit shrink-0 rounded px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide ${
            roleChip[msg.role] ?? "bg-zinc-700/40 text-zinc-400"
          }`}
        >
          {label}
        </span>
        <div className="min-w-0 flex-1 text-zinc-300">
          {msg.content && <span className="whitespace-pre-wrap break-words">{msg.content}</span>}
          {msg.toolCalls?.map((tc, j) => (
            <div key={j} className="font-mono text-[11px] text-zinc-400">
              <span className="text-zinc-500">→</span> {tc.name}
              {tc.arguments ? (
                <span className="text-zinc-600">({tc.arguments.slice(0, 120)})</span>
              ) : (
                <span className="text-zinc-600">()</span>
              )}
            </div>
          ))}
        </div>
      </div>
      {msg.reasoningContent && (
        <div className="ml-[1.75rem] rounded-md border border-zinc-700/60 bg-zinc-900/50 px-2 py-1.5">
          <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-zinc-500">
            Thinking
          </div>
          <pre className="whitespace-pre-wrap break-words font-mono text-[11px] text-zinc-400">
            {msg.reasoningContent}
          </pre>
        </div>
      )}
    </div>
  );
}

interface RunNodeProps {
  run: AgentRun;
  depth: number;
  // onOpenDetail, when given, makes the run's name its own clickable
  // element that opens a full transcript view instead of just toggling
  // this row's inline expand/collapse.
  onOpenDetail?: (runId: string) => void;
}

// RunNode is one run row, individually expandable to reveal its messages and
// nested sub-agent runs (recursively).
export default function RunNode({ run, depth, onOpenDetail }: RunNodeProps) {
  const [open, setOpen] = useState(true);
  const summary = childSummary(run.children);
  const hasResult = Boolean(run.result?.trim());
  const hasDetail = run.messages.length > 0 || run.children.length > 0 || hasResult;
  const s = statusStyles(run.status);
  const dur = elapsed(run.startedAt, run.endedAt);

  return (
    <div className={depth > 0 ? "border-l border-zinc-800/80 pl-2.5" : ""}>
      <button
        onClick={() => setOpen((v) => !v)}
        className="group relative flex w-full items-center gap-2 overflow-hidden rounded-md py-1 pl-2.5 pr-2 text-left text-sm transition-colors hover:bg-zinc-800/70"
      >
        {/* status accent bar */}
        <span className={`absolute left-0 top-1 bottom-1 w-0.5 rounded-full ${s.bar}`} />

        <ChevronRight
          className={`h-3.5 w-3.5 shrink-0 text-zinc-600 transition-transform ${
            hasDetail ? "group-hover:text-zinc-400" : "opacity-0"
          } ${open ? "rotate-90" : ""}`}
        />
        <span className={`h-2 w-2 shrink-0 rounded-full ${s.dot}`} />
        {onOpenDetail ? (
          <span
            role="button"
            onClick={(e) => {
              e.stopPropagation();
              onOpenDetail(run.id);
            }}
            className="shrink-0 truncate font-medium text-zinc-100 hover:text-blue-400 hover:underline"
          >
            {run.name}
          </span>
        ) : (
          <span className="shrink-0 truncate font-medium text-zinc-100">{run.name}</span>
        )}
        {run.model && (
          <span className="shrink-0 truncate font-mono text-[11px] text-zinc-500">{run.model}</span>
        )}
        <span className={`shrink-0 text-[11px] ${s.text}`}>{run.status}</span>

        <span className="ml-auto flex shrink-0 items-center gap-2">
          {summary && (
            <span className="rounded-full bg-zinc-800 px-2 py-0.5 font-mono text-[10px] text-zinc-400 ring-1 ring-inset ring-zinc-700/60">
              {summary}
            </span>
          )}
          {dur && <span className="font-mono text-[10px] tabular-nums text-zinc-600">{dur}</span>}
        </span>
      </button>

      {open && hasDetail && (
        <div className="ml-[1.15rem] mt-1 mb-2 space-y-2 border-l border-zinc-800/60 pl-3">
          {run.err && (
            <div className="rounded-md bg-red-950/40 px-2 py-1 text-xs text-red-300 ring-1 ring-inset ring-red-900/40">
              {run.err}
            </div>
          )}
          {run.messages.length > 0 && (
            <div className="space-y-1.5 rounded-md bg-zinc-900/70 p-2 ring-1 ring-inset ring-zinc-800/80">
              {run.messages.map((m, i) => messageLine(m, i))}
            </div>
          )}
          {hasResult && (
            <div className="rounded-md bg-emerald-950/25 px-2 py-1.5 ring-1 ring-inset ring-emerald-900/30">
              <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-emerald-300/75">
                Result
              </div>
              <pre className="whitespace-pre-wrap break-words font-mono text-[11px] text-emerald-100/90">
                {run.result}
              </pre>
            </div>
          )}
          {run.children.map((child) => (
            <RunNode key={child.id} run={child} depth={depth + 1} />
          ))}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Replace `AgentPreview.tsx` to import from the new module**

Replace the full contents of `web/src/components/Chat/AgentPreview.tsx`:

```tsx
import { Bot } from "lucide-react";
import { useChatState } from "../../stores/chatStore";
import { useAgentRuns } from "../../hooks/useAgentRuns";
import RunNode from "../Agents/RunNode";

// AgentPreview is the live "agent preview" rail above the chat input: top-level
// agent runs, each clickable to expand its messages and nested sub-agents
// inline. Renders nothing when no runs are active.
export default function AgentPreview() {
  const { sessionId } = useChatState();
  const runs = useAgentRuns(sessionId);

  if (runs.length === 0) return null;

  const running = runs.filter((r) => r.status === "running").length;

  return (
    <div className="max-h-52 shrink-0 overflow-y-auto border-t border-zinc-800 bg-gradient-to-b from-zinc-900 to-zinc-950/80 px-3 py-2">
      <div className="mb-1.5 flex items-center gap-2">
        <Bot className="h-3.5 w-3.5 text-blue-400" />
        <span className="text-[11px] font-semibold uppercase tracking-wider text-zinc-400">
          Agents
        </span>
        <span className="rounded-full bg-zinc-800 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400 ring-1 ring-inset ring-zinc-700/60">
          {runs.length}
        </span>
        {running > 0 && (
          <span className="flex items-center gap-1 text-[10px] text-amber-300/80">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-amber-400" />
            {running} running
          </span>
        )}
      </div>
      <div className="space-y-0.5">
        {runs.map((run) => (
          <RunNode key={run.id} run={run} depth={0} />
        ))}
      </div>
    </div>
  );
}
```

(`onOpenDetail` wiring for the rail is added in Task 3 — this step only
verifies the extraction is behavior-preserving.)

- [ ] **Step 3: Write a test for `RunNode`**

Create `web/src/components/Agents/RunNode.test.tsx`:
```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import RunNode from "./RunNode";
import type { AgentRun } from "../../api/types";

const baseRun: AgentRun = {
  id: "run-1",
  name: "code-reviewer",
  status: "done",
  model: "sonnet-5",
  startedAt: "2026-08-06T10:00:00.000Z",
  endedAt: "2026-08-06T10:00:02.000Z",
  inputTokens: 10,
  outputTokens: 20,
  messages: [{ role: "assistant", content: "looks good" }],
  children: [],
};

describe("RunNode", () => {
  it("renders the run name, status, and toggles messages on row click", () => {
    render(<RunNode run={baseRun} depth={0} />);
    expect(screen.getByText("code-reviewer")).toBeInTheDocument();
    expect(screen.getByText("done")).toBeInTheDocument();
    expect(screen.getByText("looks good")).toBeInTheDocument();

    fireEvent.click(screen.getByText("code-reviewer"));
    expect(screen.queryByText("looks good")).not.toBeInTheDocument();
  });

  it("renders nested child runs recursively", () => {
    const withChild: AgentRun = {
      ...baseRun,
      children: [{ ...baseRun, id: "run-2", name: "sub-agent" }],
    };
    render(<RunNode run={withChild} depth={0} />);
    expect(screen.getByText("sub-agent")).toBeInTheDocument();
  });

  it("calls onOpenDetail when the name is clicked, without toggling the row", () => {
    const onOpenDetail = vi.fn();
    render(<RunNode run={baseRun} depth={0} onOpenDetail={onOpenDetail} />);

    fireEvent.click(screen.getByText("code-reviewer"));

    expect(onOpenDetail).toHaveBeenCalledWith("run-1");
    // row click was not triggered, so messages stay visible (still open)
    expect(screen.getByText("looks good")).toBeInTheDocument();
  });
});
```

- [ ] **Step 4: Run the tests and verify they pass**

Run: `cd web && npm run test`
Expected: all tests PASS (smoke test + 3 new `RunNode` tests).

- [ ] **Step 5: Verify the TypeScript build still passes**

Run: `cd web && npm run build`
Expected: no type errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/Agents/RunNode.tsx web/src/components/Agents/RunNode.test.tsx web/src/components/Chat/AgentPreview.tsx
git commit -m "refactor: extract RunNode from AgentPreview into shared Agents component"
```

---

### Task 3: Wire the rail's "click name to open detail" behavior

**Files:**
- Modify: `web/src/components/Chat/AgentPreview.tsx`
- Test: `web/src/components/Chat/AgentPreview.test.tsx`

**Interfaces:**
- Consumes: `RunNode`'s `onOpenDetail` prop from Task 2.
- Produces: `AgentPreview` now accepts `{ onOpenDetail?: (runId: string) => void }` as props, forwarded to each top-level `RunNode`. `App.tsx` (Task 5) passes the real handler; until then it's optional so `AgentPreview` still works standalone.

- [ ] **Step 1: Add the `onOpenDetail` prop to `AgentPreview`**

Edit `web/src/components/Chat/AgentPreview.tsx`:
```tsx
interface AgentPreviewProps {
  onOpenDetail?: (runId: string) => void;
}

export default function AgentPreview({ onOpenDetail }: AgentPreviewProps) {
```
and change the render loop:
```tsx
        {runs.map((run) => (
          <RunNode key={run.id} run={run} depth={0} onOpenDetail={onOpenDetail} />
        ))}
```

- [ ] **Step 2: Write a test mocking `useAgentRuns` to verify the click wiring**

Create `web/src/components/Chat/AgentPreview.test.tsx`:
```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import AgentPreview from "./AgentPreview";
import type { AgentRun } from "../../api/types";

vi.mock("../../stores/chatStore", () => ({
  useChatState: () => ({ sessionId: "session-1" }),
}));

const runningRun: AgentRun = {
  id: "run-1",
  name: "code-reviewer",
  status: "running",
  startedAt: "2026-08-06T10:00:00.000Z",
  inputTokens: 0,
  outputTokens: 0,
  messages: [],
  children: [],
};

const mockUseAgentRuns = vi.fn<[], AgentRun[]>();
vi.mock("../../hooks/useAgentRuns", () => ({
  useAgentRuns: () => mockUseAgentRuns(),
}));

describe("AgentPreview", () => {
  it("calls onOpenDetail with the run id when a run name is clicked", () => {
    mockUseAgentRuns.mockReturnValue([runningRun]);
    const onOpenDetail = vi.fn();
    render(<AgentPreview onOpenDetail={onOpenDetail} />);

    fireEvent.click(screen.getByText("code-reviewer"));

    expect(onOpenDetail).toHaveBeenCalledWith("run-1");
  });

  it("renders nothing when there are no runs", () => {
    mockUseAgentRuns.mockReturnValue([]);
    const { container } = render(<AgentPreview onOpenDetail={vi.fn()} />);

    expect(container).toBeEmptyDOMElement();
  });
});
```

- [ ] **Step 3: Run the tests and verify they pass**

Run: `cd web && npm run test`
Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/Chat/AgentPreview.tsx web/src/components/Chat/AgentPreview.test.tsx
git commit -m "feat: rail run-name click opens full agent detail view"
```

---

### Task 4: Build `AgentsPanel` (list + detail states)

**Files:**
- Create: `web/src/components/Agents/AgentsPanel.tsx`
- Test: `web/src/components/Agents/AgentsPanel.test.tsx`

**Interfaces:**
- Consumes: `useAgentRuns(sessionId)` (`web/src/hooks/useAgentRuns.ts`,
  unchanged), `RunNode`, `statusStyles`, `elapsed`, `childSummary` from
  `web/src/components/Agents/RunNode.tsx` (Task 2).
- Produces (for Task 5): `export default function AgentsPanel(props: { session: string | undefined; selectedRunId: string | null; onSelectRun: (runId: string | null) => void }): JSX.Element`

`AgentsPanel` is stateless with respect to selection — the selected run id
is owned by `App.tsx` (Task 5) so the rail (Task 3) and the tab share one
source of truth, matching the spec's "State" section.

- [ ] **Step 1: Write `AgentsPanel.tsx`**

```tsx
import { ArrowLeft, Bot } from "lucide-react";
import { useAgentRuns } from "../../hooks/useAgentRuns";
import RunNode, { childSummary, elapsed, statusStyles } from "./RunNode";
import type { AgentRun } from "../../api/types";

interface AgentsPanelProps {
  session: string | undefined;
  selectedRunId: string | null;
  onSelectRun: (runId: string | null) => void;
}

function findRun(runs: AgentRun[], id: string): AgentRun | undefined {
  for (const r of runs) {
    if (r.id === id) return r;
    const child = findRun(r.children, id);
    if (child) return child;
  }
  return undefined;
}

function AgentListRow({ run, onOpen }: { run: AgentRun; onOpen: () => void }) {
  const s = statusStyles(run.status);
  const summary = childSummary(run.children);
  const dur = elapsed(run.startedAt, run.endedAt);

  return (
    <button
      onClick={onOpen}
      className="group relative flex w-full items-center gap-2 overflow-hidden rounded-md py-2 pl-3 pr-2 text-left text-sm transition-colors hover:bg-zinc-800/70"
    >
      <span className={`absolute left-0 top-1 bottom-1 w-0.5 rounded-full ${s.bar}`} />
      <span className={`h-2 w-2 shrink-0 rounded-full ${s.dot}`} />
      <span className="shrink-0 truncate font-medium text-zinc-100">{run.name}</span>
      {run.model && (
        <span className="shrink-0 truncate font-mono text-[11px] text-zinc-500">{run.model}</span>
      )}
      <span className={`shrink-0 text-[11px] ${s.text}`}>{run.status}</span>
      <span className="ml-auto flex shrink-0 items-center gap-2">
        {summary && (
          <span className="rounded-full bg-zinc-800 px-2 py-0.5 font-mono text-[10px] text-zinc-400 ring-1 ring-inset ring-zinc-700/60">
            {summary}
          </span>
        )}
        {dur && <span className="font-mono text-[10px] tabular-nums text-zinc-600">{dur}</span>}
      </span>
    </button>
  );
}

export default function AgentsPanel({ session, selectedRunId, onSelectRun }: AgentsPanelProps) {
  const runs = useAgentRuns(session ?? null);
  const selected = selectedRunId ? findRun(runs, selectedRunId) : undefined;

  if (selected) {
    const s = statusStyles(selected.status);
    const dur = elapsed(selected.startedAt, selected.endedAt);
    return (
      <div className="flex h-full flex-col overflow-hidden">
        <div className="flex shrink-0 items-center gap-2 border-b border-zinc-800 px-4 py-3">
          <button
            onClick={() => onSelectRun(null)}
            className="flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
          >
            <ArrowLeft className="h-4 w-4" />
            Agents
          </button>
          <span className={`h-2 w-2 shrink-0 rounded-full ${s.dot}`} />
          <span className="font-medium text-zinc-100">{selected.name}</span>
          {selected.model && (
            <span className="font-mono text-[11px] text-zinc-500">{selected.model}</span>
          )}
          <span className={`text-[11px] ${s.text}`}>{selected.status}</span>
          {dur && <span className="font-mono text-[10px] tabular-nums text-zinc-600">{dur}</span>}
        </div>
        <div className="flex-1 overflow-y-auto p-3">
          <RunNode run={selected} depth={0} />
        </div>
      </div>
    );
  }

  if (runs.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 text-zinc-500">
        <Bot className="h-8 w-8" />
        <p className="text-sm">No agent runs yet in this session.</p>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto p-3">
      <div className="space-y-1">
        {runs.map((run) => (
          <AgentListRow key={run.id} run={run} onOpen={() => onSelectRun(run.id)} />
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Write tests for `AgentsPanel`**

Create `web/src/components/Agents/AgentsPanel.test.tsx`:
```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import AgentsPanel from "./AgentsPanel";
import type { AgentRun } from "../../api/types";

const runs: AgentRun[] = [
  {
    id: "run-1",
    name: "code-reviewer",
    status: "done",
    startedAt: "2026-08-06T10:00:00.000Z",
    endedAt: "2026-08-06T10:00:02.000Z",
    inputTokens: 0,
    outputTokens: 0,
    messages: [{ role: "assistant", content: "reviewed" }],
    children: [
      {
        id: "run-2",
        name: "sub-linter",
        status: "done",
        startedAt: "2026-08-06T10:00:00.000Z",
        endedAt: "2026-08-06T10:00:01.000Z",
        inputTokens: 0,
        outputTokens: 0,
        messages: [],
        children: [],
      },
    ],
  },
];

const mockUseAgentRuns = vi.fn<[], AgentRun[]>();
vi.mock("../../hooks/useAgentRuns", () => ({
  useAgentRuns: () => mockUseAgentRuns(),
}));

describe("AgentsPanel", () => {
  it("shows the empty state when there are no runs", () => {
    mockUseAgentRuns.mockReturnValue([]);
    render(<AgentsPanel session="session-1" selectedRunId={null} onSelectRun={vi.fn()} />);

    expect(screen.getByText("No agent runs yet in this session.")).toBeInTheDocument();
  });

  it("renders the run list and opens a run's detail view on click", () => {
    mockUseAgentRuns.mockReturnValue(runs);
    const onSelectRun = vi.fn();
    render(<AgentsPanel session="session-1" selectedRunId={null} onSelectRun={onSelectRun} />);

    expect(screen.getByText("code-reviewer")).toBeInTheDocument();
    fireEvent.click(screen.getByText("code-reviewer"));
    expect(onSelectRun).toHaveBeenCalledWith("run-1");
  });

  it("renders the selected run's full tree, including nested children, in detail view", () => {
    mockUseAgentRuns.mockReturnValue(runs);
    render(<AgentsPanel session="session-1" selectedRunId="run-1" onSelectRun={vi.fn()} />);

    expect(screen.getByText("reviewed")).toBeInTheDocument();
    expect(screen.getByText("sub-linter")).toBeInTheDocument();
  });

  it("calls onSelectRun(null) when the back button is clicked", () => {
    mockUseAgentRuns.mockReturnValue(runs);
    const onSelectRun = vi.fn();
    render(<AgentsPanel session="session-1" selectedRunId="run-1" onSelectRun={onSelectRun} />);

    fireEvent.click(screen.getByText("Agents"));
    expect(onSelectRun).toHaveBeenCalledWith(null);
  });
});
```

- [ ] **Step 3: Run the tests and verify they pass**

Run: `cd web && npm run test`
Expected: all tests PASS.

- [ ] **Step 4: Verify the TypeScript build still passes**

Run: `cd web && npm run build`
Expected: no type errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/Agents/AgentsPanel.tsx web/src/components/Agents/AgentsPanel.test.tsx
git commit -m "feat: add AgentsPanel with run list and full-screen detail view"
```

---

### Task 5: Wire the Agents tab into `TopTabs` and `App.tsx`

**Files:**
- Modify: `web/src/components/Layout/TopTabs.tsx`
- Modify: `web/src/App.tsx`

**Interfaces:**
- Consumes: `AgentsPanel` (Task 4), `AgentPreview`'s `onOpenDetail` prop
  (Task 3).
- Produces: `activeTab === "agents"` renders `AgentsPanel`; App owns
  `selectedAgentRunId: string | null` state, passed to `AgentsPanel` and to
  `AgentPreview` as `onOpenDetail`.

- [ ] **Step 1: Add the "agents" entry to `TopTabs.tsx`**

Edit `web/src/components/Layout/TopTabs.tsx` imports and `mainTabs`:
```tsx
import { MessageSquare, FolderGit2, GitBranch, ScrollText, Paperclip, Activity, FileCode, X, CalendarClock, History, Bot } from "lucide-react";
```
```tsx
const mainTabs = [
  { id: "chat", label: "Chat", icon: MessageSquare },
  { id: "agents", label: "Agents", icon: Bot },
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "changes", label: "Changes", icon: History },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "status", label: "Status", icon: Activity },
  { id: "logs", label: "Logs", icon: ScrollText },
  { id: "cron", label: "Cron", icon: CalendarClock },
  { id: "assets", label: "Assets", icon: Paperclip },
];
```

- [ ] **Step 2: Add `selectedAgentRunId` state and wire it in `App.tsx`**

In `web/src/App.tsx`, add the import:
```tsx
import AgentsPanel from "./components/Agents/AgentsPanel";
```

Add state next to the other `useState` declarations in `HomeApp` (near
`cmdOpen`):
```tsx
  const [selectedAgentRunId, setSelectedAgentRunId] = useState<string | null>(null);
```

Add a handler that both opens the tab and selects the run, used by the
rail:
```tsx
  const openAgentDetail = (runId: string) => {
    setSelectedAgentRunId(runId);
    setActiveTab("agents");
  };
```

Update the `AgentPreview` usage (inside the `activeTab === "chat"` block)
to pass the handler:
```tsx
                <AgentPreview onOpenDetail={openAgentDetail} />
```

Add the new tab's content alongside the other `activeTab === "..."` blocks:
```tsx
            {activeTab === "agents" && (
              <AgentsPanel
                session={currentSessionId ?? undefined}
                selectedRunId={selectedAgentRunId}
                onSelectRun={setSelectedAgentRunId}
              />
            )}
```

- [ ] **Step 3: Reset `selectedAgentRunId` when the session changes**

Add an effect near the other session-driven effects in `HomeApp` (after the
`useEditorTabs` destructure):
```tsx
  useEffect(() => {
    setSelectedAgentRunId(null);
  }, [currentSessionId]);
```

- [ ] **Step 4: Verify the TypeScript build passes**

Run: `cd web && npm run build`
Expected: no type errors.

- [ ] **Step 5: Verify the full test suite still passes**

Run: `cd web && npm run test`
Expected: all tests PASS (no regressions from the wiring change).

- [ ] **Step 6: Manual verification**

Use the `run` skill to start the web dev server (`cd web && npm run dev`,
proxying `/api` to a running `ocode` server per `web/vite.config.ts`).
Trigger at least one subagent run in a session (e.g. dispatch any task that
spawns a subagent), then in the browser:
1. Confirm a new "Agents" tab appears in the top nav.
2. Click it — confirm the run list renders full-height (no `max-h-52` cap).
3. Click a run — confirm the detail view shows its full transcript
   (messages, tool calls, thinking, nested sub-agent runs) with a working
   back button.
4. Go to the Chat tab, click a run's **name** in the rail — confirm it
   switches to the Agents tab with that run's detail already open.
5. Confirm the rail's existing chevron/row click still toggles inline
   expand/collapse as before (no regression).

- [ ] **Step 7: Commit**

```bash
git add web/src/components/Layout/TopTabs.tsx web/src/App.tsx
git commit -m "feat: add Agents tab to top nav, wire rail-to-tab navigation"
```

---

## Self-Review Notes

- **Spec coverage:** Shared rendering (Task 2) ✓, new tab list+detail
  (Task 4/5) ✓, rail → tab entry point (Task 3/5) ✓, state/reset-on-session
  (Task 5 Step 3) ✓, desktop (no separate work — noted in Global
  Constraints) ✓, testing (Task 1 infra + tests throughout) ✓.
- **Placeholder scan:** none — every step has runnable code or an exact
  command.
- **Type consistency:** `AgentRun`/`AgentRunMessage` used identically to
  `web/src/api/types.ts` across all tasks; `RunNode`'s `onOpenDetail`
  signature (`(runId: string) => void`) matches `AgentPreviewProps` (Task 3)
  and `App.tsx`'s `openAgentDetail` (Task 5); `AgentsPanel`'s
  `onSelectRun: (runId: string | null) => void` matches `App.tsx`'s
  `setSelectedAgentRunId` (same type, `useState<string | null>`).
