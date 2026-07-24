# Web/Desktop Code Editor — Chat Context + Inline Diff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a live "active file + selection" chip above chat input that auto-attaches as `@path#Lstart-Lend` context on send, and render inline git-diff decorations (highlighted add/modify lines, read-only deleted-line blocks) in the code editor.

**Architecture:** Extend the `useEditorTabs` hook (from the companion core-editor plan) with selection tracking and a save-triggered refresh counter. A new pure-function diff parser turns the existing `getChangeDiff` unified-diff patch into structured hunks; `FileEditor` applies them as Monaco decorations (line highlights) and view zones (deleted-line blocks, which live outside the editable text model and are therefore not modifiable). A new `EditorContextChip` renders in `ChatInput`, which prepends the formatted ref to outgoing messages using the same `@ref` convention already used for file uploads.

**Tech Stack:** React + TypeScript, Monaco (`@monaco-editor/react` / `monaco-editor` decorations & view-zones APIs), existing `api.getChangeDiff` (`web/src/api/client.ts:440`).

## Global Constraints

- Depends on `docs/superpowers/plans/2026-07-24-web-editor-core.md` being implemented first — this plan's Task 2 modifies the `useEditorTabs` hook that plan creates.
- Deleted-line blocks must be read-only via being outside Monaco's editable text model (view zones), not via a read-only-region hack — matches spec decision in `2026-07-24-web-editor-context-diff-design.md` §2.
- Diff fetch failures / no-diff files must fail silently (no decorations, no inline error) — this is the common case, not an error state.
- No frontend test runner exists in this repo (`web/package.json` has no `test` script) — frontend changes are verified via `bun run typecheck`, `bun run build`, and manual browser QA (matches the pattern already used in the core-editor plan). The one pure-logic unit (`parseDiffPatch`) is still written test-first using a throwaway Node script (Step pattern below), not a committed test file, since there's no harness to keep it in.

---

## File Structure

- **Create:** `web/src/lib/parseDiffPatch.ts` — pure unified-diff parser.
- **Modify:** `web/src/hooks/useEditorTabs.ts` — add per-tab `selection`, `diffVersion`, and derived `activeEditorContext`.
- **Modify:** `web/src/components/Files/FileEditor.tsx` — add `onSelectionChange` prop (selection reporting) and `session`/`diffVersion` props (diff decorations).
- **Modify:** `web/src/index.css` — diff decoration CSS classes.
- **Create:** `web/src/components/Chat/EditorContextChip.tsx`.
- **Modify:** `web/src/components/Chat/ChatInput.tsx` — accept `activeEditorContext`, render chip, include in sent message.
- **Modify:** `web/src/pages/SessionPage.tsx` — pass new hook fields through to `FileEditor` and `ChatInput`.
- **Modify:** `web/src/App.tsx` — same wiring as `SessionPage.tsx`.

**Interfaces produced:**

```ts
// web/src/lib/parseDiffPatch.ts
export type DiffLineType = "context" | "add" | "del";
export interface DiffHunk {
  oldStart: number;
  newStart: number;
  lines: { type: DiffLineType; text: string }[];
}
export function parseDiffPatch(patch: string): DiffHunk[];

// web/src/hooks/useEditorTabs.ts additions
export interface EditorSelection { startLine: number; endLine: number }
export interface EditorTab {
  // ...existing fields from core plan...
  selection: EditorSelection | null;
  diffVersion: number;
}
export interface UseEditorTabsResult {
  // ...existing fields...
  activeEditorContext: { path: string; selection: EditorSelection | null } | null;
  handleSelectionChange: (id: string, selection: EditorSelection | null) => void;
}
```

---

## Task 1: `parseDiffPatch` utility

**Files:**
- Create: `web/src/lib/parseDiffPatch.ts`

**Interfaces:**
- Produces: `DiffLineType`, `DiffHunk`, `parseDiffPatch(patch: string): DiffHunk[]` (shapes above).

- [ ] **Step 1: Write the implementation**

`api.getChangeDiff` wraps `internal/changes/diff.go`'s `RenderDiff`, which shells out to `diff -u` and also returns plain placeholder strings with no `@@` marker for the no-op cases (`"(new file — no pre-session baseline)"`, `"(file unchanged since session start)"`, `"(file deleted since session start)"`). The parser must return `[]` for those without erroring.

Create `web/src/lib/parseDiffPatch.ts`:

```ts
export type DiffLineType = "context" | "add" | "del";

export interface DiffHunk {
  oldStart: number;
  newStart: number;
  lines: { type: DiffLineType; text: string }[];
}

const HUNK_HEADER = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

/**
 * Parses a `diff -u` unified-diff patch (as returned by api.getChangeDiff)
 * into structured hunks. Returns [] for non-diff placeholder strings
 * ("(new file — no pre-session baseline)", etc.) or empty input.
 */
export function parseDiffPatch(patch: string): DiffHunk[] {
  if (!patch || !patch.includes("@@")) return [];

  const hunks: DiffHunk[] = [];
  let current: DiffHunk | null = null;

  for (const line of patch.split("\n")) {
    const headerMatch = line.match(HUNK_HEADER);
    if (headerMatch) {
      current = {
        oldStart: parseInt(headerMatch[1], 10),
        newStart: parseInt(headerMatch[2], 10),
        lines: [],
      };
      hunks.push(current);
      continue;
    }
    if (!current) continue; // skip --- / +++ file headers before the first hunk

    if (line.startsWith("+")) {
      current.lines.push({ type: "add", text: line.slice(1) });
    } else if (line.startsWith("-")) {
      current.lines.push({ type: "del", text: line.slice(1) });
    } else if (line.startsWith(" ")) {
      current.lines.push({ type: "context", text: line.slice(1) });
    }
    // Lines that are none of the above (e.g. "\ No newline at end of file")
    // are ignored.
  }

  return hunks;
}
```

- [ ] **Step 2: Verify with a throwaway script**

Create `/tmp/verify-parse-diff.mjs` (not committed — this repo has no TS test runner, so this is a manual verification script):

```js
import { parseDiffPatch } from "/Users/james/www/ocode/web/src/lib/parseDiffPatch.ts";
```

Since that file is TypeScript and Node can't run it directly, instead verify via a quick `bunx tsx` invocation:

Run:
```bash
cd web && cat > /tmp/verify-parse-diff.ts <<'EOF'
import { parseDiffPatch } from "./src/lib/parseDiffPatch";

const patch = `--- a/foo.txt
+++ b/foo.txt
@@ -1,3 +1,3 @@
 line1
-line2
+line2-changed
 line3
`;

const hunks = parseDiffPatch(patch);
console.log(JSON.stringify(hunks, null, 2));
console.assert(hunks.length === 1, "expected 1 hunk");
console.assert(hunks[0].lines.length === 4, "expected 4 lines");
console.assert(hunks[0].lines[1].type === "del" && hunks[0].lines[1].text === "line2", "del line mismatch");
console.assert(hunks[0].lines[2].type === "add" && hunks[0].lines[2].text === "line2-changed", "add line mismatch");

console.assert(parseDiffPatch("(file unchanged since session start)").length === 0, "placeholder should yield no hunks");
console.assert(parseDiffPatch("").length === 0, "empty input should yield no hunks");
console.log("All assertions passed");
EOF
bunx tsx /tmp/verify-parse-diff.ts
rm /tmp/verify-parse-diff.ts
```

Expected output: the printed hunk JSON followed by `All assertions passed`, with no assertion failures printed to stderr.

- [ ] **Step 3: Type-check**

Run: `cd web && bun run typecheck`
Expected: no new errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/parseDiffPatch.ts
git commit -m "feat(web): add parseDiffPatch unified-diff parser"
```

---

## Task 2: Extend `useEditorTabs` with selection + diff refresh tracking

**Files:**
- Modify: `web/src/hooks/useEditorTabs.ts`

**Interfaces:**
- Consumes: existing `EditorTab`/`UseEditorTabsResult` from the core plan.
- Produces: `EditorSelection`, `EditorTab.selection`, `EditorTab.diffVersion`, `UseEditorTabsResult.activeEditorContext`, `UseEditorTabsResult.handleSelectionChange`.

- [ ] **Step 1: Add the new fields and derive `activeEditorContext`**

In `web/src/hooks/useEditorTabs.ts`, add the `EditorSelection` type and extend `EditorTab`:

```ts
export interface EditorSelection {
  startLine: number;
  endLine: number;
}
```

Add `selection: EditorSelection | null;` and `diffVersion: number;` to the `EditorTab` interface.

Add `activeEditorContext: { path: string; selection: EditorSelection | null } | null;` and `handleSelectionChange: (id: string, selection: EditorSelection | null) => void;` to `UseEditorTabsResult`.

- [ ] **Step 2: Initialize the new fields and implement the setter**

In `handleOpenFile`, update the pushed tab object to include the new fields:

```ts
      setEditorTabs((prev) => [
        ...prev,
        {
          id,
          path,
          content: data.content,
          originalContent: data.content,
          isDirty: false,
          selection: null,
          diffVersion: 0,
        },
      ]);
```

Add the setter function (near `handleEditorChange`):

```ts
  const handleSelectionChange = useCallback((id: string, selection: EditorSelection | null) => {
    setEditorTabs((prev) => prev.map((t) => (t.id === id ? { ...t, selection } : t)));
  }, []);
```

In `saveEditorTab`, bump `diffVersion` alongside clearing dirty (so `FileEditor` knows to refetch the diff after a save reflects new working-tree state):

```ts
        setEditorTabs((prev) =>
          prev.map((t) =>
            t.id === id ? { ...t, originalContent: t.content, isDirty: false, diffVersion: t.diffVersion + 1 } : t,
          ),
        );
```

- [ ] **Step 3: Derive `activeEditorContext` and return the new fields**

Add just before the `return` statement:

```ts
  const activeTabInfo = editorTabs.find((t) => t.id === activeTab);
  const activeEditorContext = activeTabInfo
    ? { path: activeTabInfo.path, selection: activeTabInfo.selection }
    : null;
```

Add `activeEditorContext` and `handleSelectionChange` to the returned object.

- [ ] **Step 4: Type-check**

Run: `cd web && bun run typecheck`
Expected: errors in `SessionPage.tsx`/`App.tsx`/`FileEditor.tsx` are expected at this point (they don't yet consume the new fields) — confirm the *only* new errors are "not all code paths" / unused-field style warnings in those three files, not inside `useEditorTabs.ts` itself. They'll be resolved by Tasks 3, 6, 7, 8.

- [ ] **Step 5: Commit**

```bash
git add web/src/hooks/useEditorTabs.ts
git commit -m "feat(web): track per-tab selection and diff-refresh version in useEditorTabs"
```

---

## Task 3: `FileEditor` selection reporting

**Files:**
- Modify: `web/src/components/Files/FileEditor.tsx`

**Interfaces:**
- Consumes: `EditorSelection` (Task 2).
- Produces: `FileEditorProps.onSelectionChange?: (selection: EditorSelection | null) => void`.

- [ ] **Step 1: Add the prop and wire it in `handleEditorMount`**

In `web/src/components/Files/FileEditor.tsx`, add the import and prop:

```ts
import type { EditorSelection } from "../../hooks/useEditorTabs";
```

Add to `FileEditorProps` (after `onChange`):

```ts
  onSelectionChange?: (selection: EditorSelection | null) => void;
```

Add to the destructured props (after `onChange`):

```ts
  onSelectionChange,
```

Since `handleEditorMount` is a `useCallback` with an empty dependency array (so it isn't recreated on every render, matching the existing pattern at `FileEditor.tsx:107`), read the latest `onSelectionChange` via a ref rather than adding it as a dependency:

```ts
  const onSelectionChangeRef = useRef(onSelectionChange);
  onSelectionChangeRef.current = onSelectionChange;
```

Add this inside `handleEditorMount`, after `monaco.editor.setTheme("ocode-dark");`:

```ts
    editor.onDidChangeCursorSelection((e) => {
      if (e.selection.isEmpty()) {
        onSelectionChangeRef.current?.(null);
      } else {
        onSelectionChangeRef.current?.({
          startLine: e.selection.startLineNumber,
          endLine: e.selection.endLineNumber,
        });
      }
    });
```

- [ ] **Step 2: Type-check**

Run: `cd web && bun run typecheck`
Expected: no new errors in `FileEditor.tsx` itself.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Files/FileEditor.tsx
git commit -m "feat(web): report editor selection changes from FileEditor"
```

---

## Task 4: Inline diff decorations in `FileEditor`

**Files:**
- Modify: `web/src/components/Files/FileEditor.tsx`
- Modify: `web/src/index.css`

**Interfaces:**
- Consumes: `parseDiffPatch` (Task 1), `api.getChangeDiff` (existing, `web/src/api/client.ts:440`).
- Produces: `FileEditorProps.session?: string`, `FileEditorProps.diffVersion?: number`.

- [ ] **Step 1: Add CSS classes for the decorations**

Append to `web/src/index.css`:

```css
.diff-line-added {
  background-color: rgba(34, 197, 94, 0.15);
}
.diff-line-modified {
  background-color: rgba(59, 130, 246, 0.15);
}
.diff-deleted-block {
  background-color: rgba(239, 68, 68, 0.12);
  border-left: 3px solid rgba(239, 68, 68, 0.6);
  font-family: 'JetBrains Mono', 'Fira Code', 'Cascadia Code', 'SF Mono', 'Menlo', monospace;
  font-size: 13px;
  white-space: pre;
  padding-left: 4px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  color: #fca5a5;
}
.diff-deleted-block button {
  background: transparent;
  border: none;
  color: #fca5a5;
  cursor: pointer;
  padding: 0 8px;
  opacity: 0.7;
}
.diff-deleted-block button:hover {
  opacity: 1;
}
```

- [ ] **Step 2: Add props and imports**

`FileEditor.tsx:5` already has `import { api } from "../../api/client";` — do not duplicate it. Add these new imports alongside it:

```ts
import { parseDiffPatch, type DiffHunk } from "../../lib/parseDiffPatch";
import { monaco } from "../../lib/monaco-setup";
```

`monaco-setup.ts` (`web/src/lib/monaco-setup.ts:40`) exports the shared `monaco` namespace directly (`export { loader, monaco };`), configured once for the whole app — use that import for `monaco.Range` below instead of touching `window` or threading a ref through `handleEditorMount`.

Add to `FileEditorProps`:

```ts
  session?: string;
  diffVersion?: number;
```

Add to the destructured props:

```ts
  session,
  diffVersion,
```

- [ ] **Step 3: Compute and apply decorations**

Add these refs near the top of the component body (with the other `useRef`s):

```ts
  const decorationIdsRef = useRef<string[]>([]);
  const viewZoneIdsRef = useRef<string[]>([]);
```

Add a helper function above the component (or inside, as a plain function — keep it outside the component so it isn't recreated every render):

```ts
function classifyHunkLines(hunk: DiffHunk): { addLines: number[]; delRuns: { afterNewLine: number; text: string }[] } {
  const addLines: number[] = [];
  const delRuns: { afterNewLine: number; text: string }[] = [];
  let newLine = hunk.newStart;
  let pendingDelText: string[] = [];
  let pendingDelAfterLine = newLine - 1;

  const flushDel = () => {
    if (pendingDelText.length > 0) {
      delRuns.push({ afterNewLine: pendingDelAfterLine, text: pendingDelText.join("\n") });
      pendingDelText = [];
    }
  };

  for (const line of hunk.lines) {
    if (line.type === "add") {
      addLines.push(newLine);
      newLine += 1;
      flushDel();
    } else if (line.type === "del") {
      pendingDelText.push(line.text);
    } else {
      flushDel();
      pendingDelAfterLine = newLine - 1;
      newLine += 1;
    }
  }
  flushDel();

  return { addLines, delRuns };
}
```

Add the diff-fetch-and-apply effect, after the existing "Update content when the file path changes" effect (`FileEditor.tsx:151-156`):

```ts
  useEffect(() => {
    if (!session) return;
    let cancelled = false;

    api
      .getChangeDiff(session, path)
      .then((res) => {
        if (cancelled) return;
        const hunks = parseDiffPatch(res.patch);
        const editorInst = editorRef.current;
        if (!editorInst) return;

        const decorations: import("monaco-editor").editor.IModelDeltaDecoration[] = [];
        const zoneDomNodes: { afterLineNumber: number; text: string }[] = [];

        for (const hunk of hunks) {
          const { addLines, delRuns } = classifyHunkLines(hunk);
          for (const lineNum of addLines) {
            decorations.push({
              range: new monaco.Range(lineNum, 1, lineNum, 1),
              options: { isWholeLine: true, className: "diff-line-added", linesDecorationsClassName: "diff-line-added" },
            });
          }
          for (const run of delRuns) {
            zoneDomNodes.push({ afterLineNumber: run.afterNewLine, text: run.text });
          }
        }

        decorationIdsRef.current = editorInst.deltaDecorations(decorationIdsRef.current, decorations);

        editorInst.changeViewZones((accessor) => {
          for (const id of viewZoneIdsRef.current) accessor.removeZone(id);
          viewZoneIdsRef.current = [];

          for (const zone of zoneDomNodes) {
            const domNode = document.createElement("div");
            domNode.className = "diff-deleted-block";
            const textSpan = document.createElement("span");
            textSpan.textContent = zone.text;
            const copyBtn = document.createElement("button");
            copyBtn.textContent = "Copy";
            copyBtn.onclick = () => navigator.clipboard.writeText(zone.text);
            domNode.appendChild(textSpan);
            domNode.appendChild(copyBtn);

            const lineCount = zone.text.split("\n").length;
            const zoneId = accessor.addZone({
              afterLineNumber: zone.afterNewLine,
              heightInLines: lineCount,
              domNode,
            });
            viewZoneIdsRef.current.push(zoneId);
          }
        });
      })
      .catch(() => {
        // Silent: no diff available for this file (common case) or no
        // active session. No inline error per spec — this is expected,
        // not a failure.
      });

    return () => {
      cancelled = true;
    };
  }, [session, path, diffVersion]);
```

- [ ] **Step 4: Type-check**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/Files/FileEditor.tsx web/src/index.css
git commit -m "feat(web): render inline diff decorations (add/modify highlight, read-only deleted blocks) in FileEditor"
```

---

## Task 5: `EditorContextChip` component

**Files:**
- Create: `web/src/components/Chat/EditorContextChip.tsx`

**Interfaces:**
- Consumes: `EditorSelection` (Task 2).
- Produces: `EditorContextChip` component, props `{ path: string; selection: EditorSelection | null }`.

- [ ] **Step 1: Implement the component**

Create `web/src/components/Chat/EditorContextChip.tsx`:

```tsx
import { FileCode } from "lucide-react";
import type { EditorSelection } from "../../hooks/useEditorTabs";

interface Props {
  path: string;
  selection: EditorSelection | null;
}

function fileNameFromPath(path: string): string {
  return path.split("/").pop() || path;
}

export default function EditorContextChip({ path, selection }: Props) {
  const label = selection
    ? `${fileNameFromPath(path)}:${selection.startLine}-${selection.endLine}`
    : fileNameFromPath(path);

  return (
    <span
      className="inline-flex items-center gap-1 text-xs bg-blue-600/20 text-blue-300 rounded px-2 py-0.5"
      title={path}
    >
      <FileCode className="w-3 h-3" />
      {label}
    </span>
  );
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && bun run typecheck`
Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Chat/EditorContextChip.tsx
git commit -m "feat(web): add EditorContextChip component"
```

---

## Task 6: Wire the chip into `ChatInput`

**Files:**
- Modify: `web/src/components/Chat/ChatInput.tsx`

**Interfaces:**
- Consumes: `EditorContextChip` (Task 5), `EditorSelection` (Task 2).
- Produces: `ChatInputProps.activeEditorContext?: { path: string; selection: EditorSelection | null } | null`.

- [ ] **Step 1: Add the prop**

In `web/src/components/Chat/ChatInput.tsx`, add the import:

```ts
import EditorContextChip from "./EditorContextChip";
import type { EditorSelection } from "../../hooks/useEditorTabs";
```

Extend `ChatInputProps` (`ChatInput.tsx:9-12`):

```ts
interface ChatInputProps {
  /** Called when a slash command is entered. Return true if handled (async). */
  onSlashCommand?: (command: string) => boolean | Promise<boolean>;
  activeEditorContext?: { path: string; selection: EditorSelection | null } | null;
}
```

Update the function signature (`ChatInput.tsx:14`):

```ts
export default function ChatInput({ onSlashCommand, activeEditorContext }: ChatInputProps) {
```

- [ ] **Step 2: Render the chip**

Add just before the existing `attachedFiles` chip block (`ChatInput.tsx:152`):

```tsx
      {activeEditorContext && (
        <div className="flex flex-wrap gap-1 mb-2">
          <EditorContextChip path={activeEditorContext.path} selection={activeEditorContext.selection} />
        </div>
      )}
```

- [ ] **Step 3: Include the context ref in the outgoing message**

Replace the message-building lines in `handleSend` (`ChatInput.tsx:99-102`):

```ts
    const refs = attachedFiles.map((n) => `@.ocode/uploads/${n}`).join(" ");
    const contextRef = activeEditorContext
      ? `@${activeEditorContext.path}${
          activeEditorContext.selection
            ? `#L${activeEditorContext.selection.startLine}-L${activeEditorContext.selection.endLine}`
            : ""
        }`
      : "";
    const allRefs = [refs, contextRef].filter(Boolean).join(" ");
    const finalMessage = allRefs ? `${allRefs} ${trimmed}` : trimmed;
    setAttachedFiles([]);
    sendMessage(finalMessage);
```

- [ ] **Step 4: Type-check**

Run: `cd web && bun run typecheck`
Expected: no new errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/Chat/ChatInput.tsx
git commit -m "feat(web): attach active editor file/selection as chat context on send"
```

---

## Task 7: Wire into `SessionPage.tsx`

**Files:**
- Modify: `web/src/pages/SessionPage.tsx`

- [ ] **Step 1: Pass the new hook fields through**

Add `activeEditorContext` and `handleSelectionChange` to the destructured result of `useEditorTabs()` (already called in this file per the core plan's Task 8).

Update the `FileEditor` usage (added in the core plan's Task 8) to add the new props:

```tsx
                <FileEditor
                  key={editorTab.id}
                  path={editorTab.path}
                  content={editorTab.content}
                  onChange={(value) => handleEditorChange(editorTab.id, value)}
                  onSelectionChange={(sel) => handleSelectionChange(editorTab.id, sel)}
                  session={state.sessionId ?? undefined}
                  diffVersion={editorTab.diffVersion}
                  readOnly={false}
                />
```

Update the `ChatInput` usage (`SessionPage.tsx:417`):

```tsx
              <ChatInput onSlashCommand={handleCommand} activeEditorContext={activeEditorContext} />
```

- [ ] **Step 2: Type-check**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [ ] **Step 3: Build**

Run: `cd web && bun run build`
Expected: build succeeds.

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/SessionPage.tsx
git commit -m "feat(web): wire editor context chip and diff decorations into SessionPage"
```

---

## Task 8: Wire into `App.tsx`

**Files:**
- Modify: `web/src/App.tsx`

- [ ] **Step 1: Pass the new hook fields through**

Add `activeEditorContext` and `handleSelectionChange` to the destructured result of `useEditorTabs()` (already called in this file per the core plan's Task 9).

Update the `FileEditor` usage (added in the core plan's Task 9):

```tsx
                  <FileEditor
                    key={editorTab.id}
                    path={editorTab.path}
                    content={editorTab.content}
                    onChange={(value) => handleEditorChange(editorTab.id, value)}
                    onSelectionChange={(sel) => handleSelectionChange(editorTab.id, sel)}
                    session={currentSessionId ?? undefined}
                    diffVersion={editorTab.diffVersion}
                    readOnly={false}
                  />
```

Update the `ChatInput` usage (`App.tsx:295`):

```tsx
                <ChatInput onSlashCommand={handleCommand} activeEditorContext={activeEditorContext} />
```

- [ ] **Step 2: Type-check**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [ ] **Step 3: Build**

Run: `cd web && bun run build`
Expected: build succeeds.

- [ ] **Step 4: Commit**

```bash
git add web/src/App.tsx
git commit -m "feat(web): wire editor context chip and diff decorations into HomeApp"
```

---

## Task 9: Manual verification

**Files:** none (verification only)

- [ ] **Step 1: Run the app**

Use the `run` skill to start the server, open the web UI in a browser, in a workspace with at least one file modified since session start (so `getChangeDiff` returns a real patch).

- [ ] **Step 2: Verify diff decorations**

Open the modified file in the editor. Confirm: added/modified lines show a highlighted background; deleted-line blocks appear at the correct positions with the removed text visible.

- [ ] **Step 3: Verify deleted blocks are read-only but copyable**

Click into a deleted-line block and attempt to type — confirm no effect on the editable buffer (view zones are outside the text model). Click its Copy button, paste elsewhere — confirm the pasted text matches the removed line(s) exactly.

- [ ] **Step 4: Verify diff refresh after save**

Edit the file, save it (Ctrl/Cmd+S from the core plan). Confirm the diff decorations update to reflect the new working-tree state (fewer/different highlighted lines).

- [ ] **Step 5: Verify no decorations / no errors on a clean file**

Open a file with no changes since session start. Confirm no decorations appear and no error is shown or logged.

- [ ] **Step 6: Verify the context chip**

With an editor tab active and no selection, confirm the chip above chat input shows just the filename. Select a range of lines in the editor, confirm the chip updates to `filename:startLine-endLine`. Switch to a different editor tab, confirm the chip updates to the new file with no selection. Switch to the Chat tab (no editor tab active), confirm the chip disappears.

- [ ] **Step 7: Verify the chip attaches to sent messages**

With the chip showing a file+selection, type a message and send it. Confirm (via network tab or server logs) the outgoing message text is prefixed with `@path#Lstart-Lend`, matching the format an uploaded-file `@ref` uses.

- [ ] **Step 8: Repeat steps 2-7 on the non-session route (`App.tsx`/`HomeApp`)**

Navigate to `/` and repeat the same checks to confirm both entry points behave identically.

No commit for this task — verification only.
