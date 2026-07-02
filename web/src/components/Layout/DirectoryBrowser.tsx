import { useState, useEffect, useCallback } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { ScrollArea } from "../ui/scroll-area";
import { Loader2, Folder, ArrowUp, Home } from "lucide-react";
import { api } from "../../api/client";
import type { DirectoryEntry } from "../../api/types";
import { cn } from "../../lib/utils";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSelect: (path: string) => void;
}

export default function DirectoryBrowser({ open, onOpenChange, onSelect }: Props) {
  const [currentPath, setCurrentPath] = useState("");
  const [parentPath, setParentPath] = useState("");
  const [directories, setDirectories] = useState<DirectoryEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [selectedPath, setSelectedPath] = useState("");

  const browse = useCallback(async (path?: string) => {
    setLoading(true);
    setError("");
    try {
      const res = await api.browseDirectory(path);
      setCurrentPath(res.current_path);
      setParentPath(res.parent_path);
      setDirectories(res.directories);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to browse directory");
      setDirectories([]);
    } finally {
      setLoading(false);
    }
  }, []);

  // Start at roots when dialog opens
  useEffect(() => {
    if (open) {
      setSelectedPath("");
      browse();
    }
  }, [open, browse]);

  const navigateInto = (entry: DirectoryEntry) => {
    setSelectedPath(entry.path);
    browse(entry.path);
  };

  const navigateUp = () => {
    if (parentPath) {
      setSelectedPath(parentPath);
      browse(parentPath);
    }
  };

  const navigateToHome = () => {
    setSelectedPath("");
    browse();
  };

  const handleConfirm = () => {
    if (selectedPath) {
      onSelect(selectedPath);
      onOpenChange(false);
    }
  };

  const handleDoubleClick = (entry: DirectoryEntry) => {
    navigateInto(entry);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && selectedPath) {
      handleConfirm();
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Browse for Folder</DialogTitle>
        </DialogHeader>

        {/* Current path bar */}
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            className="h-8 w-8 p-0 shrink-0"
            onClick={navigateToHome}
            title="Show root directories"
          >
            <Home className="w-4 h-4" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="h-8 w-8 p-0 shrink-0"
            disabled={!parentPath}
            onClick={navigateUp}
            title="Go up one level"
          >
            <ArrowUp className="w-4 h-4" />
          </Button>
          <div className="flex-1 truncate text-xs text-muted-foreground px-2 py-1 bg-muted rounded">
            {currentPath || "Root directories"}
          </div>
        </div>

        {/* Directory listing */}
        <ScrollArea className="h-64 border border-border rounded-md">
          {loading ? (
            <div className="flex items-center justify-center h-full">
              <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
            </div>
          ) : error ? (
            <div className="flex items-center justify-center h-full text-xs text-destructive px-4 text-center">
              {error}
            </div>
          ) : directories.length === 0 ? (
            <div className="flex items-center justify-center h-full text-xs text-muted-foreground">
              No subdirectories found
            </div>
          ) : (
            <div className="py-1">
              {directories.map((entry) => (
                <button
                  key={entry.path}
                  className={cn(
                    "w-full flex items-center gap-3 px-3 py-2 text-sm text-left hover:bg-accent transition-colors",
                    selectedPath === entry.path && "bg-accent text-accent-foreground",
                  )}
                  onClick={() => setSelectedPath(entry.path)}
                  onDoubleClick={() => handleDoubleClick(entry)}
                  type="button"
                >
                  <Folder className="w-4 h-4 shrink-0 text-muted-foreground/70" />
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium">{entry.name}</div>
                    <div className="truncate text-xs text-muted-foreground/60">
                      {entry.path}
                    </div>
                  </div>
                </button>
              ))}
            </div>
          )}
        </ScrollArea>

        {/* Selected path input */}
        <div className="flex items-center gap-2">
          <Input
            value={selectedPath}
            onChange={(e) => setSelectedPath(e.target.value)}
            placeholder="Selected folder path"
            className="h-8 text-xs flex-1"
            onKeyDown={handleKeyDown}
          />
        </div>

        {/* Action buttons */}
        <div className="flex justify-end gap-2">
          <Button
            variant="outline"
            size="sm"
            className="h-8"
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            size="sm"
            className="h-8"
            disabled={!selectedPath}
            onClick={handleConfirm}
          >
            Select Folder
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
