# Web/Desktop Code Editor Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Ctrl/Cmd+P file picker, Ctrl/Cmd+S save with dirty tracking, and middle-click-to-close-with-confirmation to the web/desktop code editor tabs.

**Architecture:** A new backend `PUT /api/files/content` write endpoint with a path-containment guard, plus a new shared frontend hook `useEditorTabs` (extracted from the duplicated inline state currently in `App.tsx` and `SessionPage.tsx`) that owns tab state, dirty tracking, save, and close-confirmation. New `FilePicker` and `ConfirmCloseDialog` components built on existing shadcn/cmdk primitives.

**Tech Stack:** Go (`net/http`, stdlib `os`/`path/filepath`), React + TypeScript, Monaco (`@monaco-editor/react`), cmdk (`web/src/components/ui/command.tsx`), shadcn `Dialog` (`web/src/components/ui/dialog.tsx`).

## Global Constraints

- Path containment: any new file-write code must reject paths that resolve outside the server's work directory (spec: `2026-07-24-web-editor-core-design.md` §3).
- No toast/notification library exists in this codebase — errors surface inline, matching the existing `SessionPage` error-state pattern.
- Ctrl/Cmd+P and Ctrl/Cmd+S must `preventDefault()` to suppress the browser's native print/save dialogs.
- Reuse existing primitives: `Command`/`CommandDialog` (`web/src/components/ui/command.tsx`) for the file picker, `Dialog`/`DialogFooter`/`Button` (pattern in `web/src/components/Changes/ChangesPanel.tsx:82-99`) for the close-confirmation dialog.

---

## File Structure

- **Modify:** `internal/server/handler_files.go` — add `HandleSaveFileContent` (PUT).
- **Modify:** `internal/server/server.go` — register `PUT /api/files/content` route.
- **Create:** `internal/server/handler_files_test.go` — tests for the new write handler.
- **Modify:** `web/src/api/client.ts` — add `api.saveFileContent(path, content)`.
- **Create:** `web/src/hooks/useEditorTabs.ts` — shared hook: tab state, dirty tracking, open/close/save. `App.tsx` and `SessionPage.tsx` currently duplicate identical `EditorTab` state/handlers (`App.tsx:96-129`, `SessionPage.tsx:55-90`); this task set adds non-trivial new logic (dirty flags, save, confirm-gated close) to both call sites, so extracting the shared hook now avoids tripling that logic across three files. Behavior for existing callers (`FileTree`, `GitPanel` passing `onOpenFile`) is unchanged.
- **Create:** `web/src/components/Files/FilePicker.tsx` — Ctrl/Cmd+P fuzzy file picker.
- **Create:** `web/src/components/Files/ConfirmCloseDialog.tsx` — Save/Discard/Cancel dialog.
- **Modify:** `web/src/hooks/useKeyboard.ts` — add `onFilePicker` (Ctrl/Cmd+P) and `onSave` (Ctrl/Cmd+S) handlers.
- **Modify:** `web/src/components/Layout/TopTabs.tsx` — middle-click close, dirty-dot indicator.
- **Modify:** `web/src/pages/SessionPage.tsx` — use `useEditorTabs`, wire `FilePicker`, wire new keyboard handlers.
- **Modify:** `web/src/App.tsx` (`HomeApp`) — same wiring as `SessionPage.tsx`.

**Interfaces produced by `useEditorTabs`** (consumed by every later task):

```ts
export interface EditorTab {
  id: string;
  path: string;
  content: string;
  originalContent: string;
  isDirty: boolean;
}

export interface UseEditorTabsResult {
  editorTabs: EditorTab[];
  activeTab: string;
  setActiveTab: (tab: string) => void;
  handleOpenFile: (path: string) => Promise<void>;
  handleEditorChange: (id: string, content: string) => void;
  requestCloseTab: (id: string) => void;
  saveEditorTab: (id: string) => Promise<void>;
  pendingClose: { id: string; path: string } | null;
  confirmSaveAndClose: () => Promise<void>;
  confirmDiscardAndClose: () => void;
  cancelClose: () => void;
  saveError: string | null;
}
```

---

## Task 1: Backend save endpoint

**Files:**
- Modify: `internal/server/handler_files.go`
- Modify: `internal/server/server.go:117` (add route next to existing GET)
- Test: `internal/server/handler_files_test.go` (new file)

**Interfaces:**
- Produces: `func (h *Handler) HandleSaveFileContent(w http.ResponseWriter, r *http.Request)`, request JSON `{"path": string, "content": string}`, response `{"path": string, "saved": true}`.

- [ ] **Step 1: Write the failing tests**

Create `internal/server/handler_files_test.go`:

```go
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func newFilesHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	tmpDir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(tmpDir)
	return h, tmpDir
}

func TestHandleSaveFileContentWritesFile(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	target := filepath.Join(tmpDir, "a.txt")
	if err := os.WriteFile(target, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{"path": target, "content": "changed\n"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	h.HandleSaveFileContent(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Path  string `json:"path"`
		Saved bool   `json:"saved"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Saved {
		t.Fatalf("expected saved=true, got %+v", resp)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "changed\n" {
		t.Fatalf("expected file content %q, got %q", "changed\n", string(got))
	}
}

func TestHandleSaveFileContentMissingPath(t *testing.T) {
	h, _ := newFilesHandler(t)
	body, _ := json.Marshal(map[string]string{"content": "x"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	h.HandleSaveFileContent(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSaveFileContentRejectsPathEscape(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	outside := filepath.Join(filepath.Dir(tmpDir), "outside.txt")

	body, _ := json.Marshal(map[string]string{"path": outside, "content": "x"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	h.HandleSaveFileContent(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path escape, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("expected outside.txt to not be created")
	}
}

func TestHandleSaveFileContentRejectsDotDotEscape(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	escaping := filepath.Join(tmpDir, "..", "escaped.txt")

	body, _ := json.Marshal(map[string]string{"path": escaping, "content": "x"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	h.HandleSaveFileContent(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for .. escape, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/... -run TestHandleSaveFileContent -v`
Expected: FAIL — `h.HandleSaveFileContent undefined`.

- [ ] **Step 3: Implement the handler**

In `internal/server/handler_files.go`, add below `HandleFileContent`:

```go
type saveFileContentRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (h *Handler) HandleSaveFileContent(w http.ResponseWriter, r *http.Request) {
	var req saveFileContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	root := h.workDir
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve work dir")
		return
	}
	absTarget, err := filepath.Abs(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		writeError(w, http.StatusBadRequest, "path is outside the workspace")
		return
	}

	if err := os.WriteFile(absTarget, []byte(req.Content), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":  req.Path,
		"saved": true,
	})
}
```

Add `"encoding/json"` to the existing import block in `handler_files.go` (it currently imports `net/http`, `os`, `path/filepath`, `sort`, `strings`, and the snapshot package — `encoding/json` is new, `strings` is already present).

In `internal/server/server.go`, add the route immediately after the existing `GET /api/files/content` line (`server.go:117`):

```go
	s.mux.HandleFunc("PUT /api/files/content", s.authMiddleware(s.handleSaveFileContent))
```

And add the thin wrapper in the same style as the other `handleFileContent`-style wrappers (near `handler_files.go`-backed wrappers in `server.go:328-334`):

```go
func (s *Server) handleSaveFileContent(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSaveFileContent(w, r)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSaveFileContent -v`
Expected: PASS (all 4 subtests).

- [ ] **Step 5: Run the full server test suite to check for regressions**

Run: `go test ./internal/server/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server/handler_files.go internal/server/server.go internal/server/handler_files_test.go
git commit -m "feat(server): add PUT /api/files/content save endpoint with path-containment guard"
```

---

## Task 2: Frontend API client method

**Files:**
- Modify: `web/src/api/client.ts`

**Interfaces:**
- Consumes: `fetchJSON<T>(path, init)` (existing, `client.ts:65`).
- Produces: `api.saveFileContent(path: string, content: string): Promise<{path: string; saved: boolean}>`.

- [ ] **Step 1: Add the method**

In `web/src/api/client.ts`, add next to the existing `getMonacoSettings` entry (near line 247), inside the `api` object:

```ts
  saveFileContent: (path: string, content: string) =>
    fetchJSON<{ path: string; saved: boolean }>("/api/files/content", {
      method: "PUT",
      body: JSON.stringify({ path, content }),
    }),
```

- [ ] **Step 2: Type-check**

Run: `cd web && bun run typecheck`
Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat(web): add saveFileContent API client method"
```

---

## Task 3: `useEditorTabs` hook

**Files:**
- Create: `web/src/hooks/useEditorTabs.ts`

**Interfaces:**
- Consumes: `api.saveFileContent` (Task 2), `apiPath`/`authHeaders` (existing, `web/src/api/client.ts`).
- Produces: `EditorTab`, `UseEditorTabsResult`, `useEditorTabs(): UseEditorTabsResult` (shapes given in File Structure above).

- [ ] **Step 1: Implement the hook**

Create `web/src/hooks/useEditorTabs.ts`:

```ts
import { useCallback, useRef, useState } from "react";
import { api, apiPath, authHeaders } from "../api/client";

export interface EditorTab {
  id: string;
  path: string;
  content: string;
  originalContent: string;
  isDirty: boolean;
}

export interface UseEditorTabsResult {
  editorTabs: EditorTab[];
  activeTab: string;
  setActiveTab: (tab: string) => void;
  handleOpenFile: (path: string) => Promise<void>;
  handleEditorChange: (id: string, content: string) => void;
  requestCloseTab: (id: string) => void;
  saveEditorTab: (id: string) => Promise<void>;
  pendingClose: { id: string; path: string } | null;
  confirmSaveAndClose: () => Promise<void>;
  confirmDiscardAndClose: () => void;
  cancelClose: () => void;
  saveError: string | null;
}

export function useEditorTabs(initialTab = "chat"): UseEditorTabsResult {
  const [editorTabs, setEditorTabs] = useState<EditorTab[]>([]);
  const [activeTab, setActiveTab] = useState(initialTab);
  const [pendingClose, setPendingClose] = useState<{ id: string; path: string } | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const openFileIdsRef = useRef<Set<string>>(new Set());

  const handleOpenFile = useCallback(async (path: string) => {
    const id = `editor-${path}`;
    if (openFileIdsRef.current.has(id)) {
      setActiveTab(id);
      return;
    }
    try {
      const res = await fetch(apiPath(`/api/files/content?path=${encodeURIComponent(path)}`), {
        headers: authHeaders(),
      });
      if (!res.ok) throw new Error("Failed to load file");
      const data = await res.json();
      openFileIdsRef.current.add(id);
      setEditorTabs((prev) => [
        ...prev,
        { id, path, content: data.content, originalContent: data.content, isDirty: false },
      ]);
      setActiveTab(id);
    } catch (err) {
      console.error("Failed to open file:", err);
    }
  }, []);

  const handleEditorChange = useCallback((id: string, content: string) => {
    setEditorTabs((prev) =>
      prev.map((t) => (t.id === id ? { ...t, content, isDirty: content !== t.originalContent } : t)),
    );
  }, []);

  const closeTabNow = useCallback((id: string) => {
    openFileIdsRef.current.delete(id);
    setEditorTabs((prev) => prev.filter((t) => t.id !== id));
    setActiveTab((prev) => (prev === id ? "files" : prev));
  }, []);

  const requestCloseTab = useCallback(
    (id: string) => {
      const tab = editorTabs.find((t) => t.id === id);
      if (!tab) return;
      if (tab.isDirty) {
        setPendingClose({ id, path: tab.path });
      } else {
        closeTabNow(id);
      }
    },
    [editorTabs, closeTabNow],
  );

  const saveEditorTab = useCallback(
    async (id: string) => {
      const tab = editorTabs.find((t) => t.id === id);
      if (!tab) return;
      try {
        await api.saveFileContent(tab.path, tab.content);
        setSaveError(null);
        setEditorTabs((prev) =>
          prev.map((t) => (t.id === id ? { ...t, originalContent: t.content, isDirty: false } : t)),
        );
      } catch (err) {
        setSaveError(err instanceof Error ? err.message : "Failed to save file");
        throw err;
      }
    },
    [editorTabs],
  );

  const confirmSaveAndClose = useCallback(async () => {
    if (!pendingClose) return;
    try {
      await saveEditorTab(pendingClose.id);
      closeTabNow(pendingClose.id);
      setPendingClose(null);
    } catch {
      // saveError is already set by saveEditorTab; keep the dialog open so
      // the user can retry or fall back to Discard/Cancel.
    }
  }, [pendingClose, saveEditorTab, closeTabNow]);

  const confirmDiscardAndClose = useCallback(() => {
    if (!pendingClose) return;
    closeTabNow(pendingClose.id);
    setPendingClose(null);
    setSaveError(null);
  }, [pendingClose, closeTabNow]);

  const cancelClose = useCallback(() => {
    setPendingClose(null);
    setSaveError(null);
  }, []);

  return {
    editorTabs,
    activeTab,
    setActiveTab,
    handleOpenFile,
    handleEditorChange,
    requestCloseTab,
    saveEditorTab,
    pendingClose,
    confirmSaveAndClose,
    confirmDiscardAndClose,
    cancelClose,
    saveError,
  };
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && bun run typecheck`
Expected: no new errors (hook is not yet consumed, so no unused-export warnings should occur — it's a named export).

- [ ] **Step 3: Commit**

```bash
git add web/src/hooks/useEditorTabs.ts
git commit -m "feat(web): add useEditorTabs hook with dirty tracking and save"
```

---

## Task 4: `ConfirmCloseDialog` component

**Files:**
- Create: `web/src/components/Files/ConfirmCloseDialog.tsx`

**Interfaces:**
- Consumes: `Dialog`, `DialogContent`, `DialogHeader`, `DialogTitle`, `DialogFooter` (`web/src/components/ui/dialog.tsx`), `Button` (`web/src/components/ui/button.tsx`).
- Produces: `ConfirmCloseDialog` React component, props `{ path: string; open: boolean; error: string | null; onSave: () => void; onDiscard: () => void; onCancel: () => void }`.

- [ ] **Step 1: Implement the component**

Create `web/src/components/Files/ConfirmCloseDialog.tsx`:

```tsx
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";

interface Props {
  path: string;
  open: boolean;
  error: string | null;
  onSave: () => void;
  onDiscard: () => void;
  onCancel: () => void;
}

export default function ConfirmCloseDialog({ path, open, error, onSave, onDiscard, onCancel }: Props) {
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Save changes to {path}?</DialogTitle>
        </DialogHeader>
        {error && <p className="text-sm text-red-400">{error}</p>}
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={onDiscard}>
            Discard
          </Button>
          <Button onClick={onSave}>Save</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && bun run typecheck`
Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Files/ConfirmCloseDialog.tsx
git commit -m "feat(web): add ConfirmCloseDialog for unsaved editor tab close"
```

---

## Task 5: `FilePicker` component (Ctrl/Cmd+P)

**Files:**
- Create: `web/src/components/Files/FilePicker.tsx`

**Interfaces:**
- Consumes: `Command`, `CommandDialog`, `CommandEmpty`, `CommandGroup`, `CommandInput`, `CommandItem`, `CommandList` (`web/src/components/ui/command.tsx`), `apiPath`/`authHeaders` (`web/src/api/client.ts`), backend `GET /api/files/tree` (existing, returns `FileNode[]` — `internal/server/handler_files.go:13-18`: `{name, path, is_dir, children?}`).
- Produces: `FilePicker` React component, props `{ open: boolean; onClose: () => void; onOpenFile: (path: string) => void }`.

- [ ] **Step 1: Implement the component**

Create `web/src/components/Files/FilePicker.tsx`:

```tsx
import { useEffect, useState } from "react";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "../ui/command";
import { apiPath, authHeaders } from "../../api/client";

interface FileNode {
  name: string;
  path: string;
  is_dir: boolean;
  children?: FileNode[];
}

interface Props {
  open: boolean;
  onClose: () => void;
  onOpenFile: (path: string) => void;
}

function flattenFiles(nodes: FileNode[]): string[] {
  const out: string[] = [];
  for (const n of nodes) {
    if (n.is_dir) {
      if (n.children) out.push(...flattenFiles(n.children));
    } else {
      out.push(n.path);
    }
  }
  return out;
}

export default function FilePicker({ open, onClose, onOpenFile }: Props) {
  const [files, setFiles] = useState<string[]>([]);

  useEffect(() => {
    if (!open) return;
    fetch(apiPath("/api/files/tree"), { headers: authHeaders() })
      .then((res) => res.json())
      .then((tree: FileNode[]) => setFiles(flattenFiles(tree)))
      .catch((err) => console.error("Failed to load file tree:", err));
  }, [open]);

  return (
    <CommandDialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <CommandInput placeholder="Search files..." />
      <CommandList>
        <CommandEmpty>No files found</CommandEmpty>
        <CommandGroup heading="Files">
          {files.map((path) => (
            <CommandItem
              key={path}
              value={path}
              onSelect={() => {
                onOpenFile(path);
                onClose();
              }}
            >
              <span className="font-mono text-sm">{path}</span>
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && bun run typecheck`
Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Files/FilePicker.tsx
git commit -m "feat(web): add Ctrl/Cmd+P file picker"
```

---

## Task 6: Keyboard shortcut wiring

**Files:**
- Modify: `web/src/hooks/useKeyboard.ts`

**Interfaces:**
- Produces: `ShortcutHandlers.onFilePicker?: () => void`, `ShortcutHandlers.onSave?: () => void`.

- [ ] **Step 1: Add the handlers**

Replace the full contents of `web/src/hooks/useKeyboard.ts`:

```ts
import { useEffect, useRef } from "react";

interface ShortcutHandlers {
  onNewSession?: () => void;
  onCommandPalette?: () => void;
  onFilePicker?: () => void;
  onSave?: () => void;
  onEscape?: () => void;
}

export function useKeyboard(handlers: ShortcutHandlers) {
  const ref = useRef(handlers);
  ref.current = handlers;

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        ref.current.onCommandPalette?.();
      }
      if (e.key === "p" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        ref.current.onFilePicker?.();
      }
      if (e.key === "s" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        ref.current.onSave?.();
      }
      if (e.key === "n" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        ref.current.onNewSession?.();
      }
      if (e.key === "Escape") {
        ref.current.onEscape?.();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && bun run typecheck`
Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/hooks/useKeyboard.ts
git commit -m "feat(web): add Ctrl/Cmd+P and Ctrl/Cmd+S keyboard shortcuts"
```

---

## Task 7: Middle-click close + dirty-dot in `TopTabs`

**Files:**
- Modify: `web/src/components/Layout/TopTabs.tsx`

**Interfaces:**
- Consumes: none new (adds optional `isDirty` to `EditorTabInfo`).
- Produces: `EditorTabInfo.isDirty?: boolean`; close button now fires on middle-click as well as left-click.

- [ ] **Step 1: Update `EditorTabInfo` and the close button**

In `web/src/components/Layout/TopTabs.tsx`, change the interface (line 3-6):

```ts
export interface EditorTabInfo {
  id: string;
  path: string;
  isDirty?: boolean;
}
```

Replace the editor-tab rendering block (lines 73-103) with:

```tsx
            {editorTabs.map((et) => {
              const isActive = activeTab === et.id;
              return (
                <div
                  key={et.id}
                  className="flex items-center gap-1 shrink-0"
                  onMouseDown={(e) => {
                    if (e.button === 1) {
                      e.preventDefault();
                      e.stopPropagation();
                      onEditorTabClose(et.id);
                    }
                  }}
                >
                  <button
                    onClick={() => onTabChange(et.id)}
                    className={`flex items-center gap-1.5 px-2 py-1.5 rounded-md text-xs font-medium transition-colors whitespace-nowrap ${
                      isActive
                        ? "bg-blue-600/20 text-blue-400"
                        : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800"
                    }`}
                    title={et.path}
                  >
                    <FileCode className="w-3.5 h-3.5" />
                    <span className="max-w-[120px] truncate">{fileNameFromPath(et.path)}</span>
                    {et.isDirty && (
                      <span className="w-1.5 h-1.5 rounded-full bg-zinc-300 shrink-0" title="Unsaved changes" />
                    )}
                  </button>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      onEditorTabClose(et.id);
                    }}
                    className="p-0.5 rounded hover:bg-zinc-700 text-zinc-500 hover:text-zinc-300 transition-colors"
                    title="Close"
                  >
                    <X className="w-3 h-3" />
                  </button>
                </div>
              );
            })}
```

(Only the interface and this one block change; everything else in the file is untouched.)

- [ ] **Step 2: Type-check**

Run: `cd web && bun run typecheck`
Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Layout/TopTabs.tsx
git commit -m "feat(web): middle-click closes editor tabs, add dirty indicator"
```

---

## Task 8: Wire everything into `SessionPage.tsx`

**Files:**
- Modify: `web/src/pages/SessionPage.tsx`

- [ ] **Step 1: Replace the inline editor-tab state with `useEditorTabs`**

Remove the inline `EditorTab` interface, `editorTabs`/`openFileIdsRef` state, and `handleOpenFile`/`handleCloseEditorTab` callbacks (`SessionPage.tsx:55-90`). Replace with:

```ts
  const {
    editorTabs,
    activeTab,
    setActiveTab,
    handleOpenFile,
    handleEditorChange,
    requestCloseTab,
    pendingClose,
    confirmSaveAndClose,
    confirmDiscardAndClose,
    cancelClose,
    saveError,
    saveEditorTab,
  } = useEditorTabs("chat");
```

Remove the old `const [activeTab, setActiveTab] = useState("chat");` (now supplied by the hook) and add the import:

```ts
import { useEditorTabs } from "../hooks/useEditorTabs";
import FilePicker from "../components/Files/FilePicker";
import ConfirmCloseDialog from "../components/Files/ConfirmCloseDialog";
```

- [ ] **Step 2: Wire keyboard shortcuts**

Add near other top-level state in the component body:

```ts
  const [filePickerOpen, setFilePickerOpen] = useState(false);

  useKeyboard({
    onFilePicker: () => setFilePickerOpen(true),
    onSave: () => {
      if (activeTab.startsWith("editor-")) {
        const tab = editorTabs.find((t) => t.id === activeTab);
        if (tab) saveEditorTab(tab.id);
      }
    },
    onEscape: () => setFilePickerOpen(false),
  });
```

Add the import: `import { useKeyboard } from "../hooks/useKeyboard";`

- [ ] **Step 3: Update `TopTabs`, `FileEditor`, and add the new dialogs to JSX**

Update the `TopTabs` usage (`SessionPage.tsx:391-396`):

```tsx
      <TopTabs
        activeTab={activeTab}
        onTabChange={setActiveTab}
        editorTabs={editorTabs.map((t) => ({ id: t.id, path: t.path, isDirty: t.isDirty }))}
        onEditorTabClose={requestCloseTab}
      />
```

Update the `FileEditor` usage (`SessionPage.tsx:405-411`) to pass `onChange`:

```tsx
                <FileEditor
                  key={editorTab.id}
                  path={editorTab.path}
                  content={editorTab.content}
                  onChange={(value) => handleEditorChange(editorTab.id, value)}
                  readOnly={false}
                />
```

Add just before the closing `</div>` of the component's returned JSX (after the existing dialogs, following the same pattern as other top-level dialogs in this file):

```tsx
      <FilePicker
        open={filePickerOpen}
        onClose={() => setFilePickerOpen(false)}
        onOpenFile={handleOpenFile}
      />
      <ConfirmCloseDialog
        path={pendingClose?.path ?? ""}
        open={pendingClose !== null}
        error={saveError}
        onSave={confirmSaveAndClose}
        onDiscard={confirmDiscardAndClose}
        onCancel={cancelClose}
      />
```

- [ ] **Step 4: Type-check**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [ ] **Step 5: Build**

Run: `cd web && bun run build`
Expected: build succeeds.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/SessionPage.tsx
git commit -m "feat(web): wire Ctrl+P/Ctrl+S/dirty-close into SessionPage editor tabs"
```

---

## Task 9: Wire everything into `App.tsx`

**Files:**
- Modify: `web/src/App.tsx`

- [ ] **Step 1: Replace the inline editor-tab state in `HomeApp` with `useEditorTabs`**

Remove the inline `EditorTab` interface, `editorTabs`/`openFileIdsRef` state, and `handleOpenFile`/`handleCloseEditorTab` callbacks (`App.tsx:96-129`). Replace with the same hook call as Task 8:

```ts
  const {
    editorTabs,
    activeTab,
    setActiveTab,
    handleOpenFile,
    handleEditorChange,
    requestCloseTab,
    pendingClose,
    confirmSaveAndClose,
    confirmDiscardAndClose,
    cancelClose,
    saveError,
    saveEditorTab,
  } = useEditorTabs("chat");
```

Remove the old `const [activeTab, setActiveTab] = useState("chat");` (`App.tsx:94`) and add imports:

```ts
import { useEditorTabs } from "./hooks/useEditorTabs";
import FilePicker from "./components/Files/FilePicker";
import ConfirmCloseDialog from "./components/Files/ConfirmCloseDialog";
```

- [ ] **Step 2: Extend the existing `useKeyboard` call**

Update the existing call (`App.tsx:181-185`):

```ts
  const [filePickerOpen, setFilePickerOpen] = useState(false);

  useKeyboard({
    onNewSession: () => dispatch({ type: "RESET" }),
    onCommandPalette: () => setCmdOpen(true),
    onFilePicker: () => setFilePickerOpen(true),
    onSave: () => {
      if (activeTab.startsWith("editor-")) {
        const tab = editorTabs.find((t) => t.id === activeTab);
        if (tab) saveEditorTab(tab.id);
      }
    },
    onEscape: () => {
      setCmdOpen(false);
      setFilePickerOpen(false);
    },
  });
```

- [ ] **Step 3: Update `TopTabs`, `FileEditor`, and add the new dialogs**

Update the `TopTabs` usage (`App.tsx:264-269`):

```tsx
          <TopTabs
            activeTab={activeTab}
            onTabChange={setActiveTab}
            editorTabs={editorTabs.map((t) => ({ id: t.id, path: t.path, isDirty: t.isDirty }))}
            onEditorTabClose={requestCloseTab}
          />
```

Update the `FileEditor` usage (`App.tsx:281-288`):

```tsx
                  <FileEditor
                    key={editorTab.id}
                    path={editorTab.path}
                    content={editorTab.content}
                    onChange={(value) => handleEditorChange(editorTab.id, value)}
                    readOnly={false}
                  />
```

Add alongside the existing dialogs near the end of `HomeApp`'s JSX (after `ModelDialog`, `App.tsx:336-340`):

```tsx
      <FilePicker
        open={filePickerOpen}
        onClose={() => setFilePickerOpen(false)}
        onOpenFile={handleOpenFile}
      />
      <ConfirmCloseDialog
        path={pendingClose?.path ?? ""}
        open={pendingClose !== null}
        error={saveError}
        onSave={confirmSaveAndClose}
        onDiscard={confirmDiscardAndClose}
        onCancel={cancelClose}
      />
```

- [ ] **Step 4: Type-check**

Run: `cd web && bun run typecheck`
Expected: no errors.

- [ ] **Step 5: Build**

Run: `cd web && bun run build`
Expected: build succeeds.

- [ ] **Step 6: Commit**

```bash
git add web/src/App.tsx
git commit -m "feat(web): wire Ctrl+P/Ctrl+S/dirty-close into HomeApp editor tabs"
```

---

## Task 10: Manual verification

**Files:** none (verification only)

- [ ] **Step 1: Run the app**

Use the `run` skill (or `go run ./cmd/ocode web` / project's existing dev command) to start the server, then open the web UI in a browser.

- [ ] **Step 2: Verify Ctrl+P**

Press Ctrl/Cmd+P → picker opens with a searchable file list. Type part of a filename → list narrows. Press Enter on a result → file opens as an editor tab.

- [ ] **Step 3: Verify save + dirty tracking**

Edit the open file's content → a dirty-dot appears next to the tab label. Press Ctrl/Cmd+S → dirty-dot disappears. Reload the file from disk (e.g. `cat` it in a terminal) to confirm the on-disk content matches.

- [ ] **Step 4: Verify middle-click close**

Edit a file again (dirty). Middle-click its tab → `ConfirmCloseDialog` appears. Test all three buttons across three separate dirty tabs: Save closes and persists; Discard closes without persisting (verify via `cat`); Cancel leaves the tab open and dirty.

- [ ] **Step 5: Verify clean-tab close is unaffected**

Open a file, don't edit it, middle-click (and separately, left-click the X) → closes immediately, no dialog.

- [ ] **Step 6: Repeat steps 2-5 on the non-session route (`App.tsx`/`HomeApp`)**

Navigate to `/` (not `/session/:id`) and repeat the same checks to confirm both entry points work identically.

No commit for this task — verification only. If any step fails, fix the relevant task above and re-verify before proceeding to Spec 2's plan.
