import { useState } from "react";
import { useProjectState } from "../../stores/projectStore";
import { FolderGit2, Plus, Trash2, ChevronLeft, FolderOpen, Loader2 } from "lucide-react";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { ScrollArea } from "../ui/scroll-area";
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "../ui/tooltip";
import { Separator } from "../ui/separator";

interface Props {
  isOpen: boolean;
  onToggle: () => void;
}

export default function ProjectSidebar({ isOpen, onToggle }: Props) {
  const { state, selectProject, addProject, removeProject } = useProjectState();
  const [adding, setAdding] = useState(false);
  const [newPath, setNewPath] = useState("");

  const handleAdd = async () => {
    const path = newPath.trim();
    if (!path) return;
    await addProject(path);
    setNewPath("");
    setAdding(false);
  };

  // Collapsed state: icon-only sidebar with tooltips
  if (!isOpen) {
    return (
      <TooltipProvider delayDuration={300}>
        <div className="flex flex-col items-center py-2 w-10 border-r border-border bg-background">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                className="p-2 h-9 w-9"
                onClick={onToggle}
              >
                <ChevronLeft className="w-4 h-4 rotate-180" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right">Show project sidebar</TooltipContent>
          </Tooltip>

          <Separator className="my-2 w-6" />

          {state.projects.length > 0 && (
            <div className="flex flex-col gap-1">
              {state.projects.slice(0, 5).map((p) => (
                <Tooltip key={p.path}>
                  <TooltipTrigger asChild>
                    <Button
                      variant="ghost"
                      size="sm"
                      className={`p-2 h-9 w-9 ${
                        state.activeProject?.path === p.path
                          ? "bg-primary/20 text-primary"
                          : "text-muted-foreground"
                      }`}
                      onClick={() => selectProject(p)}
                    >
                      <FolderGit2 className="w-4 h-4" />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent side="right">{p.name}</TooltipContent>
                </Tooltip>
              ))}
            </div>
          )}
        </div>
      </TooltipProvider>
    );
  }

  // Expanded state
  return (
    <div className="flex flex-col w-60 border-r border-border bg-background">
      {/* Header */}
      <div className="flex items-center justify-between px-4 h-12 border-b border-border">
        <h2 className="text-sm font-semibold text-foreground">Projects</h2>
        <Button variant="ghost" size="sm" className="h-8 w-8 p-0" onClick={onToggle}>
          <ChevronLeft className="w-4 h-4" />
        </Button>
      </div>

      {/* Project list */}
      <ScrollArea className="flex-1">
        {state.loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
          </div>
        ) : state.projects.length === 0 ? (
          <div className="px-4 py-12 text-center text-xs text-muted-foreground">
            <FolderOpen className="w-8 h-8 mx-auto mb-2 text-muted-foreground/60" />
            <p>No projects yet</p>
            <p className="mt-1">Add a project root below</p>
          </div>
        ) : (
          <div className="py-1">
            {state.projects.map((project) => (
              <div key={project.path} className="group relative px-1">
                <Button
                  variant="ghost"
                  className={`w-full justify-start gap-3 px-3 h-auto py-2.5 text-sm ${
                    state.activeProject?.path === project.path
                      ? "bg-accent text-accent-foreground"
                      : "text-muted-foreground"
                  }`}
                  onClick={() => selectProject(project)}
                >
                  <FolderGit2 className="w-4 h-4 shrink-0 text-muted-foreground/70" />
                  <div className="min-w-0 flex-1 text-left">
                    <div className="truncate font-medium text-foreground/90">
                      {project.name}
                    </div>
                    <div className="truncate text-xs text-muted-foreground/60">
                      {project.path}
                    </div>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="p-1 h-6 w-6 opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-destructive"
                    onClick={(e) => {
                      e.stopPropagation();
                      removeProject(project.path);
                    }}
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </Button>
                </Button>
              </div>
            ))}
          </div>
        )}
      </ScrollArea>

      {/* Add project */}
      <div className="border-t border-border p-3">
        {adding ? (
          <div className="flex flex-col gap-2">
            <Input
              type="text"
              value={newPath}
              onChange={(e) => setNewPath(e.target.value)}
              placeholder="/path/to/project"
              className="h-8 text-xs"
              onKeyDown={(e) => {
                if (e.key === "Enter") handleAdd();
                if (e.key === "Escape") setAdding(false);
              }}
              autoFocus
            />
            <div className="flex gap-2">
              <Button size="sm" className="flex-1 h-7 text-xs" onClick={handleAdd}>
                Add
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-xs"
                onClick={() => setAdding(false)}
              >
                Cancel
              </Button>
            </div>
          </div>
        ) : (
          <Button
            variant="ghost"
            size="sm"
            className="w-full justify-start gap-2 h-8 text-xs text-muted-foreground"
            onClick={() => setAdding(true)}
          >
            <Plus className="w-3.5 h-3.5" />
            <span>Add Project</span>
          </Button>
        )}
      </div>
    </div>
  );
}
