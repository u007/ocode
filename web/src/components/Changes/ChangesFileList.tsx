import { useState } from "react";
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "@/components/ui/tooltip";
import type { FileChange } from "@/api/types";

const STATUS_BADGES: Record<string, { label: string; color: string }> = {
  added: { label: "+", color: "bg-green-500/20 text-green-400" },
  modified: { label: "M", color: "bg-yellow-500/20 text-yellow-400" },
  deleted: { label: "-", color: "bg-red-500/20 text-red-400" },
};

interface Props {
  files: FileChange[];
  selectedPath: string | null;
  onSelect: (path: string) => void;
  onUndoFile: (path: string) => void;
  onUndoBlock: (path: string) => void;
}

export default function ChangesFileList({ files, selectedPath, onSelect, onUndoFile, onUndoBlock }: Props) {
  const [expanded, setExpanded] = useState<string | null>(null);

  if (files.length === 0) {
    return <div className="p-3 text-xs text-foreground">No changes in this session yet.</div>;
  }

  return (
    <TooltipProvider>
      <div className="divide-y divide-border">
        {files.map((file) => {
          const badge = STATUS_BADGES[file.status] || STATUS_BADGES.modified;
          const isSelected = selectedPath === file.originalPath;
          const isExpanded = expanded === file.originalPath;
          const authorSummary = file.authors
            .map((a) => `${a.agentName} · ${a.changes}`)
            .join(", ");
          return (
            <div key={file.originalPath} className={isSelected ? "bg-muted" : ""}>
              <button
                onClick={() => onSelect(file.originalPath)}
                className="w-full text-left px-3 py-1.5 flex items-center gap-2 text-sm hover:bg-muted/50"
              >
                <span className={`inline-flex items-center justify-center w-5 h-5 rounded text-[10px] font-bold ${badge.color}`}>
                  {badge.label}
                </span>
                <span className="font-mono text-foreground truncate flex-1">{file.originalPath}</span>
                {!file.undoable && (
                  <span className="text-[10px] text-muted-foreground shrink-0">(bash) ⚠</span>
                )}
              </button>
              <div className="px-3 pb-1.5 flex items-center gap-3 text-[11px] text-muted-foreground">
                <span>{authorSummary}</span>
                <button
                  onClick={() => setExpanded(isExpanded ? null : file.originalPath)}
                  className="hover:text-foreground"
                >
                  {isExpanded ? "hide details" : "details"}
                </button>
                <div className="flex-1" />
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>
                        <button
                          disabled={!file.undoable}
                          onClick={() => onUndoBlock(file.originalPath)}
                          className="hover:text-foreground disabled:opacity-40 disabled:hover:text-muted-foreground"
                        >
                          undo last
                        </button>
                      </span>
                    </TooltipTrigger>
                    {!file.undoable && (
                      <TooltipContent>bash-only change — no backup to restore</TooltipContent>
                    )}
                  </Tooltip>
                </TooltipProvider>
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>
                        <button
                          disabled={!file.undoable}
                          onClick={() => onUndoFile(file.originalPath)}
                          className="hover:text-foreground disabled:opacity-40 disabled:hover:text-muted-foreground"
                        >
                          undo file
                        </button>
                      </span>
                    </TooltipTrigger>
                    {!file.undoable && (
                      <TooltipContent>bash-only change — no backup to restore</TooltipContent>
                    )}
                  </Tooltip>
                </TooltipProvider>
              </div>
              {isExpanded && (
                <div className="px-3 pb-2 text-[11px] text-muted-foreground space-y-0.5">
                  <div>{file.changeCount} tool call(s)</div>
                  {file.lastBashCommand && <div className="font-mono">$ {file.lastBashCommand}</div>}
                  <div>created {file.createdAt}</div>
                  <div>updated {file.updatedAt}</div>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </TooltipProvider>
  );
}
