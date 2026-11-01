import { useEffect, useState } from "react";
import { formatTokens } from "../format";
import { contextLevel } from "../gauge";
import { filterModels, type ModelInfo } from "../models";
import type { ModeChoice } from "../modes";
import type { WorkbookInfo } from "../port";

const MODES: { id: ModeChoice; label: string }[] = [
  { id: "ask", label: "Ask" },
  { id: "auto", label: "Auto" },
  { id: "yolo", label: "YOLO" },
];

/** Closes a popover on a pointer press outside `box` and on Escape, while it is open. */
export function useDismissable(box: HTMLElement | null, open: boolean, onClose: () => void) {
  useEffect(() => {
    if (!open) return;
    const press = (e: PointerEvent) => { if (box && e.target instanceof Node && !box.contains(e.target)) onClose(); };
    const key = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    document.addEventListener("pointerdown", press);
    document.addEventListener("keydown", key);
    return () => {
      document.removeEventListener("pointerdown", press);
      document.removeEventListener("keydown", key);
    };
  }, [box, open, onClose]);
}

/** Ask | Auto | YOLO. YOLO is red while it is the active choice. */
export function ModeControl({ value, onPick }: { value: ModeChoice; onPick: (m: ModeChoice) => void }) {
  return (
    <div className="seg" role="group" aria-label="Permission mode">
      {MODES.map((m) => (
        <button
          key={m.id}
          aria-pressed={value === m.id}
          className={m.id === "yolo" && value === "yolo" ? "danger" : undefined}
          onClick={() => onPick(m.id)}
        >{m.label}</button>
      ))}
    </div>
  );
}

/** The model the next message uses, chosen from the providers ocode reports as connected. */
export function ModelPicker({ current, models, onPick }: { current: string | undefined; models: ModelInfo[]; onPick: (name: string) => void }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [box, setBox] = useState<HTMLDivElement | null>(null);
  const close = () => setOpen(false);
  useDismissable(box, open, close);
  const { rows, total } = filterModels(models, query);
  // No pick yet: the next chat uses ocode's own default model.
  const label = current === undefined ? "Server default" : current;
  return (
    <div className="model" ref={setBox}>
      <button className="model-btn" aria-haspopup="listbox" aria-expanded={open} title={label} onClick={() => setOpen(!open)}>
        {label}
      </button>
      {open && (
        <div className="pop">
          <input autoFocus placeholder="Filter connected models" aria-label="Filter connected models" value={query} onChange={(e) => setQuery(e.target.value)} />
          {models.length === 0 && <div className="muted">No connected models. Connect a provider in ocode, then reload the pane.</div>}
          <ul role="listbox" aria-label="Connected models">
            {rows.map((m) => (
              <li key={m.name}>
                <button role="option" aria-selected={m.name === current} onClick={() => { onPick(m.name); close(); }}>
                  <span>{m.display_name}</span><span className="muted">{m.name}</span>
                </button>
              </li>
            ))}
          </ul>
          {total > rows.length && <div className="muted">Showing {rows.length} of {total}. Type to narrow the list.</div>}
        </div>
      )}
    </div>
  );
}

/** Small chips for the open file's state. Only what the host confirmed is shown; read-only and protected are amber. */
export function FileChips({ info }: { info: WorkbookInfo | null }) {
  if (!info) return null;
  const chips: { label: string; warn: boolean }[] = [];
  if (info.state !== "unknown") chips.push({ label: info.state, warn: false });
  if (info.blank === true) chips.push({ label: "blank", warn: false });
  if (info.readOnly === true) chips.push({ label: "read-only", warn: true });
  if (info.protectedView === true) chips.push({ label: "protected", warn: true });
  return <>{chips.map((c) => <span key={c.label} className={c.warn ? "chip warn" : "chip"}>{c.label}</span>)}</>;
}

/** Thin bar and "12.4k / 1M" for the open session's context window. Amber from 70%, red from 90%. */
export function ContextGauge({ current, max, model }: { current: number; max: number; model: string }) {
  const pct = Math.min(100, (current / max) * 100);
  const level = contextLevel(current, max);
  return (
    <div className={`gauge ${level}`} title={`${current.toLocaleString()} of ${max.toLocaleString()} tokens (${pct.toFixed(1)}%) on ${model}`}>
      <span className="bar" role="meter" aria-label="Context window" aria-valuemin={0} aria-valuemax={max} aria-valuenow={current}>
        <span className="fill" style={{ width: `${pct}%` }} />
      </span>
      <span className="muted">{formatTokens(current)} / {formatTokens(max)}</span>
    </div>
  );
}

export interface MenuItem {
  label: string;
  disabled: boolean;
  onSelect: () => void;
  /** Present for a setting: a checkbox item that shows a checkmark when true. Omit for a plain action. */
  checked?: boolean;
  /** One quiet line under the label, for what the item changes. */
  note?: string;
}

/** The ⋯ button and its list. Closes on an outside press, on Escape, and after a choice. */
export function OverflowMenu({ items }: { items: MenuItem[] }) {
  const [open, setOpen] = useState(false);
  const [box, setBox] = useState<HTMLDivElement | null>(null);
  const close = () => setOpen(false);
  useDismissable(box, open, close);
  return (
    <div className="menu" ref={setBox}>
      <button aria-label="More actions" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}>⋯</button>
      {open && (
        <div className="pop menu-pop" role="menu">
          {items.map((it) => it.checked === undefined ? (
            <button key={it.label} role="menuitem" disabled={it.disabled} onClick={() => { close(); it.onSelect(); }}>{it.label}</button>
          ) : (
            <button key={it.label} role="menuitemcheckbox" aria-checked={it.checked} className="check-item" disabled={it.disabled} onClick={() => { close(); it.onSelect(); }}>
              <span className="check" aria-hidden="true">{it.checked ? "✓" : ""}</span>{it.label}
              {it.note && <span className="muted">{it.note}</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
