---
type: Plan
title: Shared Confirm Dialog Implementation Plan
description: Shared Confirm Dialog implementation plan (tasks 1–8 done, task 9 pending)
tags:
  - plan
  - web
  - confirm-dialog
  - destructive-actions
timestamp: 2026-09-25T16:48:10Z
---
# Shared Confirm Dialog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every destructive action in the ocode web/desktop SPA is gated by one shared rendered confirm dialog that actually works in the desktop webview and reports a failed action instead of closing as if it succeeded.

**Architecture:** A new `common/ConfirmDialog` owns dialog STRUCTURE (focus policy, destructive button, pending state, inline failure display); each call site owns its own wording. `onConfirm` must reject on failure — the dialog catches, renders the reason in a `role="alert"`, and stays open. The two destructive `projectStore` mutations re-throw after logging so a rejection can reach the dialog.

**Tech Stack:** React 19, TypeScript, Radix Dialog via the repo's `components/ui/dialog` wrapper, shadcn/ui `Button`, vitest + @testing-library/react (jsdom), Vite.

**Spec:** `docs/superpowers/specs/2026-09-25-shared-confirm-dialog-design.md`

**Status note (read before executing):** ALL NINE tasks are implemented and verified in the working tree; every step is checked `[x]`, so this plan is the review record rather than unstarted work. Tasks 1–8 were implemented as written. Task 9 was implemented by a concurrent session in the same working tree (one deviation, recorded in the task) and then independently verified. Verified state: full web suite 268 test files / 2214 tests passing, `npx tsgo --noEmit` clean, `npx vite build` clean. Nothing is committed yet at the time of writing.

## Global Constraints

- **Never `window.confirm` or `window.prompt`.** Native JS dialogs are unsupported in the Wails/WKWebView desktop webview, where `confirm()` SILENTLY RETURNS FALSE — an action guarded by it is unreachable in the desktop app while looking correct in a browser.
- **`onConfirm` must REJECT on failure.** The dialog renders the message in a `role="alert"` and stays open. A confirm that closes on a failed write is a lie.
- **The SAFE action carries `data-dialog-default-action`;** the destructive button is `variant="destructive"` and is never the default focus, so Enter on a freshly opened confirm cancels instead of destroying.
- **The shared component owns structure only.** Wording lives at each call site.
- **Copy must be honest per action.** Do not write "this cannot be undone" for a list-only removal whose files survive; keep it for genuinely on-disk destructive actions.
- **Success does not auto-close.** The call site closes the dialog (`await onConfirm(); setPendingNull`), so a failure can leave it standing.
- **No new dependencies.**
- This repo's working tree routinely carries concurrent WIP from other sessions: `git add` ONLY the paths listed in each task.

## Review Focus

Five input classes the spec implies that are the most likely to bite a real user. Each line's test lives in the task that owns the code.

1. **The action is rejected by the server** (404, remote SSH host down, 409) — the dialog must stay open showing the reason, never close as though the write landed. → Tasks 1, 2, and one site test in each of Tasks 3–7.
2. **Enter pressed on a freshly opened confirm** — must cancel, not destroy; the safe action is the default-focused one. → Task 1.
3. **The collapsed-rail and mobile-drawer surfaces** — the rail render branch has no dialogs of its own, so a confirm mounted in only one branch is a confirm that never appears. → Task 3.
4. **Two projects sharing the same path on different hosts** — the confirm must disambiguate with `host:path`, or the user cannot tell which entry is about to go. → Task 3.
5. **A group holding 0 or 1 project** — plural agreement and the "nothing to move" case; a confirm that says "Its 2 projects will move" for an empty group is wrong. → Task 4.

## File Structure

- Create: `web/src/components/common/ConfirmDialog.tsx` — the one shared confirm; structure only.
- Create: `web/src/components/common/ConfirmDialog.test.tsx` — its contract.
- Modify: `web/src/stores/projectStore.tsx` — `removeProject`, `deleteGroup` re-throw after logging.
- Modify: `web/src/stores/projectStore.test.tsx` — the re-throw contract.
- Modify: `web/src/components/Layout/ProjectSidebar.tsx` — `pendingRemove` / `pendingGroupDelete` state + `renderConfirms()`.
- Modify: `web/src/components/Layout/ProjectSidebar.test.tsx` — project and group confirms.
- Modify: `web/src/components/Cron/CronPanel.tsx` — delete-job confirm.
- Create: `web/src/components/Cron/CronPanel.confirm.test.tsx`
- Modify: `web/src/components/Logs/LogPanel.tsx` — clear-logs confirm.
- Modify: `web/src/components/Logs/LogPanel.test.tsx` — clear confirmation.
- Modify: `web/src/components/Settings/ProfilesManager.tsx` — delete-profile and remove-key confirms.
- Create: `web/src/components/Settings/ProfilesManager.test.tsx`
- Modify: `CHANGES.md` — one dated entry.
- Modify: `skills/ocode-web/SKILL.md` — rules 37 and 38.
- Existing spec: `docs/superpowers/specs/2026-09-25-shared-confirm-dialog-design.md`

---

### Task 1: The shared `ConfirmDialog`

**Files:**
- Create: `web/src/components/common/ConfirmDialog.tsx`
- Test: `web/src/components/common/ConfirmDialog.test.tsx`

**Interfaces:**
- Consumes: `components/ui/dialog` (`Dialog`, `DialogContent`, `DialogFooter`, `DialogHeader`, `DialogTitle`), `components/ui/button` (`Button`).
- Produces: default export `ConfirmDialog(props: ConfirmDialogProps)`, where
  `ConfirmDialogProps = { open: boolean; title: string; description?: ReactNode; confirmLabel: string; pendingLabel?: string; onConfirm: () => Promise<void>; onCancel: () => void }`.
  Every later task imports this default export.

- [x] **Step 1: Write the failing test**

`web/src/components/common/ConfirmDialog.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import ConfirmDialog from "./ConfirmDialog";

function renderConfirm(overrides: Partial<React.ComponentProps<typeof ConfirmDialog>> = {}) {
  const onConfirm = vi.fn().mockResolvedValue(undefined);
  const onCancel = vi.fn();
  const utils = render(
    <ConfirmDialog
      open
      title="Delete the thing?"
      description="This removes it from the list."
      confirmLabel="Delete"
      onConfirm={onConfirm}
      onCancel={onCancel}
      {...overrides}
    />,
  );
  return { ...utils, onConfirm, onCancel };
}

function dialog() {
  return screen.getByRole("dialog");
}
function confirmButton(name = "Delete") {
  return within(dialog()).getByRole("button", { name });
}

describe("ConfirmDialog", () => {
  it("does not run the action on Cancel", () => {
    const { onConfirm, onCancel } = renderConfirm();
    fireEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));
    expect(onConfirm).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalled();
  });

  it("does not run the action when the dialog is dismissed with Escape", () => {
    const { onConfirm, onCancel } = renderConfirm();
    fireEvent.keyDown(dialog(), { key: "Escape" });
    expect(onConfirm).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalled();
  });

  it("runs the action exactly once on confirm", async () => {
    const { onConfirm, onCancel } = renderConfirm();
    fireEvent.click(confirmButton());
    await waitFor(() => expect(onConfirm).toHaveBeenCalledTimes(1));
    expect(onCancel).not.toHaveBeenCalled();
  });

  it("shows the error and stays open when the action rejects", async () => {
    const onConfirm = vi.fn().mockRejectedValue(new Error("server said 404"));
    renderConfirm({ onConfirm });
    fireEvent.click(confirmButton());
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("server said 404"));
    expect(dialog()).toBeDefined();
    expect(confirmButton()).not.toBeDisabled();
  });

  it("clears a previous error when the action is retried", async () => {
    const onConfirm = vi.fn()
      .mockRejectedValueOnce(new Error("server said 404"))
      .mockResolvedValueOnce(undefined);
    renderConfirm({ onConfirm });
    fireEvent.click(confirmButton());
    await waitFor(() => expect(screen.getByRole("alert")).toBeDefined());
    fireEvent.click(confirmButton());
    await waitFor(() => expect(onConfirm).toHaveBeenCalledTimes(2));
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("shows a pending label and disables both buttons while the action runs", async () => {
    let release!: () => void;
    const onConfirm = vi.fn(() => new Promise<void>((resolve) => { release = resolve; }));
    renderConfirm({ onConfirm, confirmLabel: "Delete", pendingLabel: "Deleting…" });
    fireEvent.click(confirmButton());
    await waitFor(() => expect(screen.getByText("Deleting…")).toBeDefined());
    expect(within(dialog()).getByRole("button", { name: "Cancel" })).toBeDisabled();
    fireEvent.click(screen.getByText("Deleting…"));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    release();
    await waitFor(() => expect(screen.queryByText("Deleting…")).toBeNull());
  });

  it("focuses Cancel, the safe action, not the destructive button", () => {
    renderConfirm();
    expect(within(dialog()).getByRole("button", { name: "Cancel" })).toHaveFocus();
  });

  it("renders nothing when closed", () => {
    renderConfirm({ open: false });
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/common/ConfirmDialog.test.tsx`
Expected: FAIL — `Failed to resolve import "./ConfirmDialog"`.

- [x] **Step 3: Write the minimal implementation**

`web/src/components/common/ConfirmDialog.tsx`:

```tsx
import { useEffect, useState, type ReactNode } from "react";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Button } from "../ui/button";

export default function ConfirmDialog({
  open, title, description, confirmLabel, pendingLabel, onConfirm, onCancel,
}: {
  open: boolean;
  title: string;
  description?: ReactNode;
  confirmLabel: string;
  pendingLabel?: string;
  onConfirm: () => Promise<void>;
  onCancel: () => void;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // A fresh open starts clean: a stale error from a previous attempt would
  // read as this one's.
  useEffect(() => {
    if (open) {
      setError(null);
      setPending(false);
    }
  }, [open]);

  if (!open) return null;

  return (
    <Dialog open onOpenChange={(o) => !o && !pending && onCancel()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="text-sm">{title}</DialogTitle>
        </DialogHeader>
        {description != null && (
          <div className="text-sm text-muted-foreground break-words">{description}</div>
        )}
        {error && (
          <div role="alert" className="text-xs text-destructive break-words">{error}</div>
        )}
        <DialogFooter className="gap-2">
          <Button variant="ghost" onClick={onCancel} disabled={pending} data-dialog-default-action>
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={pending}
            onClick={async () => {
              setError(null);
              setPending(true);
              try {
                await onConfirm();
              } catch (err) {
                // Handled by rendering it inline: the dialog stays open so the
                // user can read the reason and retry or cancel.
                // intentionally not logged: the message is shown in the dialog
                // itself, and a console line here would duplicate the one
                // failure the user can already see. Callers that also keep an
                // audit trail (the projectStore mutations) log before re-throwing.
                setError(err instanceof Error && err.message ? err.message : "The action failed");
              } finally {
                setPending(false);
              }
            }}
          >
            {pending ? (pendingLabel ?? "Working…") : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

- [x] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/components/common/ConfirmDialog.test.tsx`
Expected: PASS — 8 tests, 0 failures.

- [x] **Step 5: Commit**

```bash
git add web/src/components/common/ConfirmDialog.tsx web/src/components/common/ConfirmDialog.test.tsx
git commit -m "feat(web): add shared ConfirmDialog for destructive actions"
```

---

### Task 2: Make the destructive store mutations re-throw

**Files:**
- Modify: `web/src/stores/projectStore.tsx` (`removeProject`, `deleteGroup`)
- Test: `web/src/stores/projectStore.test.tsx`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `removeProject(path: string, host?: string): Promise<void>` and `deleteGroup(name: string): Promise<void>` — both now REJECT after logging. Tasks 3 and 4 rely on this rejection reaching `ConfirmDialog`.

- [x] **Step 1: Write the failing test**

Add to `web/src/stores/projectStore.test.tsx` — first extend the hoisted api mock so the new calls are observable, then add the describe:

```tsx
// in `const projectApi = vi.hoisted(...)`, after `setProjectGroup: vi.fn(),`
removeProject: vi.fn(),
deleteGroup: vi.fn(),

// in `vi.mock("../api/client", ...)`, after `setProjectGroup: projectApi.setProjectGroup,`
removeProject: projectApi.removeProject,
deleteGroup: projectApi.deleteGroup,
```

```tsx
describe("projectStore destructive mutations propagate failures", () => {
  it("removeProject propagates a local failure", async () => {
    projectApi.removeProject.mockRejectedValueOnce(new Error("remove project: 404"));
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const { result } = setup();
    await act(async () => {
      await expect(result.current.removeProject("/proj-a")).rejects.toThrow("404");
    });
    expect(errSpy).toHaveBeenCalledWith("Failed to remove project:", expect.any(Error));
    errSpy.mockRestore();
  });

  it("removeProject propagates a remote failure without touching the local call", async () => {
    // Mocks are not auto-cleared between tests in this file.
    projectApi.removeProject.mockClear();
    projectApi.removeRemoteProject.mockRejectedValueOnce(new Error("remote connect failed"));
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const { result } = setup();
    await act(async () => {
      await expect(result.current.removeProject("/remote", "dev@example.com"))
        .rejects.toThrow("remote connect failed");
    });
    expect(projectApi.removeRemoteProject).toHaveBeenCalledWith("/remote", "dev@example.com");
    expect(projectApi.removeProject).not.toHaveBeenCalled();
    errSpy.mockRestore();
  });

  it("deleteGroup propagates a failure", async () => {
    projectApi.deleteGroup.mockRejectedValueOnce(new Error("delete group: 404"));
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const { result } = setup();
    await act(async () => {
      await expect(result.current.deleteGroup("g")).rejects.toThrow("404");
    });
    expect(errSpy).toHaveBeenCalledWith("Failed to delete group:", expect.any(Error));
    errSpy.mockRestore();
  });
});
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/stores/projectStore.test.tsx -t "destructive mutations propagate failures"`
Expected: FAIL ×3 — `promise resolved "undefined" instead of rejecting`.

- [x] **Step 3: Write the minimal implementation**

`web/src/stores/projectStore.tsx` — `removeProject`:

```tsx
  const removeProject = useCallback(async (path: string, host?: string) => {
    try {
      if (host) {
        await api.removeRemoteProject(path, host);
      } else {
        await api.removeProject(path);
      }
      await refreshProjects();
    } catch (err) {
      // Re-thrown so the sidebar's removal confirm can keep the dialog open
      // with the reason attached; a swallowed failure closes the dialog as if
      // the project were gone. Same contract as renameProject below.
      console.error("Failed to remove project:", err);
      throw err;
    }
  }, [refreshProjects]);
```

`web/src/stores/projectStore.tsx` — `deleteGroup`:

```tsx
  const deleteGroup = useCallback(async (name: string) => {
    try {
      await api.deleteGroup(name);
      await refreshGroups();
      await refreshProjects();
    } catch (err) {
      // Re-thrown for the same reason as removeProject: the group-delete
      // confirm must survive a failure instead of closing as if it worked.
      console.error("Failed to delete group:", err);
      throw err;
    }
  }, [refreshGroups, refreshProjects]);
```

- [x] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/stores/projectStore.test.tsx`
Expected: PASS — 37 tests, 0 failures (the only callers of both actions are the two confirms in Task 3/4, which handle the rejection).

- [x] **Step 5: Commit**

```bash
git add web/src/stores/projectStore.tsx web/src/stores/projectStore.test.tsx
git commit -m "fix(web): re-throw failed project/group removals so the confirm can report them"
```

---

### Task 3: Project removal confirms on every sidebar surface

**Files:**
- Modify: `web/src/components/Layout/ProjectSidebar.tsx`
- Test: `web/src/components/Layout/ProjectSidebar.test.tsx`

**Interfaces:**
- Consumes: `ConfirmDialog` (Task 1); `removeProject` (Task 2, rejects).
- Produces: `pendingRemove: Project | null` state and `renderConfirms(): JSX.Element` in `ProjectSidebar`; `SortableGroupHeader`'s `onDelete` prop type becomes `() => void`.

- [x] **Step 1: Write the failing test**

Add module-scope helpers plus a describe to `web/src/components/Layout/ProjectSidebar.test.tsx` (they must be module scope so Task 4 can reuse them):

```tsx
/** The open confirm dialog, or null when none is open. */
function confirmDialog(): HTMLElement | null {
  return screen.queryByRole("dialog");
}

/** Click a confirm dialog's destructive button (default: "Remove"). */
function clickConfirm(name: RegExp = /^Remove$/): void {
  fireEvent.click(within(confirmDialog()!).getByRole("button", { name }));
}

describe("ProjectSidebar project removal confirmation", () => {
  beforeEach(() => {
    stateFake.projects = [project("/proj", "")];
    stateFake.groups = [];
    stateFake.activeProject = null;
    stateFake.tabsByProject = {};
    Object.keys(chatSessionsFake).forEach((k) => delete chatSessionsFake[k]);
    terminalStateFake.byProject = {};
    actionsFake.removeProject.mockClear();
    actionsFake.removeProject.mockResolvedValue(undefined);
  });

  it("does not remove the project until the confirm dialog is accepted", async () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    fireEvent.contextMenu(screen.getByText("proj"));
    fireEvent.click(screen.getByText("Remove"));

    expect(confirmDialog()).not.toBeNull();
    expect(actionsFake.removeProject).not.toHaveBeenCalled();

    clickConfirm();
    await waitFor(() =>
      expect(actionsFake.removeProject).toHaveBeenCalledWith("/proj", undefined),
    );
    await waitFor(() => expect(confirmDialog()).toBeNull());
  });

  it("keeps the project when the confirm dialog is cancelled", () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    fireEvent.contextMenu(screen.getByText("proj"));
    fireEvent.click(screen.getByText("Remove"));
    fireEvent.click(within(confirmDialog()!).getByRole("button", { name: "Cancel" }));
    expect(actionsFake.removeProject).not.toHaveBeenCalled();
    expect(confirmDialog()).toBeNull();
  });

  it("shows the host and path of a remote project, and reassures that files are kept", () => {
    stateFake.projects = [remoteProject("/home/user/app", "devbox")];
    const { container } = render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    // A remote row renders the name AND the host:path subtitle, both reading
    // "devbox:/home/user/app", so scope the right-click to the name node.
    fireEvent.contextMenu(container.querySelector(".group.relative .truncate.font-medium")!);
    fireEvent.click(screen.getByText("Remove"));

    const d = within(confirmDialog()!);
    expect(d.getByText("devbox:/home/user/app")).toBeDefined();
    expect(d.getByText(/not deleted/i)).toBeDefined();
    expect(d.queryByText(/cannot be undone/i)).toBeNull();
    clickConfirm();
    expect(actionsFake.removeProject).toHaveBeenCalledWith("/home/user/app", "devbox");
  });

  it("the expanded row's trash button also confirms", async () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    fireEvent.click(screen.getByTitle("Remove proj from the project list"));
    expect(confirmDialog()).not.toBeNull();
    expect(actionsFake.removeProject).not.toHaveBeenCalled();
    clickConfirm();
    await waitFor(() => expect(actionsFake.removeProject).toHaveBeenCalledTimes(1));
  });

  it("the collapsed rail's Remove also confirms", async () => {
    render(<ProjectSidebar isOpen={false} onToggle={vi.fn()} />);
    fireEvent.contextMenu(screen.getByLabelText("proj"));
    fireEvent.click(screen.getByText("Remove"));
    // The rail is a separate render branch with no dialogs of its own; the
    // confirm must be reachable there too.
    expect(confirmDialog()).not.toBeNull();
    expect(actionsFake.removeProject).not.toHaveBeenCalled();
    clickConfirm();
    await waitFor(() => expect(actionsFake.removeProject).toHaveBeenCalledTimes(1));
  });

  it("keeps the confirm open and shows the reason when the removal fails", async () => {
    actionsFake.removeProject.mockRejectedValueOnce(new Error("remove project: 404"));
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    fireEvent.contextMenu(screen.getByText("proj"));
    fireEvent.click(screen.getByText("Remove"));
    clickConfirm();
    await waitFor(() =>
      expect(within(confirmDialog()!).getByRole("alert").textContent).toContain("404"),
    );
    expect(confirmDialog()).not.toBeNull();
  });
});
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/Layout/ProjectSidebar.test.tsx -t "project removal confirmation"`
Expected: FAIL — 5 of 6 fail (`expected null not to be null` / `Unable to find role="alert"`), because `onRemove` calls the store directly.

- [x] **Step 3: Write the minimal implementation**

In `web/src/components/Layout/ProjectSidebar.tsx`:

1. Import: `import ConfirmDialog from "../common/ConfirmDialog";`
2. Add state and handlers in `ProjectSidebar`:

```tsx
  const [pendingRemove, setPendingRemove] = useState<Project | null>(null);

  const confirmRemoveProject = useCallback(async () => {
    const target = pendingRemove;
    if (!target) return;
    // No catch: the store re-throws, and ConfirmDialog renders the reason
    // inline and stays open so a failed removal never looks like a done one.
    await removeProject(target.path, target.host);
    setPendingRemove(null);
  }, [pendingRemove, removeProject]);
```

3. Point every entry point at the state instead of the store — in the collapsed rail:

```tsx
onRemove={() => setPendingRemove(p)}
```

and in the expanded row:

```tsx
onRemove={() => setPendingRemove(project)}
```

4. Render the dialog from BOTH branches. In the collapsed-rail branch (after the `</div>` closing the rail column, inside `<TooltipProvider>`) and at the end of `renderExpandedInner()` (after `<DirectoryBrowser … />`), render:

```tsx
<ConfirmDialog
  open={pendingRemove !== null}
  title="Remove project from the list?"
  // Deliberately NOT "this cannot be undone": removal only drops the entry
  // from projects.json (projects.Store.Remove), so files and transcripts
  // survive and the folder can be re-added. host:path disambiguates two
  // projects that share a path.
  description={
    pendingRemove && (
      <>
        <span className="font-medium text-foreground break-all">{pendingRemove.name}</span>{" "}
        <span className="font-mono text-xs break-all">
          ({pendingRemove.host ? `${pendingRemove.host}:${pendingRemove.path}` : pendingRemove.path})
        </span>
        <div className="mt-1 text-xs">
          Its files and chat sessions are not deleted — you can add this folder again at any time.
        </div>
      </>
    )
  }
  confirmLabel="Remove"
  pendingLabel="Removing…"
  onConfirm={confirmRemoveProject}
  onCancel={() => setPendingRemove(null)}
/>
```

5. Give the expanded row's trash button an accessible name (it had none):

```tsx
title={`Remove ${project.name} from the project list`}
aria-label={`Remove ${project.name} from the project list`}
```

- [x] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/components/Layout/ProjectSidebar.test.tsx`
Expected: PASS — 45 tests, 0 failures (6 new + 39 pre-existing).

- [x] **Step 5: Commit**

```bash
git add web/src/components/Layout/ProjectSidebar.tsx web/src/components/Layout/ProjectSidebar.test.tsx
git commit -m "fix(web): confirm before removing a project, on every sidebar surface"
```

---

### Task 4: Group deletion confirms, and says what it ungroups

**Files:**
- Modify: `web/src/components/Layout/ProjectSidebar.tsx`
- Test: `web/src/components/Layout/ProjectSidebar.test.tsx`

**Interfaces:**
- Consumes: `ConfirmDialog` (Task 1); `deleteGroup` (Task 2, rejects); the module-scope `confirmDialog()` / `clickConfirm()` helpers from Task 3.
- Produces: `pendingGroupDelete: { name: string; projectCount: number } | null` state; `SortableGroupHeader`'s `onDelete: () => void`.

- [x] **Step 1: Write the failing test**

Append to `web/src/components/Layout/ProjectSidebar.test.tsx`:

```tsx
describe("ProjectSidebar group deletion confirmation", () => {
  beforeEach(() => {
    stateFake.projects = [project("/w1", "Work"), project("/w2", "Work"), project("/u1", "")];
    stateFake.groups = [{ name: "Work", order: 1, collapsed: false }];
    stateFake.activeProject = null;
    stateFake.tabsByProject = {};
    Object.keys(chatSessionsFake).forEach((k) => delete chatSessionsFake[k]);
    terminalStateFake.byProject = {};
    actionsFake.deleteGroup.mockClear();
    actionsFake.deleteGroup.mockResolvedValue(undefined);
  });

  /** Open the group header's context menu and click Delete group. */
  function requestGroupDelete() {
    fireEvent.contextMenu(screen.getByText("Work"));
    fireEvent.click(screen.getByText("Delete group"));
  }

  it("does not delete the group until the confirm is accepted", async () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    requestGroupDelete();
    expect(confirmDialog()).not.toBeNull();
    expect(actionsFake.deleteGroup).not.toHaveBeenCalled();
    clickConfirm(/^Delete group$/);
    await waitFor(() => expect(actionsFake.deleteGroup).toHaveBeenCalledWith("Work"));
  });

  it("keeps the group when the confirm is cancelled", () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    requestGroupDelete();
    fireEvent.click(within(confirmDialog()!).getByRole("button", { name: "Cancel" }));
    expect(actionsFake.deleteGroup).not.toHaveBeenCalled();
    expect(confirmDialog()).toBeNull();
  });

  it("says how many projects move to Ungrouped and that moving back is manual", () => {
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    requestGroupDelete();
    const d = within(confirmDialog()!);
    expect(d.getByText(/2 projects/i)).toBeDefined();
    expect(d.getByText(/one by one/i)).toBeDefined();
  });

  it("uses the singular for a one-project group", () => {
    stateFake.projects = [project("/w1", "Work"), project("/u1", "")];
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    requestGroupDelete();
    const d = within(confirmDialog()!);
    expect(d.getByText(/Its 1 project will move to Ungrouped/i)).toBeDefined();
    expect(d.getByText(/to put it back/i)).toBeDefined();
  });

  it("says there is nothing to move for an empty group", () => {
    // A group can legitimately end up empty (its projects were dragged out);
    // the copy must not claim N projects are being moved.
    stateFake.projects = [project("/u1", "")];
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    requestGroupDelete();
    const d = within(confirmDialog()!);
    expect(d.getByText(/No projects are in this group/i)).toBeDefined();
    expect(d.queryByText(/Ungrouped/)).toBeNull();
  });

  it("keeps the confirm open and shows the reason when the delete fails", async () => {
    actionsFake.deleteGroup.mockRejectedValueOnce(new Error("delete group: 404"));
    render(<ProjectSidebar isOpen={true} onToggle={vi.fn()} />);
    requestGroupDelete();
    clickConfirm(/^Delete group$/);
    await waitFor(() =>
      expect(within(confirmDialog()!).getByRole("alert").textContent).toContain("404"),
    );
    expect(confirmDialog()).not.toBeNull();
  });
});
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/Layout/ProjectSidebar.test.tsx -t "group deletion confirmation"`
Expected: FAIL ×6 — no dialog is rendered.

- [x] **Step 3: Write the minimal implementation**

In `web/src/components/Layout/ProjectSidebar.tsx`:

1. State + handler, beside `pendingRemove`:

```tsx
  // Group + how many projects the server will ungroup (HandleDeleteGroup).
  const [pendingGroupDelete, setPendingGroupDelete] = useState<{
    name: string;
    projectCount: number;
  } | null>(null);

  const confirmDeleteGroup = useCallback(async () => {
    const target = pendingGroupDelete;
    if (!target) return;
    await deleteGroup(target.name);
    setPendingGroupDelete(null);
  }, [pendingGroupDelete, deleteGroup]);
```

2. Widen the header prop to reflect that it no longer deletes:

```tsx
interface SortableGroupHeaderProps {
  group: ProjectGroup;
  projectCount: number;
  onToggle: () => void;
  onRename: (name: string) => Promise<void>;
  /** Opens the delete confirm; the header never deletes the group itself. */
  onDelete: () => void;
}
```

3. Header call site — pass the count so the copy can be honest:

```tsx
onDelete={() => setPendingGroupDelete({ name: group.name, projectCount: groupProjects.length })}
```

4. Render a second `ConfirmDialog` inside the same `renderConfirms()` fragment as Task 3's:

```tsx
<ConfirmDialog
  open={pendingGroupDelete !== null}
  title="Delete group?"
  description={
    pendingGroupDelete && (
      <>
        <span className="font-medium text-foreground break-all">{pendingGroupDelete.name}</span>{" "}
        will be deleted.
        <div className="mt-1 text-xs">
          {pendingGroupDelete.projectCount === 0 ? (
            <>No projects are in this group.</>
          ) : (
            <>
              Its {pendingGroupDelete.projectCount}{" "}
              {pendingGroupDelete.projectCount === 1 ? "project" : "projects"} will move to
              Ungrouped — to put {pendingGroupDelete.projectCount === 1 ? "it" : "them"} back
              you would have to move {pendingGroupDelete.projectCount === 1 ? "it" : "them"} one by one.
            </>
          )}
        </div>
      </>
    )
  }
  confirmLabel="Delete group"
  pendingLabel="Deleting…"
  onConfirm={confirmDeleteGroup}
  onCancel={() => setPendingGroupDelete(null)}
/>
```

- [x] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/components/Layout/ProjectSidebar.test.tsx`
Expected: PASS — 51 tests, 0 failures.

- [x] **Step 5: Commit**

```bash
git add web/src/components/Layout/ProjectSidebar.tsx web/src/components/Layout/ProjectSidebar.test.tsx
git commit -m "fix(web): confirm before deleting a project group, and state what it ungroups"
```

---

### Task 5: Cron job delete confirms

**Files:**
- Modify: `web/src/components/Cron/CronPanel.tsx`
- Create: `web/src/components/Cron/CronPanel.confirm.test.tsx`

**Interfaces:**
- Consumes: `ConfirmDialog` (Task 1); `api.deleteCronJob(id): Promise<unknown>`, which rejects on failure.
- Produces: `pendingDelete: CronJob | null` state in `CronPanel`; `deleteJob(job: CronJob): Promise<void>` (no confirm inside, no catch).

- [x] **Step 1: Write the failing test**

Create `web/src/components/Cron/CronPanel.confirm.test.tsx`:

```tsx
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  listCronJobs: vi.fn(),
  getCronOutbox: vi.fn(),
  getCronTargets: vi.fn(),
  addCronJob: vi.fn(),
  updateCronJob: vi.fn(),
  deleteCronJob: vi.fn(),
  drainCronOutbox: vi.fn(),
  setCronTarget: vi.fn(),
}));

vi.mock("@/api/client", () => ({ api: mocks }));
vi.mock("./CronJobDialog", () => ({ default: () => null }));
vi.mock("./CronOutboxPanel", () => ({ default: () => <div data-testid="outbox" /> }));
vi.mock("./CronTargetsPanel", () => ({ default: () => <div data-testid="targets" /> }));
vi.mock("./CronHistoryPanel", () => ({ default: () => <div data-testid="history" /> }));

import CronPanel from "./CronPanel";

const job = {
  id: "job-1",
  name: "nightly-report",
  payload: { message: "do the thing" },
  schedule: { kind: "cron", expr: "0 9 * * *" },
  state: {},
  created_at_ms: 1,
  enabled: true,
};

function renderPanel() {
  return render(<CronPanel loadingKey="cron" onLoadingEvent={() => {}} active={false} />);
}
function clickRowTrash() {
  fireEvent.click(screen.getByRole("button", { name: "Delete nightly-report" }));
}
function dialog() {
  return screen.getByRole("dialog");
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listCronJobs.mockResolvedValue({ jobs: [job] });
  mocks.getCronOutbox.mockResolvedValue({ entries: [] });
  mocks.getCronTargets.mockResolvedValue({ targets: {} });
  mocks.deleteCronJob.mockResolvedValue({});
});

describe("CronPanel delete confirmation", () => {
  it("does not delete the job until the confirm is accepted", async () => {
    renderPanel();
    await screen.findByText("nightly-report");
    clickRowTrash();
    expect(screen.getByRole("dialog")).toBeDefined();
    expect(mocks.deleteCronJob).not.toHaveBeenCalled();
    fireEvent.click(within(dialog()).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(mocks.deleteCronJob).toHaveBeenCalledWith("job-1"));
  });

  it("names the job in the confirm", async () => {
    renderPanel();
    await screen.findByText("nightly-report");
    clickRowTrash();
    expect(within(dialog()).getByText("nightly-report")).toBeDefined();
  });

  it("keeps the job when the confirm is cancelled", async () => {
    renderPanel();
    await screen.findByText("nightly-report");
    clickRowTrash();
    fireEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));
    expect(mocks.deleteCronJob).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps the confirm open and shows the reason when the delete fails", async () => {
    mocks.deleteCronJob.mockRejectedValueOnce(new Error("job is still running"));
    renderPanel();
    await screen.findByText("nightly-report");
    clickRowTrash();
    fireEvent.click(within(dialog()).getByRole("button", { name: "Delete" }));
    await waitFor(() =>
      expect(within(dialog()).getByRole("alert").textContent).toContain("job is still running"),
    );
    expect(screen.getByRole("dialog")).toBeDefined();
  });

  it("focuses Cancel, so Enter on a fresh confirm does not delete", async () => {
    renderPanel();
    await screen.findByText("nightly-report");
    clickRowTrash();
    expect(within(dialog()).getByRole("button", { name: "Cancel" })).toHaveFocus();
  });
});
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/Cron/CronPanel.confirm.test.tsx`
Expected: FAIL ×5 — `Unable to find an accessible element with the role "button" and name "Delete nightly-report"` (the trash button has no accessible name and `window.confirm` returns false in jsdom).

- [x] **Step 3: Write the minimal implementation**

In `web/src/components/Cron/CronPanel.tsx`:

1. `import ConfirmDialog from "@/components/common/ConfirmDialog";`
2. State: `const [pendingDelete, setPendingDelete] = useState<CronJob | null>(null);`
3. Replace the native guard — the action no longer confirms or catches:

```tsx
  // Runs only from the delete confirm. Rejections propagate to ConfirmDialog,
  // which renders the reason inline and keeps the confirm open — closing it
  // would make a failed delete look like a completed one.
  const deleteJob = async (job: CronJob) => {
    await api.deleteCronJob(job.id);
    await refreshAll();
  };
```

4. Row trash button opens the confirm and gains an accessible name:

```tsx
title={`Delete ${job.name || "this job"}`}
aria-label={`Delete ${job.name || "this job"}`}
onClick={(e) => {
  e.stopPropagation();
  setPendingDelete(job);
}}
```

5. Render after `<CronJobDialog … />`:

```tsx
<ConfirmDialog
  open={pendingDelete !== null}
  title="Delete cron job?"
  description={
    pendingDelete && (
      <>
        <span className="font-medium text-foreground break-all">
          {pendingDelete.name || pendingDelete.payload.message}
        </span>{" "}
        will be deleted and will stop running on its schedule. Run history is kept on disk,
        but the job is no longer listed.
      </>
    )
  }
  confirmLabel="Delete"
  pendingLabel="Deleting…"
  onConfirm={async () => {
    if (!pendingDelete) return;
    await deleteJob(pendingDelete);
    setPendingDelete(null);
  }}
  onCancel={() => setPendingDelete(null)}
/>
```

Copy note: `Service.RemoveJob` (`internal/scheduler/scheduler.go:344`) only splices the job from its list and persists — it does NOT delete run history, so the copy must not claim it does.

- [x] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/components/Cron`
Expected: PASS — 2 files, 10 tests (5 new + 5 pre-existing loading tests).

- [x] **Step 5: Commit**

```bash
git add web/src/components/Cron/CronPanel.tsx web/src/components/Cron/CronPanel.confirm.test.tsx
git commit -m "fix(web): confirm before deleting a cron job (native confirm is a no-op on desktop)"
```

---

### Task 6: Clearing session logs confirms

**Files:**
- Modify: `web/src/components/Logs/LogPanel.tsx`
- Test: `web/src/components/Logs/LogPanel.test.tsx`

**Interfaces:**
- Consumes: `ConfirmDialog` (Task 1); `api.clearLogs(sessionId, host)` which rejects.
- Produces: `confirmClear: boolean` state; `handleClear(): Promise<void>` (no confirm, no catch).

- [x] **Step 1: Write the failing test**

Append to `web/src/components/Logs/LogPanel.test.tsx` and add `waitFor` to its `@testing-library/react` import:

```tsx
describe("LogPanel clear confirmation", () => {
  function clearRequests(fetchMock: ReturnType<typeof vi.fn>) {
    return fetchMock.mock.calls.filter((call) => (call[1] as RequestInit | undefined)?.method === "DELETE");
  }
  function renderPanel() {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, text: async () => "[]" });
    vi.stubGlobal("fetch", fetchMock as unknown as typeof fetch);
    render(<LogPanel active={false} sessionId="test-session" />);
    return fetchMock;
  }

  it("does not clear until the confirm is accepted", async () => {
    const fetchMock = renderPanel();
    await act(async () => {});
    fireEvent.click(screen.getByTitle("Clear logs"));
    expect(screen.getByRole("dialog")).toBeDefined();
    expect(clearRequests(fetchMock)).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Clear logs" }));
    await act(async () => {});
    expect(clearRequests(fetchMock)).toHaveLength(1);
    expect(String(clearRequests(fetchMock)[0][0])).toContain("session_id=test-session");
  });

  it("keeps the logs when the confirm is cancelled", async () => {
    const fetchMock = renderPanel();
    await act(async () => {});
    fireEvent.click(screen.getByTitle("Clear logs"));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(clearRequests(fetchMock)).toHaveLength(0);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps the confirm open and shows the reason when the clear fails", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, text: async () => "[]" });
    vi.stubGlobal("fetch", fetchMock as unknown as typeof fetch);
    render(<LogPanel active={false} sessionId="test-session" />);
    await act(async () => {});
    // Fail only the DELETE; the initial GET still succeeds.
    fetchMock.mockImplementation(async (_url: unknown, init?: RequestInit) =>
      init?.method === "DELETE"
        ? { ok: false, status: 500, text: async () => "boom", json: async () => ({ error: "boom" }) }
        : { ok: true, text: async () => "[]" },
    );
    fireEvent.click(screen.getByTitle("Clear logs"));
    fireEvent.click(screen.getByRole("button", { name: "Clear logs" }));
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toMatch(/500|boom|failed/i),
    );
    expect(screen.getByRole("dialog")).toBeDefined();
  });
});
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/Logs/LogPanel.test.tsx -t "clear confirmation"`
Expected: FAIL ×3 — `Unable to find an accessible element with the role "dialog"`.

- [x] **Step 3: Write the minimal implementation**

In `web/src/components/Logs/LogPanel.tsx`:

1. `import ConfirmDialog from "@/components/common/ConfirmDialog";`
2. `const [confirmClear, setConfirmClear] = useState(false);`
3. `handleClear` loses the native guard and the swallowing catch:

```tsx
  // Runs only from the clear confirm. A rejection propagates to ConfirmDialog,
  // which shows the reason inline and keeps the dialog open instead of
  // dropping the log list as if the file had been cleared.
  const handleClear = async () => {
    await api.clearLogs(sessionId, host);
    setLogs([]);
  };
```

4. Toolbar button opens the confirm: `onClick={() => setConfirmClear(true)}` and add `aria-label="Clear logs"`.
5. Render as the last child of the panel root:

```tsx
<ConfirmDialog
  open={confirmClear}
  title="Clear logs for this session?"
  description="The session's log file is emptied on disk. This cannot be undone."
  confirmLabel="Clear logs"
  pendingLabel="Clearing…"
  onConfirm={async () => {
    await handleClear();
    setConfirmClear(false);
  }}
  onCancel={() => setConfirmClear(false)}
/>
```

- [x] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/components/Logs/LogPanel.test.tsx`
Expected: PASS — 19 tests, 0 failures (16 pre-existing scroll/reset tests unaffected).

- [x] **Step 5: Commit**

```bash
git add web/src/components/Logs/LogPanel.tsx web/src/components/Logs/LogPanel.test.tsx
git commit -m "fix(web): confirm before clearing session logs (native confirm is a no-op on desktop)"
```

---

### Task 7: Profile delete and provider-key removal confirm

**Files:**
- Modify: `web/src/components/Settings/ProfilesManager.tsx`
- Create: `web/src/components/Settings/ProfilesManager.test.tsx`

**Interfaces:**
- Consumes: `ConfirmDialog` (Task 1); `authedFetch` from `@/api/client`.
- Produces: `pendingDelete: Profile | null`, `pendingKeyDelete: { name: string; provider: string } | null`; `remove(profile: Profile): Promise<void>` (throws) and `deleteKey(name, provider): Promise<void>` (throws).

- [x] **Step 1: Write the failing test**

Create `web/src/components/Settings/ProfilesManager.test.tsx`:

```tsx
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const authedFetch = vi.hoisted(() => vi.fn());
vi.mock("@/api/client", () => ({ authedFetch: (...args: unknown[]) => authedFetch(...args) }));
vi.mock("../ProfileSwitcher", () => ({ getActiveWindowId: () => "win-1" }));

import ProfilesManager from "./ProfilesManager";

const profile = { name: "work", displayName: "Work", overrideCount: 2, credentialCount: 1 };
const credential = { provider: "openai", label: "OpenAI", masked: "sk-…1234", kind: "apiKey" };

/** Route authedFetch by method+url so each test only asserts what it cares about. */
function routeFetch(overrides: Record<string, { ok?: boolean; body?: unknown }> = {}) {
  authedFetch.mockImplementation(async (url: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${url}`;
    for (const [pattern, spec] of Object.entries(overrides)) {
      if (key.includes(pattern)) {
        return {
          ok: spec.ok ?? true,
          status: spec.ok === false ? 400 : 200,
          text: async () => (typeof spec.body === "string" ? spec.body : JSON.stringify(spec.body ?? {})),
          json: async () => spec.body ?? {},
        };
      }
    }
    // Detail routes first: they all START with "GET /api/profiles", so a
    // list-first router would shadow them and silently yield no credentials.
    if (key.includes("/effective")) {
      return { ok: true, status: 200, text: async () => "{}", json: async () => ({}) };
    }
    if (key.includes("/auth")) {
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify({ credentials: [credential] }),
        json: async () => ({ credentials: [credential] }),
      };
    }
    if (key === "GET /api/profiles") {
      return {
        ok: true, status: 200,
        text: async () => JSON.stringify({ profiles: [profile] }),
        json: async () => ({ profiles: [profile] }),
      };
    }
    return { ok: true, status: 200, text: async () => "{}", json: async () => ({}) };
  });
}

function jsonCalls(method: string) {
  return authedFetch.mock.calls.filter((call) => (call[1] as RequestInit | undefined)?.method === method);
}

async function renderManager() {
  const view = render(<ProfilesManager />);
  // The row's name button renders "work ▸" as split text nodes, so wait on the
  // row's own Delete button instead.
  await screen.findByRole("button", { name: "Delete" });
  return view;
}

/** Expand the profile row so its credential list (and the Remove button) shows. */
async function expandProfile() {
  fireEvent.click(screen.getByRole("button", { name: /work/ }));
  await screen.findByRole("button", { name: "Remove" });
}

function dialog() {
  return screen.getByRole("dialog");
}

beforeEach(() => {
  authedFetch.mockReset();
  routeFetch();
});

describe("ProfilesManager delete confirmation", () => {
  it("does not delete the profile until the confirm is accepted", async () => {
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(dialog()).toBeDefined();
    expect(jsonCalls("DELETE")).toHaveLength(0);
    fireEvent.click(within(dialog()).getByRole("button", { name: "Delete profile" }));
    await waitFor(() => expect(jsonCalls("DELETE")[0][0]).toBe("/api/profiles/work"));
  });

  it("states what the delete destroys and that it cannot be undone", async () => {
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    const copy = dialog().textContent ?? "";
    expect(copy).toContain("2");
    expect(copy).toMatch(/1 key/i);
    expect(copy).toMatch(/cannot be undone/i);
  });

  it("keeps the profile when the confirm is cancelled", async () => {
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    fireEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));
    expect(jsonCalls("DELETE")).toHaveLength(0);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps the confirm open and shows the reason when the delete fails", async () => {
    routeFetch({ "DELETE /api/profiles/work": { ok: false, body: { error: "profile is active" } } });
    await renderManager();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    fireEvent.click(within(dialog()).getByRole("button", { name: "Delete profile" }));
    await waitFor(() =>
      expect(within(dialog()).getByRole("alert").textContent).toMatch(/cannot delete|active|failed/i),
    );
    expect(dialog()).toBeDefined();
  });
});

describe("ProfilesManager provider key removal", () => {
  it("does not remove the key until the confirm is accepted", async () => {
    await renderManager();
    await expandProfile();
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    expect(dialog()).toBeDefined();
    expect(jsonCalls("DELETE")).toHaveLength(0);
    fireEvent.click(within(dialog()).getByRole("button", { name: "Remove key" }));
    await waitFor(() => expect(jsonCalls("DELETE")[0][0]).toBe("/api/profiles/work/auth/openai"));
  });

  it("keeps the key when the confirm is cancelled", async () => {
    await renderManager();
    await expandProfile();
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    fireEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));
    expect(jsonCalls("DELETE")).toHaveLength(0);
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/Settings/ProfilesManager.test.tsx`
Expected: FAIL ×6 — `Unable to find an accessible element with the role "dialog"`.

- [x] **Step 3: Write the minimal implementation**

In `web/src/components/Settings/ProfilesManager.tsx`:

1. `import ConfirmDialog from "../common/ConfirmDialog"`
2. State:

```tsx
  // Destructive and irreversible (credentials live in auth.profiles.json), so
  // both actions are gated by a rendered confirm. Native `confirm()` silently
  // returns false in the Wails/WKWebView desktop webview, which made them
  // unreachable there.
  const [pendingDelete, setPendingDelete] = useState<Profile | null>(null)
  const [pendingKeyDelete, setPendingKeyDelete] = useState<{ name: string; provider: string } | null>(null)
```

3. `remove` takes the profile and THROWS so the dialog can report:

```tsx
  // Runs only from the delete confirm. Throws on failure so ConfirmDialog can
  // show the reason inline and stay open instead of the row just vanishing.
  const remove = async (profile: Profile) => {
    const name = profile.name
    if (active === name) throw new Error(`Cannot delete "${name}" — it is active in this window. Switch to Default first.`)
    const res = await authedFetch(`/api/profiles/${encodeURIComponent(name)}`, { method:"DELETE" })
    if (!res.ok) {
      const t = await res.text()
      throw new Error(t.includes("active") ? `Cannot delete "${name}" — switch to Default first.` : (t || `Failed to delete "${name}"`))
    }
    setError(null)
    await refresh()
  }
```

4. `deleteKey` also throws:

```tsx
  // Runs only from the key-removal confirm; throws on failure (see remove).
  const deleteKey = async (name: string, provider: string) => {
    const res = await authedFetch(`/api/profiles/${encodeURIComponent(name)}/auth/${encodeURIComponent(provider)}`, { method:"DELETE" })
    if (!res.ok) { const t = await res.text(); throw new Error(t || `Failed to remove the ${provider} key`) }
    const cRes = await authedFetch(`/api/profiles/${encodeURIComponent(name)}/auth`).then(r=>r.json())
    setCreds(cRes.credentials || [])
    await refresh()
  }
```

5. Row buttons open confirms instead of acting: `onClick={()=>setPendingDelete(p)}` and `onClick={()=>setPendingKeyDelete({ name: p.name, provider: c.provider })}`.
6. Render two `ConfirmDialog`s as the last children of the root, with copy that keeps "cannot be undone" (credentials are destroyed on disk) and names the fallback: profile delete → "Removes 2 overrides and 1 key from `auth.profiles.json` — this cannot be undone."; key removal → "The stored key is deleted from `auth.profiles.json` — this cannot be undone. That provider then falls back to the base `auth.json` or the environment."

- [x] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/components/Settings/ProfilesManager.test.tsx`
Expected: PASS — 6 tests, 0 failures.

- [x] **Step 5: Commit**

```bash
git add web/src/components/Settings/ProfilesManager.tsx web/src/components/Settings/ProfilesManager.test.tsx
git commit -m "fix(web): confirm before deleting a profile or removing a provider key"
```

---

### Task 8: Document the contract and validate the branch

**Files:**
- Modify: `CHANGES.md` (new dated entry at the top)
- Modify: `skills/ocode-web/SKILL.md` (rules 37 and 38)
- Existing: `docs/superpowers/specs/2026-09-25-shared-confirm-dialog-design.md`

**Interfaces:**
- Consumes: everything from Tasks 1–7.
- Produces: the durable record of the confirm contract for future work.

- [x] **Step 1: Write the CHANGES.md entry**

Add at the top of `CHANGES.md`, under `# Changelog`:

```markdown
## 2026-09-25 — Destructive actions confirm, and a failed one now says so

- Removing a project or deleting a project group from the web/desktop sidebar
  no longer happens on a single click. The row context menu, the row's hover
  trash button, and the collapsed rail's menu all open a confirmation dialog
  first. Deleting a group says how many projects will move to Ungrouped and
  that putting them back is one-by-one.
- Four destructive actions were still guarded by native `window.confirm`,
  which silently returns false in the Wails/WKWebView desktop webview — so in
  the desktop app they looked wired up and did nothing: delete cron job
  (Cron), clear session logs (Logs), delete profile and remove a provider key
  (Settings → Profiles). All four now use a rendered dialog, so the action
  works on desktop as well as in a browser. Each also gained an accessible
  name where it had none.
- New shared `web/src/components/common/ConfirmDialog.tsx` owns the structure
  (safe action default-focused, destructive confirm, pending label). A
  **rejected** action shows the reason inline and keeps the dialog open, so a
  failed write is never presented as a completed one. `projectStore`'s
  `removeProject` and `deleteGroup` now re-throw after logging, matching
  `renameProject`; previously they swallowed the failure and the confirm
  closed as if the project were gone.
- Wording is honest per action: removing a project says its files and chat
  sessions are not deleted (only the list entry goes), while clearing logs and
  deleting a profile/key keep "cannot be undone" because those are destructive
  on disk.
- Tests: `web/src/components/common/ConfirmDialog.test.tsx`,
  `web/src/components/Layout/ProjectSidebar.test.tsx`,
  `web/src/components/Cron/CronPanel.confirm.test.tsx`,
  `web/src/components/Logs/LogPanel.test.tsx`,
  `web/src/components/Settings/ProfilesManager.test.tsx`,
  `web/src/stores/projectStore.test.tsx`. Design record:
  `docs/superpowers/specs/2026-09-25-shared-confirm-dialog-design.md`.
```

- [x] **Step 2: Write the skill rules**

Add to `skills/ocode-web/SKILL.md` as rules 37 and 38 (renumber any colliding item, and keep the list sequential): rule 37 = the `ConfirmDialog` contract (never `window.confirm`/`prompt`; `onConfirm` must reject; safe action is `data-dialog-default-action`; the component owns structure only; success does not auto-close; why not `ActionErrorToast`), rule 38 = project/group removal is two-step on every sidebar surface and the wording honesty rules.

- [x] **Step 3: Validate the whole web suite**

Run: `cd web && npx vitest run`
Expected: PASS — 268 test files, 2214 tests, 0 failures.

- [x] **Step 4: Typecheck and build**

Run: `cd web && npx tsgo --noEmit && npx vite build`
Expected: both clean, no output beyond the npm `auto-install-peers` warning.

- [x] **Step 5: Commit**

```bash
git add CHANGES.md skills/ocode-web/SKILL.md
git commit -m "docs: record the shared confirm-dialog contract"
```

---

### Task 9: Remaining native-dialog stragglers (DONE — see the note)

> **Implementation note (deviation from the steps below).** A concurrent session implemented this task while the plan was being written, and its approach differs from steps 1–3 in one respect worth keeping: the profile rename is a `ConfirmDialog` with the `Input` rendered inside its `description` plus the new optional `confirmVariant="default"` prop, rather than a separate bespoke form dialog. That is the better of the two options the plan weighed — one shared component instead of a second bespoke dialog — and it avoids a red `variant="destructive"` button on an action that does not destroy anything. The `rename` handler consequently takes `(oldName, nextName)`, throws on an invalid name or a failed request so the dialog keeps the typed value, and Enter submits. The cron outbox half was implemented exactly as planned (`pendingOutboxClear` + `drainOutbox`), with copy verified against `scheduler.Outbox.Drain` in `internal/scheduler/deliver.go`. Both halves ship with tests in the files listed below.

The last two places in `web/src` where a native dialog was still used, or a destructive action was still unguarded — the same class of bug as Tasks 5–7, and the reason a profile rename was **impossible in the desktop app**. There are now no live `window.confirm` / `window.prompt` call sites in `web/src` (grep hits are comments only). Two deliberate refinements were made while executing these steps: `ConfirmDialog` gained an optional `confirmVariant` so the rename dialog is not painted destructive red, and the rename's validation failure now surfaces INSIDE the dialog via a thrown error instead of the app-wide banner, which is what keeps the user's input on screen.

**Files:**
- Modify: `web/src/components/Settings/ProfilesManager.tsx` (`rename`, line ~79)
- Create: `web/src/components/Settings/ProfilesManager.rename.test.tsx`
- Modify: `web/src/components/Cron/CronPanel.tsx` (`clearOutbox`)
- Test: `web/src/components/Cron/CronPanel.confirm.test.tsx`

**Interfaces:**
- Consumes: `ConfirmDialog` (Task 1).
- Produces: a rendered rename input for profiles; a confirm for draining the cron outbox.

- [x] **Step 1: Write the failing test for the profile rename**

In a new `web/src/components/Settings/ProfilesManager.test.tsx` block: click a profile row's `Rename` button, assert a dialog with a text input appears, assert no `POST /api/profiles/{name}/rename` call happened yet, then submit and assert the POST fires. Assert Cancel issues no request.

- [x] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/components/Settings/ProfilesManager.test.tsx -t "rename"`
Expected: FAIL — `window.prompt` is not implemented, so no dialog appears.

- [x] **Step 3: Replace `prompt()` with a rendered dialog**

In `rename`, replace `const n = prompt(...)` with a `pendingRename: { oldName: string; value: string } | null` state plus a `ConfirmDialog` whose `description` is a labelled `Input`; keep the existing `[a-z0-9_-]{1,32}` validation and the "name must match" error in the existing `setError` banner. On confirm, POST the rename and `setPendingRename(null)`.

- [x] **Step 4: Write the failing test for the cron outbox clear**

In `web/src/components/Cron/CronPanel.confirm.test.tsx`: click the outbox panel's Clear control, assert `api.drainCronOutbox` was not called, then confirm and assert it was called once.

- [x] **Step 5: Run it to verify it fails**

Run: `cd web && npx vitest run src/components/Cron/CronPanel.confirm.test.tsx -t "outbox"`
Expected: FAIL — no dialog.

- [x] **Step 6: Add the confirm**

Gate `clearOutbox` behind a `ConfirmDialog` (title "Clear pending deliveries?", description naming that queued cron deliveries are dropped and not retried), with `clearOutbox` losing its `setError` catch so a rejection reaches the dialog.

- [x] **Step 7: Run the full suite and the build**

Run: `cd web && npx vitest run && npx tsgo --noEmit && npx vite build`
Expected: 0 failures, clean typecheck, clean build.

- [x] **Step 8: Update docs and commit**

Extend the `CHANGES.md` entry and rule 37 to name both surfaces, then:

```bash
git add web/src/components/Settings/ProfilesManager.tsx web/src/components/Settings/ProfilesManager.test.tsx web/src/components/Cron/CronPanel.tsx web/src/components/Cron/CronPanel.confirm.test.tsx CHANGES.md skills/ocode-web/SKILL.md
git commit -m "fix(web): replace the last native dialog and guard the cron outbox clear"
```