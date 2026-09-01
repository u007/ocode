import { useEffect, useState } from "react";
import type { BrowseMode } from "../../lib/browserStore";

export interface AddressBarProps {
  url: string;
  status: number; // 0 = loading/no response yet
  mode: BrowseMode | null; // null before the first server nav event
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
// never a value reported by the proxied page or the screencast — page JS
// could spoof it.
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
      {mode && <span className="px-1 text-xs uppercase text-neutral-400">{mode}</span>}
      {error && <span className="px-1 text-xs text-red-500 truncate max-w-48">{error}</span>}
      <button aria-label="Open externally" onClick={props.onOpenExternal} className="px-1">↗</button>
    </div>
  );
}
