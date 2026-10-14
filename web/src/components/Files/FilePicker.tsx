import { useVirtualizer } from "@tanstack/react-virtual";
import { Eye, EyeOff, Folder } from "lucide-react";
import { useEffect, useState, useMemo, useRef } from "react";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "../ui/command";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "../ui/dialog";
import { apiPath, authHeaders } from "../../api/client";
import { parseKeywords, matchesKeywords, scoreMatch } from "../../lib/keywordFilter";
import { compareByShortestPath } from "../../lib/filePathOrder";
import { loadShowHiddenFiles, saveShowHiddenFiles, subscribeShowHiddenFiles, showHiddenFilesProjectKey } from "./showHiddenFilesPersistence";

interface FileNode {
  name: string;
  path: string;
  is_dir: boolean;
  children?: FileNode[];
}

interface FileTreeResponse {
  children: FileNode[];
  truncated: boolean;
}

interface Props {
  open: boolean;
  onClose: () => void;
  onOpenFile: (path: string, projectRoot?: string) => void;
  projectPath?: string;
  /** Registered remote target for the project; routes the tree fetch to
   *  the remote host (?host=). Without it a remote project's picker lists
   *  the server-local tree instead. */
  projectHost?: string;
  /** Query prefilled into the filter when the dialog opens. Used by the
   *  "file link didn't resolve" fallback, which seeds the basename so the
   *  user can find where the file actually lives. */
  initialQuery?: string;
  /** When set, folders are listed alongside files and selecting one calls
   *  this instead of `onOpenFile`. Left unset, the picker lists files only. */
  onOpenFolder?: (path: string, projectRoot?: string) => void;
}

interface FlatTree {
  files: string[];
  folders: string[];
}

function flattenTree(nodes: FileNode[], out: FlatTree = { files: [], folders: [] }): FlatTree {
  for (const n of nodes) {
    if (n.is_dir) {
      out.folders.push(n.path);
      if (n.children) flattenTree(n.children, out);
    } else {
      out.files.push(n.path);
    }
  }
  return out;
}

export default function FilePicker({ open, onClose, onOpenFile, onOpenFolder, projectPath, projectHost, initialQuery }: Props) {
  const [tree, setTree] = useState<FlatTree>({ files: [], folders: [] });
  const [query, setQuery] = useState("");
  // The choice is per project (and remote host); follow a project switch.
  const hiddenProjectKey = showHiddenFilesProjectKey(projectPath, projectHost);
  const [showHiddenFiles, setShowHiddenFiles] = useState(() => loadShowHiddenFiles(hiddenProjectKey));

  useEffect(() => {
    setShowHiddenFiles(loadShowHiddenFiles(hiddenProjectKey));
  }, [hiddenProjectKey]);

  // Keep the picker and the Files-tab tree in sync for the active project.
  useEffect(
    () => subscribeShowHiddenFiles(hiddenProjectKey, setShowHiddenFiles),
    [hiddenProjectKey],
  );

  const toggleShowHiddenFiles = () => {
    const next = !showHiddenFiles;
    saveShowHiddenFiles(hiddenProjectKey, next);
    setShowHiddenFiles(next);
  };

  useEffect(() => {
    if (!open) return;
    if (!projectPath) {
      setTree({ files: [], folders: [] });
      return;
    }
    const controller = new AbortController();
    // depth=0 asks the server for the full tree (no depth cap) so files
    // nested deep in the project are searchable, not just the shallow ones.
    // An explicit path anchors the request to the active project instead of
    // the server's own workDir (fixed at launch, e.g. home dir on desktop).
    const query = `path=${encodeURIComponent(projectPath)}&depth=0&show_hidden=${showHiddenFiles ? "1" : "0"}${projectHost ? `&host=${encodeURIComponent(projectHost)}` : ""}`;
    fetch(apiPath(`/api/files/tree?${query}`), { headers: authHeaders(), signal: controller.signal })
      .then((res) => res.json())
      .then((data: FileTreeResponse) => {
        if (controller.signal.aborted) return;
        if (data.truncated) {
          console.warn("File tree truncated; not all files are searchable");
        }
        setTree(flattenTree(data.children));
      })
      .catch((err) => {
        if ((err as Error).name !== "AbortError") console.error("Failed to load file tree:", err);
      });
    return () => controller.abort();
  }, [open, projectPath, projectHost, showHiddenFiles]);

  useEffect(() => {
    if (!open) setQuery("");
    else setQuery(initialQuery ?? "");
  }, [open, initialQuery]);

  const keywords = useMemo(() => parseKeywords(query), [query]);
  // Folder rows only exist when the caller can act on them. A path is either a
  // file or a folder, never both, so membership alone tells the rows apart.
  // The memos key on the boolean, not on the callback: callers pass an inline
  // arrow, and a new identity every render would re-sort the whole listing
  // (even while the picker is closed).
  const listFolders = !!onOpenFolder;
  const entries = useMemo(
    () => (listFolders ? [...tree.files, ...tree.folders] : tree.files),
    [tree, listFolders],
  );
  const folderPaths = useMemo(() => new Set(listFolders ? tree.folders : []), [tree, listFolders]);
  const filteredFiles = useMemo(() => {
    // Unfiltered: shortest path first so root-level files surface above deep ones.
    if (keywords.length === 0) return [...entries].sort(compareByShortestPath);
    // Filtered: relevance first, then shortest path as the tiebreaker.
    return entries
      .filter((p) => matchesKeywords(p, keywords))
      .sort((a, b) => {
        const scoreA = scoreMatch(a, keywords);
        const scoreB = scoreMatch(b, keywords);
        if (scoreA !== scoreB) return scoreB - scoreA;
        return compareByShortestPath(a, b);
      });
  }, [entries, keywords]);

  const listRef = useRef<HTMLDivElement>(null);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: filteredFiles.length,
    getScrollElement: () => scrollContainerRef.current,
    estimateSize: () => 42,
    overscan: 8,
  });

  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="overflow-hidden p-0 shadow-lg">
        <DialogHeader className="sr-only">
          <DialogTitle>Open a project file</DialogTitle>
          <DialogDescription>Search for a file to open in the editor.</DialogDescription>
        </DialogHeader>
        {/* shouldFilter=false disables cmdk's built-in fuzzy filter so our
            shared whitespace-AND keyword semantics (case-insensitive, matches
            name/path) apply uniformly across all file managers. */}
        <Command
          shouldFilter={false}
          className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-muted-foreground [&_[cmdk-group]:not([hidden])_~[cmdk-group]]:pt-0 [&_[cmdk-group]]:px-2 [&_[cmdk-input-wrapper]_svg]:h-5 [&_[cmdk-input-wrapper]_svg]:w-5 [&_[cmdk-input]]:h-12 [&_[cmdk-item]]:px-2 [&_[cmdk-item]]:py-3 [&_[cmdk-item]_svg]:h-5 [&_[cmdk-item]_svg]:w-5"
        >
          <div className="flex items-center gap-1 px-2 pt-1">
            <button
              type="button"
              onClick={toggleShowHiddenFiles}
              aria-pressed={showHiddenFiles}
              className={`flex items-center gap-1 text-[10px] px-2 py-0.5 rounded border transition-colors ${showHiddenFiles ? "bg-amber-500/20 text-amber-600 border-amber-500/30" : "bg-transparent text-muted-foreground border-border hover:bg-muted"}`}
              title={showHiddenFiles ? "Showing hidden/ignored files (click to hide)" : "Showing normal files only (click to show hidden/ignored)"}
            >
              {showHiddenFiles ? <Eye className="w-3 h-3" /> : <EyeOff className="w-3 h-3" />}
              <span>Hidden</span>
            </button>
          </div>
          <CommandInput placeholder="Filter by keywords..." value={query} onValueChange={setQuery} />
          <CommandList ref={scrollContainerRef}>
            <CommandEmpty>{keywords.length > 0 ? "No matching files" : "No files found"}</CommandEmpty>
            <CommandGroup
              heading={`Files${keywords.length > 0 ? ` — ${filteredFiles.length} match${filteredFiles.length === 1 ? "" : "es"}${filteredFiles.length !== entries.length ? ` of ${entries.length}` : ""}` : ""}`}
            >
              <div
                ref={listRef}
                style={{ height: virtualizer.getTotalSize(), width: "100%", position: "relative" }}
              >
                {virtualizer.getVirtualItems().map((item) => {
                  const path = filteredFiles[item.index];
                  const isFolder = folderPaths.has(path);
                  return (
                    <div
                      key={item.key}
                      style={{
                        position: "absolute",
                        top: 0,
                        left: 0,
                        width: "100%",
                        transform: `translateY(${item.start}px)`,
                        height: item.size,
                      }}
                    >
                      <CommandItem
                        value={path}
                        onSelect={() => {
                          if (isFolder) onOpenFolder?.(path, projectPath);
                          else onOpenFile(path, projectPath);
                          onClose();
                        }}
                      >
                        {isFolder && <Folder className="shrink-0 text-muted-foreground" />}
                        <span className="font-mono text-sm">{path}</span>
                        {isFolder && <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">folder</span>}
                      </CommandItem>
                    </div>
                  );
                })}
              </div>
            </CommandGroup>
          </CommandList>
        </Command>
      </DialogContent>
    </Dialog>
  );
}
