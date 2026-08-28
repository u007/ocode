import { useEffect, useState, useMemo } from "react";
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
import { parseKeywords, matchesKeywords } from "../../lib/keywordFilter";

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
}

function flattenFiles(nodes: FileNode[]): string[] {
  const out: string[] = [];
  for (const n of nodes) {
    if (n.is_dir) {
      if (n.children) out.push(...flattenFiles(n.children));
    } else {
      out.push(n.path);
    }
  }
  return out;
}

export default function FilePicker({ open, onClose, onOpenFile, projectPath }: Props) {
  const [files, setFiles] = useState<string[]>([]);
  const [query, setQuery] = useState("");

  useEffect(() => {
    if (!open) return;
    if (!projectPath) {
      setFiles([]);
      return;
    }
    const controller = new AbortController();
    // depth=0 asks the server for the full tree (no depth cap) so files
    // nested deep in the project are searchable, not just the shallow ones.
    // An explicit path anchors the request to the active project instead of
    // the server's own workDir (fixed at launch, e.g. home dir on desktop).
    const query = `path=${encodeURIComponent(projectPath)}&depth=0`;
    fetch(apiPath(`/api/files/tree?${query}`), { headers: authHeaders(), signal: controller.signal })
      .then((res) => res.json())
      .then((data: FileTreeResponse) => {
        if (controller.signal.aborted) return;
        if (data.truncated) {
          console.warn("File tree truncated; not all files are searchable");
        }
        setFiles(flattenFiles(data.children));
      })
      .catch((err) => {
        if ((err as Error).name !== "AbortError") console.error("Failed to load file tree:", err);
      });
    return () => controller.abort();
  }, [open, projectPath]);

  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const keywords = useMemo(() => parseKeywords(query), [query]);
  const filteredFiles = useMemo(() => {
    if (keywords.length === 0) return files;
    return files.filter((p) => matchesKeywords(p, keywords));
  }, [files, keywords]);

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
          <CommandInput placeholder="Filter by keywords..." value={query} onValueChange={setQuery} />
          <CommandList>
            <CommandEmpty>{keywords.length > 0 ? "No matching files" : "No files found"}</CommandEmpty>
            <CommandGroup
              heading={`Files${keywords.length > 0 ? ` — ${filteredFiles.length} match${filteredFiles.length === 1 ? "" : "es"}${filteredFiles.length !== files.length ? ` of ${files.length}` : ""}` : ""}`}
            >
              {filteredFiles.map((path) => (
                <CommandItem
                  key={path}
                  value={path}
                  onSelect={() => {
                    onOpenFile(path, projectPath);
                    onClose();
                  }}
                >
                  <span className="font-mono text-sm">{path}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </DialogContent>
    </Dialog>
  );
}
