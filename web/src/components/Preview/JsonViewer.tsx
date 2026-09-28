import { useState } from "react";
import { ChevronRight } from "lucide-react";

/**
 * JSON preview renderer for the Files-tab Split view.
 *
 * Renders a collapsible tree view of parsed JSON. Invalid JSON (common
 * while typing in split mode) shows an inline parse-error message — the
 * preview never throws. Arrays and objects are collapsed by default;
 * click a row to expand/collapse.
 */

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function TreeNode({ nodeKey, value }: { nodeKey: string; value: unknown }) {
  const [open, setOpen] = useState(false);
  const isExpandable = isObject(value) || Array.isArray(value);
  const childCount = isExpandable ? Object.keys(value).length : 0;
  const preview = Array.isArray(value)
    ? `[${childCount} item${childCount !== 1 ? "s" : ""}]`
    : isObject(value)
      ? `{${childCount} key${childCount !== 1 ? "s" : ""}}`
      : String(value);

  if (!isExpandable) {
    return (
      <div className="flex gap-1 py-0.5 pl-4 text-xs">
        <span className="shrink-0 text-muted-foreground">{nodeKey}:</span>
        <span className="break-all text-foreground">{preview}</span>
      </div>
    );
  }

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-1 rounded px-1 py-0.5 text-left text-xs hover:bg-muted"
        aria-expanded={open}
      >
        <ChevronRight
          className={`h-3 w-3 shrink-0 text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
        <span className="shrink-0 text-muted-foreground">{nodeKey}:</span>
        <span className="text-foreground">{preview}</span>
      </button>
      {open && (
        <div className="ml-2 border-l border-border pl-2">
          {Object.entries(value).map(([k, v]) => (
            <TreeNode key={k} nodeKey={k} value={v} />
          ))}
        </div>
      )}
    </div>
  );
}

export default function JsonViewer({ content }: { content: string }) {
  let parsed: unknown;
  let error: string | null = null;

  try {
    parsed = JSON.parse(content);
  } catch (e) {
    error = e instanceof Error ? e.message : "Invalid JSON";
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center p-4 text-xs text-muted-foreground">
        <div className="rounded border border-border bg-muted/50 p-3 text-center">
          <div className="mb-1 text-foreground">Invalid JSON</div>
          <div className="max-w-md break-all">{error}</div>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full overflow-auto p-2 font-mono">
      <TreeNode key="root" nodeKey="" value={parsed} />
    </div>
  );
}
