# Plan: Replace Hand-Rolled UI Components with shadcn/ui

## Executive Summary

Migrate three hand-crafted React components (ModelSelector, PermissionDialog, CommandPalette) to shadcn/ui primitives in the `web/` directory. This adds Radix UI accessibility, keyboard navigation, and a consistent design system to the ocode web UI without breaking the existing dark theme or Go build pipeline.

**Scope**: 3 component migrations + shadcn infrastructure setup. ~48KB gzipped bundle increase. No backend changes. No changes to the Go embed or build process.

---

## Current State Analysis

### Project Structure
```
web/
├── package.json          # React 18.3.1 + Vite 6.0.5 + Tailwind 3.4.17
├── tailwind.config.ts    # darkMode: "class", empty theme.extend
├── tsconfig.json         # strict, noUnusedLocals, bundler resolution
├── postcss.config.js     # tailwindcss + autoprefixer
├── index.html            # <html class="dark">, bg-zinc-900 body
├── src/
│   ├── index.css         # 3 Tailwind directives + minimal :root/.light
│   ├── main.tsx
│   ├── App.tsx           # Root: ErrorBoundary > ChatProvider > AppInner
│   ├── api/
│   │   ├── client.ts     # REST + SSE API client
│   │   └── types.ts      # ModelInfo { name, provider }, etc.
│   ├── stores/
│   │   └── chatStore.tsx # useReducer-based chat state
│   ├── hooks/
│   │   ├── useChat.ts
│   │   ├── useKeyboard.ts
│   │   ├── useSessions.ts
│   │   ├── useSSE.ts
│   │   └── useTheme.ts
│   └── components/
│       ├── Chat/         # ChatPanel, ChatInput, MessageBubble
│       ├── Sidebar/      # ModelSelector, AgentTabs, SessionList
│       ├── Files/        # FileTree
│       ├── Git/          # GitPanel
│       └── common/       # CommandPalette, PermissionDialog, ErrorBoundary, StatusBar
```

### Components to Migrate

| Component | File | Lines | Current Pattern | Target shadcn Component |
|-----------|------|-------|-----------------|------------------------|
| ModelSelector | `Sidebar/ModelSelector.tsx` | 29 | Plain `<select>` + Tailwind classes | `Select` (Radix Select) |
| PermissionDialog | `common/PermissionDialog.tsx` | 44 | Fixed overlay + manual dismiss state | `Dialog` (Radix Dialog) |
| CommandPalette | `common/CommandPalette.tsx` | 86 | Fixed overlay + input + manual keyboard nav | `Command` (cmdk wrapper) |

### Critical Finding: PermissionDialog is Orphaned

`PermissionDialog` is **defined but never imported** anywhere in the codebase. It is not rendered in `App.tsx` or any other component. The migration plan includes wiring it into the app before/alongside the shadcn conversion.

### Existing Theme Strategy

- `index.html`: `<html class="dark"><body class="bg-zinc-900 text-zinc-100">`
- All components use hardcoded Tailwind zinc classes: `bg-zinc-900`, `border-zinc-700`, `text-zinc-100`, `text-zinc-400`, `text-zinc-500`
- `useTheme.ts` toggles `dark` class on `<html>` and persists to localStorage
- Light mode: `.light body { background: #ffffff; color: #18181b }`

---

## Detailed Implementation Plan

### Phase 0: shadcn/ui Infrastructure Setup

**Goal**: Initialize shadcn/ui in the existing project without breaking anything.

#### Step 0.1: Run shadcn init

```bash
cd web
npx shadcn@latest init
```

Interactive prompts — answer:
- **Style**: Default
- **Base color**: Zinc (matches existing palette)
- **CSS variables**: Yes
- **Tailwind config path**: `tailwind.config.ts`
- **CSS path**: `src/index.css`
- **tsconfig path**: `tsconfig.json`

**What this does**:
1. Creates `components.json` (shadcn config file)
2. Creates `src/lib/utils.ts` with the `cn()` helper
3. Adds CSS variables to `index.css` inside `@layer base`:
   ```css
   @layer base {
     :root {
       --background: 0 0% 100%;
       --foreground: 240 10% 3.9%;
       /* ... more variables ... */
     }
     .dark {
       --background: 240 10% 3.9%;
       --foreground: 0 0% 98%;
       /* ... maps to zinc tones ... */
     }
   }
   ```
4. Extends `tailwind.config.ts` with CSS variable-based theme tokens:
   ```ts
   theme: {
     extend: {
       colors: {
         background: "hsl(var(--background))",
         foreground: "hsl(var(--foreground))",
         border: "hsl(var(--border))",
         input: "hsl(var(--input))",
         primary: { DEFAULT: "hsl(var(--primary))", foreground: "hsl(var(--primary-foreground))" },
         /* ... etc ... */
       },
       borderRadius: {
         lg: "var(--radius)",
         md: "calc(var(--radius) - 2px)",
         sm: "calc(var(--radius) - 4px)",
       },
     },
   }
   ```

**Important**: The CSS variables use HSL values *without* the `hsl()` wrapper. Tailwind config wraps them. Existing `bg-zinc-900` classes continue working alongside the new `bg-background` classes — they are not mutually exclusive.

#### Step 0.2: Install shadcn components

```bash
npx shadcn@latest add select dialog command button
```

This installs:
- `src/components/ui/select.tsx` — Radix Select wrapper
- `src/components/ui/dialog.tsx` — Radix Dialog wrapper
- `src/components/ui/command.tsx` — cmdk wrapper
- `src/components/ui/button.tsx` — cva-based button variants

Plus Radix dependencies in `package.json`:
- `@radix-ui/react-dialog`
- `@radix-ui/react-select`
- `@radix-ui/react-label`
- `@radix-ui/react-slot`
- `cmdk`
- `class-variance-authority`
- `clsx`
- `tailwind-merge`
- `lucide-react` (icons, used by shadcn components)

#### Step 0.3: Verify build

```bash
npm run build   # tsc && vite build
```

Ensure TypeScript compilation passes and the Vite build succeeds. The `dist/` output should still be embeddable by the Go binary.

#### Step 0.4: Fix any tsconfig issues

The existing `tsconfig.json` has `"noUnusedLocals": true` and `"noUnusedParameters": true`. shadcn-generated code uses all its imports, so this should be fine. If `tsc` flags anything, the `src/lib/utils.ts` `cn()` function and shadcn component files are the most likely candidates — inspect and fix.

---

### Phase 1: Migrate ModelSelector → shadcn Select

**File**: `web/src/components/Sidebar/ModelSelector.tsx`

#### Current Implementation (29 lines)
- Fetches `ModelInfo[]` via `api.listModels()`
- Renders a plain `<select>` with `{models.map(m => <option>{m.name}</option>)}`
- Dispatches `SET_MODEL` on change
- Styled with manual Tailwind classes

#### Target Implementation
```tsx
import { useState, useEffect } from "react";
import { api } from "../../api/client";
import { useChatState, useChatDispatch } from "../../stores/chatStore";
import type { ModelInfo } from "../../api/types";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../ui/select";

export default function ModelSelector() {
  const [models, setModels] = useState<ModelInfo[]>([]);
  const { model: activeModel } = useChatState();
  const dispatch = useChatDispatch();

  useEffect(() => {
    api.listModels().then(setModels).catch(console.error);
  }, []);

  return (
    <div className="p-3 border-b border-zinc-700">
      <label className="text-xs text-zinc-500 uppercase tracking-wider">Model</label>
      <Select
        value={activeModel || ""}
        onValueChange={(v) => dispatch({ type: "SET_MODEL", model: v })}
      >
        <SelectTrigger className="mt-1 w-full">
          <SelectValue placeholder="Select a model" />
        </SelectTrigger>
        <SelectContent>
          {models.map((m) => (
            <SelectItem key={m.name} value={m.name}>
              {m.name}
              <span className="ml-2 text-zinc-500 text-xs">({m.provider})</span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
```

#### Key Changes
1. Replace `<select>` with `<Select>` / `<SelectTrigger>` / `<SelectContent>` / `<SelectItem>`
2. `onChange={(e) => ...}` → `onValueChange={(v) => ...}`
3. Show provider as secondary text in each item (enhancement over current bare `<option>`)
4. `<option key={m.name}>` → `<SelectItem key={m.name} value={m.name}>`

#### Risk Assessment
- **Low risk**: Direct 1:1 mapping. Radix Select handles keyboard nav, focus, and accessibility automatically.
- **Edge case**: If `models` is empty, `SelectContent` renders nothing — same as current behavior.
- **Empty value guard**: `value={activeModel || ""}` — Radix Select requires non-empty value. If `activeModel` can be `""`, add a disabled placeholder `<SelectItem value="" disabled>Select a model</SelectItem>` or use `value={activeModel || undefined}` to make it uncontrolled.

---

### Phase 2: Migrate PermissionDialog → shadcn Dialog

**Files**: 
- `web/src/components/common/PermissionDialog.tsx` (rewrite)
- `web/src/App.tsx` or a new SSE handler (wire it in)

#### Current Implementation (44 lines)
- Props: `{ tool, command?, requestId, onApprove }`
- Uses `useState(false)` for dismiss tracking
- Renders conditionally (null when dismissed)
- Hand-rolled fixed overlay with backdrop

#### Critical: PermissionDialog is Not Wired

The component exists but is **never imported or rendered**. Before migrating to shadcn Dialog, it needs to be integrated into the app. The likely integration point:

1. **SSE handler** in `useSSE.ts` or `useChat.ts` — the backend sends `SSEToolStartEvent` (type defined in `api/types.ts` lines 36-39) which includes `tool` and `command` fields
2. **App.tsx** — render `<PermissionDialog>` when a tool permission event arrives

#### Proposed Wiring (new state in chatStore or local state)

Add to `chatStore.tsx`:
```ts
interface ChatState {
  // ... existing fields ...
  permissionRequest: {
    tool: string;
    command?: string;
    requestId: string;
  } | null;
}

type ChatAction =
  // ... existing actions ...
  | { type: "SET_PERMISSION_REQUEST"; request: ChatState["permissionRequest"] }
  | { type: "CLEAR_PERMISSION_REQUEST" };
```

Then in the SSE handler, when a `tool_start` event arrives with a permission-required tool, dispatch `SET_PERMISSION_REQUEST`.

#### Target Implementation

```tsx
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import { Button } from "../ui/button";

interface Props {
  open: boolean;
  tool: string;
  command?: string;
  requestId: string;
  onApprove: (requestId: string, approved: boolean) => void;
  onClose: () => void;
}

export default function PermissionDialog({
  open, tool, command, requestId, onApprove, onClose,
}: Props) {
  return (
    <Dialog open={open} onOpenChange={(isOpen) => {
      if (!isOpen) {
        onApprove(requestId, false); // deny on overlay click / escape
        onClose();
      }
    }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Permission Required</DialogTitle>
          <DialogDescription>
            The agent wants to use{" "}
            <span className="font-mono text-blue-400">{tool}</span>
          </DialogDescription>
        </DialogHeader>
        {command && (
          <pre className="rounded bg-zinc-800 p-3 text-xs text-zinc-300 overflow-x-auto">
            {command}
          </pre>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={() => {
            onApprove(requestId, false);
            onClose();
          }}>
            Deny
          </Button>
          <Button onClick={() => {
            onApprove(requestId, true);
            onClose();
          }}>
            Approve
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

#### Key Changes
1. Remove manual `dismissed` state — parent controls `open` prop
2. Replace hand-rolled overlay with `<Dialog>` + `<DialogContent>` (Radix manages portal, focus trap, ESC handling)
3. `<button>` → `<Button variant="outline">` and `<Button>` (default primary)
4. Add `onClose` prop for parent to clear the permission request state
5. `onOpenChange` handles overlay-click and ESC as deny

#### Integration in App.tsx

```tsx
import PermissionDialog from "./components/common/PermissionDialog";

function AppInner() {
  const { permissionRequest } = useChatState();
  const dispatch = useChatDispatch();
  
  // ... existing code ...

  return (
    <div className="flex h-screen">
      {/* ... existing layout ... */}
      {permissionRequest && (
        <PermissionDialog
          open={true}
          tool={permissionRequest.tool}
          command={permissionRequest.command}
          requestId={permissionRequest.requestId}
          onApprove={(id, approved) => {
            // Send approval back to backend via API
            api.approveTool(id, approved);
            dispatch({ type: "CLEAR_PERMISSION_REQUEST" });
          }}
          onClose={() => dispatch({ type: "CLEAR_PERMISSION_REQUEST" })}
        />
      )}
    </div>
  );
}
```

#### Risk Assessment
- **Medium risk**: Requires new API endpoint on the Go backend to handle tool approval/denial. This is **out of scope** for the frontend-only migration — the wiring can be stubbed or deferred.
- **Alternative**: Keep the component orphaned for now, migrate it to shadcn Dialog format, and wire it in a separate PR when the backend permission API is ready.

---

### Phase 3: Migrate CommandPalette → shadcn Command (cmdk)

**File**: `web/src/components/common/CommandPalette.tsx`

#### Current Implementation (86 lines)
- Props: `{ open, onClose, onExecute }`
- Hardcoded 4 commands array
- Manual keyboard navigation (ArrowUp/Down/Enter/Escape)
- Manual input focus via `useRef`
- Hand-rolled filtered list with selected state tracking

#### Target Implementation

```tsx
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "../ui/command";

interface Props {
  open: boolean;
  onClose: () => void;
  onExecute: (command: string) => void;
}

const COMMANDS = [
  { name: "/clear", description: "Clear chat history" },
  { name: "/model", description: "Switch model" },
  { name: "/session", description: "Switch session" },
  { name: "/help", description: "Show help" },
];

export default function CommandPalette({ open, onClose, onExecute }: Props) {
  return (
    <CommandDialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <CommandInput placeholder="Type a command..." />
      <CommandList>
        <CommandEmpty>No commands found</CommandEmpty>
        <CommandGroup heading="Commands">
          {COMMANDS.map((cmd) => (
            <CommandItem
              key={cmd.name}
              value={`${cmd.name} ${cmd.description}`}
              onSelect={() => {
                onExecute(cmd.name);
                onClose();
              }}
            >
              <span className="font-mono text-blue-400">{cmd.name}</span>
              <span className="ml-2 text-zinc-500">{cmd.description}</span>
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
```

#### Key Changes
1. Remove ALL manual keyboard handling (cmdk handles ArrowUp/Down/Enter/Escape natively)
2. Remove `useRef` for input focus (cmdk manages its own focus)
3. Remove `useState` for query and selected index (cmdk handles filtering and selection)
4. Replace hand-rolled overlay with `<CommandDialog>` (Radix Dialog + cmdk)
5. Replace filtered list rendering with `<CommandList>` + `<CommandItem>`
6. `value` prop on `<CommandItem>` enables cmdk's fuzzy search on combined name+description
7. `<CommandEmpty>` replaces the manual empty state

#### Parent Component Change

The `App.tsx` `handleCommand` function (line 26-28) stays exactly the same — `onExecute` still receives the command string.

#### Keyboard Shortcut

The `useKeyboard.ts` hook (line 15-17) opens the palette on `Cmd+K`/`Ctrl+K`. This stays unchanged — `CommandDialog` renders in a portal and handles focus correctly when opened programmatically.

#### Risk Assessment
- **Low risk**: cmdk is battle-tested (used by Vercel, Linear, Raycast). The `value` prop controls search matching — setting it to `${name} ${description}` matches the current filter-by-name behavior plus adds description search (enhancement).
- **One thing to verify**: `CommandDialog` renders inside a Radix `Dialog`. If the app already has a dialog open (PermissionDialog), focus management between two stacked dialogs could be tricky. Mitigation: PermissionDialog is currently orphaned, so this won't happen in practice. If both are needed later, use Radix's `Modal` vs `NonModal` variants.

---

### Phase 4: ErrorBoundary — Leave As-Is (Recommended)

**File**: `web/src/components/common/ErrorBoundary.tsx`

This is a React class component (required for `componentDidCatch` / `getDerivedStateFromError`). shadcn components are functional. The options:

#### Option A: Leave As-Is (Recommended)
The error boundary is a safety net that renders when the app crashes. Using a shadcn component here creates a circular dependency — if shadcn's code is what crashed, the error boundary itself would fail. Keep the hand-rolled fallback.

#### Option B: Minimal Enhancement (Optional)
Replace the hand-rolled button with shadcn `<Button>`:

```tsx
import { Button } from "../ui/button";

// In render():
<Button onClick={() => { this.setState({ hasError: false, error: null }); window.location.reload(); }}>
  Reload
</Button>
```

This is safe because the Button component is a thin wrapper around `<button>` with cva variants — unlikely to be the crash source.

**Recommendation**: Option A — leave it alone. The error boundary is not a "dialog" and doesn't benefit from Radix primitives.

---

### Phase 5: StatusBar — No Changes

**File**: `web/src/components/common/StatusBar.tsx`

This is a simple flex bar showing model name, streaming indicator, error, theme toggle, and app name. It has no dialogs, modals, selects, or command palettes. **No shadcn migration needed.**

---

## Implementation Order

| Step | Action | Risk | Est. Time |
|------|--------|------|-----------|
| 1 | `npx shadcn@latest init` (Zinc, CSS vars) | Low | 5 min |
| 2 | `npx shadcn@latest add select dialog command button` | Low | 2 min |
| 3 | `npm run build` — verify tsc + vite pass | — | 2 min |
| 4 | Migrate ModelSelector → Select | Low | 15 min |
| 5 | Migrate CommandPalette → Command | Low | 15 min |
| 6 | Migrate PermissionDialog → Dialog + wire into app | Medium | 30 min |
| 7 | Manual QA: dark theme, keyboard nav, model select | — | 15 min |
| 8 | `npm run build` — verify final build | — | 2 min |
| **Total** | | | **~85 min** |

---

## Files Modified

| File | Action | Description |
|------|--------|-------------|
| `web/package.json` | Modified | New dependencies (radix, cmdk, cva, clsx, tailwind-merge, lucide-react) |
| `web/tailwind.config.ts` | Modified | Extended with CSS variable-based color tokens |
| `web/src/index.css` | Modified | Added `@layer base` with CSS variables |
| `web/src/lib/utils.ts` | **Created** | `cn()` helper |
| `web/src/components.json` | **Created** | shadcn configuration |
| `web/src/components/ui/button.tsx` | **Created** | shadcn Button |
| `web/src/components/ui/select.tsx` | **Created** | shadcn Select |
| `web/src/components/ui/dialog.tsx` | **Created** | shadcn Dialog |
| `web/src/components/ui/command.tsx` | **Created** | shadcn Command |
| `web/src/components/Sidebar/ModelSelector.tsx` | **Modified** | `<select>` → `<Select>` |
| `web/src/components/common/CommandPalette.tsx` | **Modified** | Hand-rolled → `<CommandDialog>` |
| `web/src/components/common/PermissionDialog.tsx` | **Modified** | Hand-rolled → `<Dialog>` + new `open`/`onClose` props |
| `web/src/App.tsx` | **Modified** | Wire PermissionDialog (conditional) |
| `web/src/stores/chatStore.tsx` | **Modified** | Add `permissionRequest` state (if wiring PermissionDialog) |

---

## Files NOT Modified

- `web/src/components/common/ErrorBoundary.tsx` — keep as-is (class component safety)
- `web/src/components/common/StatusBar.tsx` — no dialog/modal/select
- `web/src/components/Chat/*` — no relevant UI patterns
- `web/src/components/Sidebar/AgentTabs.tsx` — button group, not a dialog/select
- `web/src/components/Sidebar/SessionList.tsx` — list view, not a dialog/select
- `web/src/components/Files/FileTree.tsx` — not a dialog/select
- `web/src/components/Git/GitPanel.tsx` — not a dialog/select
- `web/src/hooks/*` — no changes
- `web/src/api/*` — no changes (unless adding tool approval endpoint)
- All Go source files — no changes

---

## Risk Assessment

### Low Risk
- **shadcn init**: Well-tested CLI, adds files without modifying existing code
- **ModelSelector**: Direct 1:1 mapping, Radix Select is mature
- **CommandPalette**: cmdk is the de facto standard for command palettes

### Medium Risk
- **PermissionDialog wiring**: Requires new state management in chatStore and potentially a new backend API endpoint. Can be deferred.
- **CSS variable coexistence**: New shadcn components use `bg-background`, existing code uses `bg-zinc-900`. Both work simultaneously. The only visual inconsistency is if CSS variable values don't exactly match zinc tones — verify with manual QA.

### Mitigations
- shadcn's Zinc base color preset maps directly to Tailwind's zinc palette
- Build verification at each step catches TypeScript errors early
- All existing classes continue working — no forced migration of existing components

---

## Validation Criteria

1. ✅ `npm run build` succeeds (tsc + vite)
2. ✅ ModelSelector renders with shadcn Select, model list populates, selecting a model dispatches correctly
3. ✅ CommandPalette opens on Cmd+K, search filters commands, keyboard nav works, selecting a command calls `onExecute`
4. ✅ PermissionDialog (if wired) shows as a proper modal dialog, approve/deny buttons work, overlay click dismisses
5. ✅ Dark theme looks identical to before (zinc tones match)
6. ✅ Light theme still works via theme toggle
7. ✅ Existing functionality in ChatPanel, SessionList, AgentTabs, StatusBar, FileTree, GitPanel unaffected
8. ✅ `dist/` output embeddable by Go binary (no new public assets)

---

## Rollback Strategy

If any phase introduces visual regressions or build failures:

1. **shadcn init issues**: Delete `components.json`, `src/lib/utils.ts`, `src/components/ui/`. Revert `tailwind.config.ts` and `index.css` from git.
2. **Component migration issues**: Revert the specific `.tsx` file from git. shadcn UI components in `src/components/ui/` can stay (harmless).
3. **Full rollback**: `git checkout -- web/` restores everything. The only non-reversible change is new entries in `package.json` — run `npm install` to clean up.
