import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "../ui/dialog";
import { ScrollArea } from "../ui/scroll-area";
import { Separator } from "../ui/separator";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="inline-flex items-center justify-center rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[11px] leading-none text-muted-foreground">
      {children}
    </kbd>
  );
}

function ShortcutRow({ keys, desc }: { keys: React.ReactNode; desc: string }) {
  return (
    <div className="flex items-center justify-between gap-4 py-1.5 text-xs">
      <span className="text-muted-foreground">{desc}</span>
      <span className="flex items-center gap-1 shrink-0">{keys}</span>
    </div>
  );
}

export default function EditorHelpDialog({ open, onOpenChange }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl h-[75vh] p-0 overflow-hidden gap-0 flex flex-col">
        <DialogHeader className="px-6 py-4 border-b border-border shrink-0">
          <DialogTitle className="text-sm">Editor — How to use</DialogTitle>
          <DialogDescription className="text-xs">
            File preview editor powered by Monaco. Tips and keyboard shortcuts.
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className="flex-1 min-h-0">
          <div className="px-6 py-4 space-y-6">
            {/* How to use */}
            <section className="space-y-3">
              <h3 className="text-xs font-semibold uppercase tracking-wider text-foreground">How to use</h3>
              <ul className="space-y-2 text-xs leading-relaxed text-muted-foreground list-disc list-inside marker:text-muted-foreground/60">
                <li>
                  <span className="text-foreground font-medium">Open files</span> — click a file in the file tree on the left. Each file opens in its own tab above the editor; click tabs to switch, <Kbd>×</Kbd> to close.
                </li>
                <li>
                  <span className="text-foreground font-medium">Unsaved changes</span> — a dot on the tab means the file is dirty. Press <Kbd>⌘</Kbd> + <Kbd>S</Kbd> (or <Kbd>Ctrl</Kbd> + <Kbd>S</Kbd> on Windows/Linux) to save. A draft is also kept locally so switching tabs never loses edits.
                </li>
                <li>
                  <span className="text-foreground font-medium">Saving & session</span> — saving writes to disk immediately. If the agent is running, changes are tracked per-session so the <em>Changes</em> tab can show a diff.
                </li>
                <li>
                  <span className="text-foreground font-medium">Diff decorations</span> — when a session is active, added lines get a green gutter and deleted lines appear as red inline blocks after the affected line (with a copy button). They refresh on save.
                </li>
                <li>
                  <span className="text-foreground font-medium">External changes</span> — if the file is modified outside ocode while you have unsaved edits, an amber banner appears — <em>Reload from disk</em> discards your edits, <em>Dismiss</em> keeps them.
                </li>
                <li>
                  <span className="text-foreground font-medium">Scroll & selection</span> — scroll position is remembered per file. Drag to select a range; the selection is sent to the agent so it can scope edits.
                </li>
                <li>
                  <span className="text-foreground font-medium">Settings</span> — the <em>Settings</em> button in the editor header (or the gear in the toolbar) opens font size, tab size, word wrap, minimap, line numbers, theme, and extensions.
                </li>
              </ul>
            </section>

            <Separator />

            {/* App shortcuts */}
            <section className="space-y-3">
              <h3 className="text-xs font-semibold uppercase tracking-wider text-foreground">App shortcuts</h3>
              <div className="rounded-md border border-border divide-y divide-border">
                <div className="px-3">
                  <ShortcutRow keys={<><Kbd>⌘</Kbd><span className="text-muted-foreground/50">/</span><Kbd>Ctrl</Kbd> + <Kbd>S</Kbd></>} desc="Save file" />
                  <ShortcutRow keys={<><Kbd>⌘</Kbd><span className="text-muted-foreground/50">/</span><Kbd>Ctrl</Kbd> + <Kbd>P</Kbd></>} desc="Quick file picker" />
                  <ShortcutRow keys={<><Kbd>⌘</Kbd><span className="text-muted-foreground/50">/</span><Kbd>Ctrl</Kbd> + <Kbd>K</Kbd></>} desc="Command palette" />
                  <ShortcutRow keys={<><Kbd>⌘</Kbd><span className="text-muted-foreground/50">/</span><Kbd>Ctrl</Kbd> + <Kbd>W</Kbd></>} desc="Close editor tab (desktop only)" />
                </div>
              </div>
              <p className="text-[11px] text-muted-foreground/70">
                On macOS use <Kbd>⌘</Kbd>; on Windows/Linux use <Kbd>Ctrl</Kbd>. <Kbd>⌘</Kbd> + <Kbd>W</Kbd> is only active inside the ocode desktop app (a browser tab would close otherwise).
              </p>
            </section>

            <Separator />

            {/* Editor shortcuts */}
            <section className="space-y-3">
              <h3 className="text-xs font-semibold uppercase tracking-wider text-foreground">Editor shortcuts (Monaco)</h3>
              <p className="text-xs text-muted-foreground">
                These work while the editor is focused. Press <Kbd>F1</Kbd> inside the editor to open the full command palette with every action.
              </p>
              <div className="rounded-md border border-border divide-y divide-border">
                <div className="px-3">
                  <ShortcutRow keys={<><Kbd>F1</Kbd> <span className="text-muted-foreground/50">or</span> <Kbd>⇧</Kbd> + <Kbd>⌘</Kbd> + <Kbd>P</Kbd></>} desc="Command palette (all editor actions)" />
                  <ShortcutRow keys={<><Kbd>⌘</Kbd><span className="text-muted-foreground/50">/</span><Kbd>Ctrl</Kbd> + <Kbd>F</Kbd></>} desc="Find" />
                  <ShortcutRow keys={<><Kbd>⌘</Kbd><span className="text-muted-foreground/50">/</span><Kbd>Ctrl</Kbd> + <Kbd>H</Kbd></>} desc="Find & replace" />
                  <ShortcutRow keys={<><Kbd>⌘</Kbd> + <Kbd>G</Kbd> <span className="text-muted-foreground/50">/</span> <Kbd>⇧</Kbd> + <Kbd>⌘</Kbd> + <Kbd>G</Kbd></>} desc="Next / previous match" />
                  <ShortcutRow keys={<><Kbd>⌘</Kbd><span className="text-muted-foreground/50">/</span><Kbd>Ctrl</Kbd> + <Kbd>/</Kbd></>} desc="Toggle line comment" />
                  <ShortcutRow keys={<><Kbd>⇧</Kbd> + <Kbd>Alt</Kbd> + <Kbd>F</Kbd></>} desc="Format document" />
                  <ShortcutRow keys={<><Kbd>⌘</Kbd><span className="text-muted-foreground/50">/</span><Kbd>Ctrl</Kbd> + <Kbd>Z</Kbd></>} desc="Undo" />
                  <ShortcutRow keys={<><Kbd>⇧</Kbd> + <Kbd>⌘</Kbd> + <Kbd>Z</Kbd> <span className="text-muted-foreground/50">or</span> <Kbd>Ctrl</Kbd> + <Kbd>Y</Kbd></>} desc="Redo" />
                  <ShortcutRow keys={<><Kbd>⌘</Kbd> + <Kbd>D</Kbd></>} desc="Add next occurrence to selection" />
                  <ShortcutRow keys={<><Kbd>Alt</Kbd> + <Kbd>Click</Kbd></>} desc="Add cursor (multi-cursor)" />
                  <ShortcutRow keys={<><Kbd>Alt</Kbd> + <Kbd>↑</Kbd> <span className="text-muted-foreground/50">/</span> <Kbd>↓</Kbd></>} desc="Move line up / down" />
                  <ShortcutRow keys={<><Kbd>⇧</Kbd> + <Kbd>Alt</Kbd> + <Kbd>↑</Kbd> <span className="text-muted-foreground/50">/</span> <Kbd>↓</Kbd></>} desc="Copy line up / down" />
                  <ShortcutRow keys={<><Kbd>⌘</Kbd> + <Kbd>L</Kbd></>} desc="Select current line" />
                  <ShortcutRow keys={<><Kbd>⌘</Kbd> + <Kbd>]</Kbd> <span className="text-muted-foreground/50">/</span> <Kbd>[</Kbd></>} desc="Indent / outdent line" />
                  <ShortcutRow keys={<><Kbd>Ctrl</Kbd> + <Kbd>G</Kbd></>} desc="Go to line" />
                  <ShortcutRow keys={<><Kbd>⌘</Kbd> + <Kbd>K</Kbd> <Kbd>⌘</Kbd> + <Kbd>S</Kbd></>} desc="Keyboard shortcuts reference (monaco)" />
                </div>
              </div>
              <p className="text-[11px] text-muted-foreground/70">
                Tip: most shortcuts support <Kbd>⌘</Kbd> on macOS and <Kbd>Ctrl</Kbd> on Windows/Linux interchangeably. See <Kbd>F1</Kbd> for the full list — it is searchable.
              </p>
            </section>

            <Separator />

            <p className="text-[11px] text-muted-foreground/60">
              Monaco editor · Changes from <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">internal/changes</code> · Settings stored via <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">/api/monaco/*</code>
            </p>
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}
