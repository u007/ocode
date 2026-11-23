import { useEffect, useRef, useState } from "react";
import { pathToNode, rowLabel, type DomNode } from "../../lib/domInspector";

export interface DomTreeProps {
  root: DomNode;
  selectedId: string | null;
  truncated: boolean;
  onSelectNode?: (id: string | null) => void;
  onHoverNode?: (id: string | null) => void;
}

/**
 * Collapsible element tree for the DevConsole DOM tab. Expansion is local view
 * state; the selection is owned by the parent so the page surface can outline
 * the same node. Children are only rendered for expanded rows, which keeps a
 * 5000-node tree cheap.
 */
export function DomTree({ root, selectedId, truncated, onSelectNode, onHoverNode }: DomTreeProps) {
  // The root starts expanded: a lone `html` row is not a useful first view.
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set([root.id]));
  const rowRefs = useRef<Map<string, HTMLDivElement>>(new Map());

  // Reveal the selection by expanding its ancestors, so its row exists.
  useEffect(() => {
    if (!selectedId) return;
    const chain = pathToNode(root, selectedId);
    if (!chain) return;
    setExpanded((prev) => {
      const next = new Set(prev);
      let changed = false;
      for (const ancestor of chain.slice(0, -1)) {
        if (!next.has(ancestor.id)) {
          next.add(ancestor.id);
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [root, selectedId]);

  // Runs after the reveal render. Not a focus() call on purpose: moving focus
  // would take the caret away from whatever the user was typing in.
  useEffect(() => {
    if (!selectedId) return;
    rowRefs.current.get(selectedId)?.scrollIntoView?.({ block: "nearest" });
  }, [root, selectedId, expanded]);

  const toggle = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const renderRow = (node: DomNode, depth: number): React.ReactNode => {
    const kids = node.children ?? [];
    const hasKids = kids.length > 0;
    const isOpen = expanded.has(node.id);
    const isSelected = node.id === selectedId;
    return (
      <div key={node.id}>
        <div
          ref={(el) => {
            if (el) rowRefs.current.set(node.id, el);
            else rowRefs.current.delete(node.id);
          }}
          role="treeitem"
          aria-level={depth + 1}
          aria-expanded={hasKids ? isOpen : undefined}
          aria-selected={isSelected}
          onClick={() => onSelectNode?.(node.id)}
          onMouseEnter={() => onHoverNode?.(node.id)}
          onMouseLeave={() => onHoverNode?.(null)}
          style={{ paddingLeft: 4 + depth * 12 }}
          className={`flex items-center gap-1 py-px pr-2 cursor-pointer ${
            isSelected ? "bg-sky-900/40 text-sky-100" : "hover:bg-neutral-800/40 text-neutral-300"
          }`}
        >
          {hasKids ? (
            <button
              type="button"
              aria-label={`${isOpen ? "Collapse" : "Expand"} ${node.tag}`}
              onClick={(e) => {
                e.stopPropagation();
                toggle(node.id);
              }}
              className="w-3 shrink-0 text-neutral-500 hover:text-neutral-200 select-none"
            >
              {isOpen ? "▾" : "▸"}
            </button>
          ) : (
            <span className="w-3 shrink-0" />
          )}
          {/* Text only: page content is untrusted, never rendered as HTML. */}
          <span className={`truncate ${node.tag === "#text" ? "text-neutral-500 italic" : ""}`}>{rowLabel(node)}</span>
        </div>
        {hasKids && isOpen && kids.map((kid) => renderRow(kid, depth + 1))}
      </div>
    );
  };

  return (
    <div role="tree" aria-label="DOM tree">
      {renderRow(root, 0)}
      {truncated && (
        <div className="px-2 py-1 text-[10px] text-yellow-400">Tree truncated — use the picker for the rest</div>
      )}
    </div>
  );
}
