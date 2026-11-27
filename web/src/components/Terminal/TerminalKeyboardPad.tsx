import { useState } from "react";
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Keyboard } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "../ui/popover";
import { softKeySequence } from "./terminalSoftKeyboard";

/** One shortcut exposed by the terminal's floating keyboard pad.
 *
 *  Clicking an item feeds its byte sequence to the pty through `term.input(..., true)` —
 *  the same typed-input path a hardware key uses — so the shell sees exactly what a
 *  real keyboard would send (arrows as CSI sequences in normal or application-cursor
 *  mode, ^C as \x03, Esc as \x1b).
 */
type Shortcut = {
  /** Key name for the escape-sequence table (arrows only); null for literal bytes. */
  keyName: string | null;
  /** Literal bytes written to the pty when `keyName` is null (^C, Esc). */
  bytes: string;
  /** Accessible name / visible text for the item. */
  label: string;
  /** On-screen icon; null means render the `label` as text instead. */
  icon: React.ElementType | null;
};

const SHORTCUTS: Shortcut[] = [
  { keyName: "ArrowUp", bytes: "", label: "Arrow up", icon: ArrowUp },
  { keyName: "ArrowDown", bytes: "", label: "Arrow down", icon: ArrowDown },
  { keyName: "ArrowLeft", bytes: "", label: "Arrow left", icon: ArrowLeft },
  { keyName: "ArrowRight", bytes: "", label: "Arrow right", icon: ArrowRight },
  { keyName: null, bytes: "\x03", label: "^C", icon: null },
  { keyName: null, bytes: "\x1b", label: "Esc", icon: null },
];

interface TerminalKeyboardPadProps {
  /** Getter for the live terminal instance; null until ghostty-web has attached. */
  getTerm: () => { input: (data: string, typed: boolean) => void; getMode: (id: number, defaultValue?: boolean) => boolean } | null;
  /** True once the terminal shell is attached and input is accepted (`shellReady`). */
  enabled: boolean;
  inputGuard: { noteInput: () => void };
}

/** Floating keyboard pad for the web terminal.
 *
 *  A `Keyboard`-icon button (same corner/style as the voice mic in the terminal's
 *  chrome bar) opens a pop-up menu of shortcut icons above it — an Android FAB
 *  speed-dial for keys a phone soft keyboard can't easily send (arrows, ^C, Esc).
 *  Pointer/mousedown is cancelled on both the trigger and each item so tapping
 *  keeps focus in the terminal and the mobile keyboard stays open.
 *
 *  Accessibility: plain labelled buttons in a popover (no `role="menu"`/`menuitem`),
 *  because the items have no arrow-key navigation — screen readers should not expect
 *  a menu widget they cannot traverse.
 */
export function TerminalKeyboardPad({ getTerm, enabled, inputGuard }: TerminalKeyboardPadProps) {
  const [open, setOpen] = useState(false);

  const send = (keyName: string | null, bytes: string) => {
    const term = getTerm();
    if (!enabled || !term) return;
    inputGuard.noteInput();
    if (keyName !== null) {
      const seq = softKeySequence(keyName, term.getMode(1, false));
      if (seq !== null) term.input(seq, true);
    } else {
      term.input(bytes, true);
    }
    setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          data-testid="terminal-keyboard-pad"
          aria-label="Keyboard shortcuts"
          aria-expanded={open}
          disabled={!enabled}
          onPointerDown={(e) => e.preventDefault()}
          onMouseDown={(e) => e.preventDefault()}
          className="flex h-10 w-10 items-center justify-center rounded-full border border-border bg-card/95 text-foreground shadow-lg transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50 disabled:pointer-events-none"
          title="Keyboard shortcuts"
        >
          <Keyboard className="h-5 w-5" aria-hidden="true" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="center"
        sideOffset={6}
        className="w-auto p-1 gap-1"
        // Keep focus in the terminal: Radix moves focus into the popover on open,
        // which would dismiss the phone keyboard.
        onOpenAutoFocus={(e: Event) => e.preventDefault()}
        onCloseAutoFocus={(e: Event) => e.preventDefault()}
      >
        {SHORTCUTS.map(({ keyName, bytes, label, icon: Icon }) => (
          <button
            key={label}
            type="button"
            aria-label={label}
            data-testid={`terminal-keyboard-${label.replace(/\s+/g, "-").toLowerCase()}`}
            onPointerDown={(e) => e.preventDefault()}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => send(keyName, bytes)}
            className="flex h-9 w-9 items-center justify-center rounded-md border border-border bg-card text-foreground shadow-sm transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            title={label}
          >
            {Icon ? (
              <Icon className="h-4 w-4" aria-hidden="true" />
            ) : (
              <span className="text-xs font-mono text-foreground">{label}</span>
            )}
          </button>
        ))}
      </PopoverContent>
    </Popover>
  );
}
