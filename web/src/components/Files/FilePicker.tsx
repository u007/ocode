import { useEffect, useState } from "react";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "../ui/command";
import { apiPath, authHeaders } from "../../api/client";

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
  onOpenFile: (path: string) => void;
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

export default function FilePicker({ open, onClose, onOpenFile }: Props) {
  const [files, setFiles] = useState<string[]>([]);

  useEffect(() => {
    if (!open) return;
    // depth=0 asks the server for the full tree (no depth cap) so files
    // nested deep in the project are searchable, not just the shallow ones.
    fetch(apiPath(`/api/files/tree?depth=0`), { headers: authHeaders() })
      .then((res) => res.json())
      .then((data: FileTreeResponse) => {
        if (data.truncated) {
          console.warn("File tree truncated; not all files are searchable");
        }
        setFiles(flattenFiles(data.children));
      })
      .catch((err) => console.error("Failed to load file tree:", err));
  }, [open]);

  return (
    <CommandDialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <CommandInput placeholder="Search files..." />
      <CommandList>
        <CommandEmpty>No files found</CommandEmpty>
        <CommandGroup heading="Files">
          {files.map((path) => (
            <CommandItem
              key={path}
              value={path}
              onSelect={() => {
                onOpenFile(path);
                onClose();
              }}
            >
              <span className="font-mono text-sm">{path}</span>
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
