import { useState, useMemo, useCallback, useRef, useEffect } from "react";
import { useProjectState } from "../../stores/projectStore";
import { useChatSelector, type ChatState } from "../../stores/chatStore";
import { useTerminalState } from "../../stores/terminalStore";
import type { Project, ProjectGroup } from "../../api/types";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  FolderGit2,
  Plus,
  Trash2,
  ChevronLeft,
  ChevronDown,
  ChevronRight,
  FolderOpen,
  Loader2,
  FolderSearch,
  GripVertical,
  Pencil,
  FolderTree,
  X,
  Bell,
  AlertTriangle,
  Pause,
  ShieldAlert,
  MessageSquare,
} from "lucide-react";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { ScrollArea } from "../ui/scroll-area";
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "../ui/tooltip";
import { Separator } from "../ui/separator";
import DirectoryBrowser from "./DirectoryBrowser";
import { ContextMenu, type ContextMenuItem } from "./ContextMenu";
import { computeProjectDrag } from "../../lib/projectDrag";

type SessionStatus = "none" | "idle" | "running";

type ProjectIndicators = {
  sessionCount: number;
  streamingCount: number;
  stalledCount: number;
  pendingCount: number;
  terminalAlertCount: number;
  hasRealSession: boolean;
  anyStreaming: boolean;
  anyStalled: boolean;
  status: SessionStatus;
};

type ProjectSidebarItem = {
  type: "group" | "project";
  data: Project | ProjectGroup;
  groupProjects?: Project[];
};

export function buildProjectSidebarOrder(
  projects: Project[],
  groups: ProjectGroup[],
): { orderedProjects: Project[]; visibleItems: ProjectSidebarItem[] } {
  // Keep the input order within each group. It is the persisted project order
  // used by the expanded sidebar and must not be replaced with a name sort.
  const sortedGroups = [...groups].sort((a, b) => a.order - b.order);
  const projectsByGroup: Record<string, Project[]> = {};
  for (const project of projects) {
    const key = project.group || "";
    if (!projectsByGroup[key]) projectsByGroup[key] = [];
    projectsByGroup[key].push(project);
  }

  const orderedProjects: Project[] = [];
  const visibleItems: ProjectSidebarItem[] = [];

  for (const group of sortedGroups) {
    const groupProjects = projectsByGroup[group.name] || [];
    orderedProjects.push(...groupProjects);
    visibleItems.push({ type: "group", data: group, groupProjects });
    if (!group.collapsed) {
      for (const project of groupProjects) {
        visibleItems.push({ type: "project", data: project });
      }
    }
  }

  // Ungrouped projects remain after all groups, matching the expanded view.
  const ungrouped = projectsByGroup[""] || [];
  orderedProjects.push(...ungrouped);
  for (const project of ungrouped) {
    visibleItems.push({ type: "project", data: project });
  }

  // Keep orphaned projects in the collapsed rail. The expanded view has
  // historically omitted them because there is no group header to render
  // them under, but the old collapsed rail still included every project.
  const knownGroups = new Set(groups.map((group) => group.name));
  for (const project of projects) {
    if (project.group && !knownGroups.has(project.group)) {
      orderedProjects.push(project);
    }
  }

  return { orderedProjects, visibleItems };
}

/** Derive per-project indicators from open tabs, chat slices, and terminal alerts.
 *  Covers: # sessions open, streaming count / stalled (chat streaming stopped),
 *  pending permission, and terminal beep (alerted). Optimized to only re-render
 *  when the aggregate counts actually change, not on every streamed token. */
function useProjectIndicators(projectPath: string): ProjectIndicators {
  const { state: projectState } = useProjectState();
  const tabs = projectState.tabsByProject[projectPath] ?? [];
  const sessionCount = tabs.length;
  const hasRealSession = useMemo(
    () => tabs.some((t) => !t.id.startsWith("new-")),
    [tabs],
  );

  // Chat-derived aggregates: streamingCount, stalledCount, pendingCount.
  // The selector closes over `tabs` so the derived counts update even without
  // a chat dispatch when tabs are added/removed.
  const chatAgg = useChatSelector(
    (s: ChatState) => {
      let streamingCount = 0;
      let stalledCount = 0;
      let pendingCount = 0;
      for (const tab of tabs) {
        if (tab.id.startsWith("new-")) continue;
        const slice = s.sessions[tab.id];
        if (!slice) continue;
        const active = slice.isStreaming || slice.turnActive;
        if (slice.turnStalled) stalledCount++;
        else if (active) streamingCount++;
        if (slice.pendingPermission || slice.pendingQuestion) pendingCount++;
      }
      return { streamingCount, stalledCount, pendingCount };
    },
    (a, b) =>
      a.streamingCount === b.streamingCount &&
      a.stalledCount === b.stalledCount &&
      a.pendingCount === b.pendingCount,
  );

  // Terminal-derived aggregate: how many backgrounded terminals emitted a bell.
  const { state: terminalState } = useTerminalState();
  const terminalAlertCount = useMemo(() => {
    const entry = terminalState.byProject[projectPath];
    if (!entry?.alerts) return 0;
    let c = 0;
    for (const v of Object.values(entry.alerts)) if (v) c++;
    return c;
  }, [terminalState, projectPath]);

  const anyStreaming = chatAgg.streamingCount > 0;
  const anyStalled = chatAgg.stalledCount > 0;

  const status: SessionStatus = useMemo(() => {
    if (sessionCount === 0) return "none";
    if (anyStreaming) return "running";
    return hasRealSession ? "idle" : "none";
  }, [sessionCount, anyStreaming, hasRealSession]);

  return {
    sessionCount,
    streamingCount: chatAgg.streamingCount,
    stalledCount: chatAgg.stalledCount,
    pendingCount: chatAgg.pendingCount,
    terminalAlertCount,
    hasRealSession,
    anyStreaming,
    anyStalled,
    status,
  };
}

function SessionDot({ status }: { status: SessionStatus }) {
  if (status === "none") return null;
  return (
    <span
      className={`shrink-0 h-2 w-2 rounded-full ${
        status === "running"
          ? "bg-blue-500 animate-pulse"
          : "bg-muted-foreground/30"
      }`}
    />
  );
}

function ProjectBadges({ indicators }: { indicators: ProjectIndicators }) {
  const { sessionCount, streamingCount, stalledCount, pendingCount, terminalAlertCount } = indicators;
  const hasAny = sessionCount > 0 || streamingCount > 0 || stalledCount > 0 || pendingCount > 0 || terminalAlertCount > 0;
  if (!hasAny) return null;
  return (
    <span className="flex items-center gap-1 shrink-0">
      {/* # sessions open */}
      {sessionCount > 0 && (
        <span
          className="inline-flex items-center gap-0.5 rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium leading-none text-muted-foreground"
          title={`${sessionCount} session${sessionCount === 1 ? "" : "s"} open`}
        >
          <MessageSquare className="w-2.5 h-2.5" />
          {sessionCount}
        </span>
      )}
      {/* streaming count */}
      {streamingCount > 0 && (
        <span
          className="inline-flex items-center gap-0.5 rounded-full bg-blue-500/15 px-1.5 py-0.5 text-[10px] font-medium leading-none text-blue-600 dark:text-blue-400 border border-blue-500/20"
          title={`${streamingCount} streaming`}
        >
          <Loader2 className="w-2.5 h-2.5 animate-spin" />
          {streamingCount}
        </span>
      )}
      {/* chat streaming stopped / stalled */}
      {stalledCount > 0 && (
        <span
          className="inline-flex items-center gap-0.5 rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium leading-none text-amber-600 dark:text-amber-400 border border-amber-500/30"
          title={`${stalledCount} stalled (streaming stopped)`}
        >
          <Pause className="w-2.5 h-2.5" />
          {stalledCount}
          <AlertTriangle className="w-2.5 h-2.5" />
        </span>
      )}
      {/* pending permission / question */}
      {pendingCount > 0 && (
        <span
          className="inline-flex items-center gap-0.5 rounded-full bg-amber-500/20 px-1.5 py-0.5 text-[10px] font-medium leading-none text-amber-700 dark:text-amber-300 border border-amber-500/40"
          title={`${pendingCount} pending permission`}
        >
          <ShieldAlert className="w-2.5 h-2.5" />
          {pendingCount}
        </span>
      )}
      {/* terminal beep / bell */}
      {terminalAlertCount > 0 && (
        <span
          className="inline-flex items-center gap-0.5 rounded-full bg-red-500/15 px-1.5 py-0.5 text-[10px] font-medium leading-none text-red-600 dark:text-red-400 border border-red-500/30"
          title={`${terminalAlertCount} terminal beep`}
        >
          <Bell className="w-2.5 h-2.5" />
          {terminalAlertCount}
        </span>
      )}
    </span>
  );
}

// ── Inline Rename Hook ──────────────────────────────────────────────────────

function useInlineRename(
  initialName: string,
  onRename: (name: string) => Promise<void>,
) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(initialName);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  const start = useCallback(() => {
    setValue(initialName);
    setEditing(true);
  }, [initialName]);

  const commit = useCallback(async () => {
    const trimmed = value.trim();
    if (trimmed && trimmed !== initialName) {
      await onRename(trimmed);
    }
    setEditing(false);
  }, [value, initialName, onRename]);

  const cancel = useCallback(() => {
    setEditing(false);
    setValue(initialName);
  }, [initialName]);

  return { editing, value, setValue, inputRef, start, commit, cancel };
}

// ── Sortable Project Row ────────────────────────────────────────────────────

interface SortableProjectRowProps {
  project: Project;
  isActive: boolean;
  onSelect: () => void;
  onRemove: () => void;
  onRename: (name: string) => Promise<void>;
  onCreateGroup: (name: string) => Promise<void>;
  onAddToGroup: (group: string) => Promise<void>;
  onRemoveFromGroup: () => Promise<void>;
  groups: ProjectGroup[];
}

function SortableProjectRow({
  project,
  isActive,
  onSelect,
  onRemove,
  onRename,
  onCreateGroup,
  onAddToGroup,
  onRemoveFromGroup,
  groups,
}: SortableProjectRowProps) {
  const indicators = useProjectIndicators(project.path);
  const status = indicators.status;
  const rename = useInlineRename(project.name, onRename);

  const contextItems: ContextMenuItem[] = useMemo(() => {
    const items: ContextMenuItem[] = [
      { label: "Rename", icon: <Pencil className="w-3.5 h-3.5" />, onClick: rename.start },
      { label: "Remove", icon: <Trash2 className="w-3.5 h-3.5" />, onClick: onRemove, destructive: true },
      { separator: true, label: "", onClick: () => {} },
    ];

    if (groups.length === 0) {
      items.push({
        label: "Create group",
        icon: <FolderTree className="w-3.5 h-3.5" />,
        onClick: () => onCreateGroup("New group"),
      });
    } else {
      // Add to group submenu items
      const availableGroups = groups.filter((g) => g.name !== project.group);
      if (availableGroups.length > 0) {
        for (const g of availableGroups) {
          items.push({
            label: `Move to "${g.name}"`,
            icon: <FolderTree className="w-3.5 h-3.5" />,
            onClick: () => onAddToGroup(g.name),
          });
        }
      }
      if (project.group) {
        items.push({
          label: "Remove from group",
          icon: <X className="w-3.5 h-3.5" />,
          onClick: onRemoveFromGroup,
        });
      }
      if (!project.group) {
        items.push({
          label: "Create group",
          icon: <FolderTree className="w-3.5 h-3.5" />,
          onClick: () => onCreateGroup("New group"),
        });
      }
    }

    return items;
  }, [groups, project.group, rename.start, onRemove, onCreateGroup, onAddToGroup, onRemoveFromGroup]);

  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: project.path });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  return (
    <div ref={setNodeRef} style={style} className="group relative px-1">
      <ContextMenu items={contextItems}>
        <Button
          variant="ghost"
          className={`w-full justify-start gap-2 px-2 h-auto py-2 text-sm ${
            isActive
              ? "bg-primary/15 text-foreground border-l-2 border-primary"
              : "text-muted-foreground border-l-2 border-transparent"
          }`}
          onClick={onSelect}
        >
          <span
            className="shrink-0 cursor-grab active:cursor-grabbing opacity-0 group-hover:opacity-60 hover:!opacity-100 touch-none"
            {...attributes}
            {...listeners}
          >
            <GripVertical className="w-3.5 h-3.5" />
          </span>
          <FolderGit2 className="w-4 h-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1 text-left">
            {rename.editing ? (
              <Input
                ref={rename.inputRef}
                value={rename.value}
                onChange={(e) => rename.setValue(e.target.value)}
                onBlur={rename.commit}
                onKeyDown={(e) => {
                  if (e.key === "Enter") rename.commit();
                  if (e.key === "Escape") rename.cancel();
                }}
                className="h-5 py-0 px-1 text-sm"
                onClick={(e) => e.stopPropagation()}
              />
            ) : (
              <>
                <div className="truncate font-medium text-foreground">
                  {project.name}
                </div>
                <div className="truncate text-xs text-muted-foreground">
                  {project.path}
                </div>
              </>
            )}
          </div>
          <span className="flex items-center gap-1 shrink-0">
            <ProjectBadges indicators={indicators} />
            <SessionDot status={status} />
          </span>
          <Button
            variant="ghost"
            size="sm"
            className="p-1 h-5 w-5 opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-destructive shrink-0"
            onClick={(e) => {
              e.stopPropagation();
              onRemove();
            }}
          >
            <Trash2 className="w-3 h-3" />
          </Button>
        </Button>
      </ContextMenu>
    </div>
  );
}

// ── Sortable Group Header ───────────────────────────────────────────────────

interface SortableGroupHeaderProps {
  group: ProjectGroup;
  projectCount: number;
  onToggle: () => void;
  onRename: (name: string) => Promise<void>;
  onDelete: () => Promise<void>;
}

function SortableGroupHeader({
  group,
  projectCount,
  onToggle,
  onRename,
  onDelete,
}: SortableGroupHeaderProps) {
  const rename = useInlineRename(group.name, onRename);

  const contextItems: ContextMenuItem[] = useMemo(
    () => [
      { label: "Rename", icon: <Pencil className="w-3.5 h-3.5" />, onClick: rename.start },
      { label: "Delete group", icon: <Trash2 className="w-3.5 h-3.5" />, onClick: onDelete, destructive: true },
    ],
    [rename.start, onDelete],
  );

  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: `group:${group.name}` });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  return (
    <div ref={setNodeRef} style={style} className="group/header px-1 pt-2">
      <ContextMenu items={contextItems}>
        <div className="flex items-center gap-1">
          <span
            className="shrink-0 cursor-grab active:cursor-grabbing opacity-0 group-hover/header:opacity-60 hover:!opacity-100 touch-none"
            {...attributes}
            {...listeners}
          >
            <GripVertical className="w-3 h-3" />
          </span>
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-1 py-0 text-xs font-semibold text-muted-foreground hover:text-foreground gap-1"
            onClick={onToggle}
          >
            {group.collapsed ? (
              <ChevronRight className="w-3 h-3" />
            ) : (
              <ChevronDown className="w-3 h-3" />
            )}
            {rename.editing ? (
              <Input
                ref={rename.inputRef}
                value={rename.value}
                onChange={(e) => rename.setValue(e.target.value)}
                onBlur={rename.commit}
                onKeyDown={(e) => {
                  if (e.key === "Enter") rename.commit();
                  if (e.key === "Escape") rename.cancel();
                }}
                className="h-4 py-0 px-1 text-xs w-24"
                onClick={(e) => e.stopPropagation()}
              />
            ) : (
              <span className="uppercase tracking-wider">{group.name}</span>
            )}
            <span className="text-muted-foreground font-normal">({projectCount})</span>
          </Button>
        </div>
      </ContextMenu>
    </div>
  );
}

// ── Create Group Dialog ─────────────────────────────────────────────────────

function CreateGroupDialog({
  open,
  onClose,
  onCreate,
}: {
  open: boolean;
  onClose: () => void;
  onCreate: (name: string) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      setName("New group");
      setTimeout(() => inputRef.current?.select(), 50);
    }
  }, [open]);

  if (!open) return null;

  return (
    <div className="px-3 py-2 border-t border-border">
      <div className="flex gap-2">
        <Input
          ref={inputRef}
          value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={async (e) => {
            if (e.key === "Enter" && name.trim()) {
              await onCreate(name.trim());
              onClose();
            }
            if (e.key === "Escape") onClose();
          }}
          className="h-7 text-xs flex-1"
          autoFocus
        />
        <Button
          size="sm"
          className="h-7 text-xs"
          onClick={async () => {
            if (name.trim()) {
              await onCreate(name.trim());
              onClose();
            }
          }}
        >
          Create
        </Button>
        <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={onClose}>
          Cancel
        </Button>
      </div>
    </div>
  );
}

// ── Main Sidebar ────────────────────────────────────────────────────────────

function CollapsedProjectButton({
  project,
  isActive,
  onSelect,
}: {
  project: Project;
  isActive: boolean;
  onSelect: () => void;
}) {
  const indicators = useProjectIndicators(project.path);
  const showCount = indicators.sessionCount > 0;
  // Prioritize overlays: pending > terminal alert > streaming > stalled
  const hasPending = indicators.pendingCount > 0;
  const hasAlert = indicators.terminalAlertCount > 0;
  const hasStreaming = indicators.streamingCount > 0;
  const hasStalled = indicators.stalledCount > 0;
  const overlayColor = hasPending
    ? "bg-amber-500"
    : hasAlert
      ? "bg-red-500"
      : hasStreaming
        ? "bg-blue-500"
        : hasStalled
          ? "bg-amber-400"
          : null;
  const overlayTitle = hasPending
    ? `${indicators.pendingCount} pending permission`
    : hasAlert
      ? `${indicators.terminalAlertCount} terminal beep`
      : hasStreaming
        ? `${indicators.streamingCount} streaming`
        : hasStalled
          ? `${indicators.stalledCount} stalled`
          : "";
  const tooltipDetails = [
    `${indicators.sessionCount} session${indicators.sessionCount === 1 ? "" : "s"}`,
    hasStreaming ? `${indicators.streamingCount} streaming` : null,
    hasStalled ? `${indicators.stalledCount} stalled` : null,
    hasPending ? `${indicators.pendingCount} pending` : null,
    hasAlert ? `${indicators.terminalAlertCount} beep` : null,
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          aria-label={project.name}
          className={`relative p-2 h-9 w-9 ${
            isActive ? "bg-primary/15 text-primary" : "text-muted-foreground"
          }`}
          onClick={onSelect}
        >
          <FolderGit2 className="w-4 h-4" />
          {showCount && (
            <span className="absolute -top-1 -right-1 min-w-[14px] h-[14px] px-0.5 rounded-full bg-muted-foreground text-white text-[8px] leading-[14px] font-bold text-center border border-background">
              {indicators.sessionCount}
            </span>
          )}
          {overlayColor && (
            <span
              title={overlayTitle}
              className={`absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full border-2 border-background ${overlayColor} ${hasStreaming ? "animate-pulse" : ""}`}
            />
          )}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="right">
        <div className="text-xs">
          <div className="font-medium">{project.name}</div>
          {showCount && <div className="text-muted-foreground">{tooltipDetails}</div>}
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

interface Props {
  isOpen: boolean;
  onToggle: () => void;
  /** Current width in pixels. When set, overrides the default w-60. */
  width?: number;
}

export default function ProjectSidebar({ isOpen, onToggle, width }: Props) {
  const {
    state,
    selectProject,
    addProject,
    removeProject,
    renameProject,
    reorderProjects,
    setProjectGroup,
    createGroup,
    deleteGroup,
    renameGroup,
    reorderGroups,
    setGroupCollapsed,
  } = useProjectState();
  const [adding, setAdding] = useState(false);
  const [newPath, setNewPath] = useState("");
  const [browserOpen, setBrowserOpen] = useState(false);
  const [creatingGroup, setCreatingGroup] = useState(false);

  const toggleGroupCollapse = useCallback((name: string, currentlyCollapsed: boolean) => {
    setGroupCollapsed(name, !currentlyCollapsed);
  }, [setGroupCollapsed]);

  const handleAdd = async () => {
    const path = newPath.trim();
    if (!path) return;
    await addProject(path);
    setNewPath("");
    setAdding(false);
  };

  // Build the sorted list: groups first (in group order), then ungrouped projects
  const { orderedProjects, visibleItems: sortedItems } = useMemo(() => {
    return buildProjectSidebarOrder(state.projects || [], state.groups || []);
  }, [state.projects, state.groups]);

  // DnD sensors
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id) return;

      const activeId = String(active.id);
      const overId = String(over.id);

      // Check if this is a group reorder
      if (activeId.startsWith("group:") && overId.startsWith("group:")) {
        const groupNames = (state.groups ?? []).map((g) => g.name);
        const oldIndex = groupNames.indexOf(activeId.replace("group:", ""));
        const newIndex = groupNames.indexOf(overId.replace("group:", ""));
        if (oldIndex !== -1 && newIndex !== -1) {
          const newOrder = arrayMove(groupNames, oldIndex, newIndex);
          reorderGroups(newOrder);
        }
        return;
      }

      // Project drop — resolve to a reorder (same bucket) or a cross-bucket
      // move (group change + position). Dropping on a group header moves the
      // project into that group.
      const result = computeProjectDrag(state.projects, state.groups || [], activeId, overId);
      if (result.type === "reorder") {
        reorderProjects(result.paths);
      } else if (result.type === "move") {
        void (async () => {
          await setProjectGroup(result.path, result.group);
          await reorderProjects(result.paths);
        })();
      }
    },
    [state.groups, state.projects, reorderGroups, reorderProjects, setProjectGroup],
  );

  // Unique sortable IDs
  const sortableIds = useMemo(
    () => sortedItems.map((item) => (item.type === "group" ? `group:${(item.data as ProjectGroup).name}` : (item.data as Project).path)),
    [sortedItems],
  );

  const handleCreateGroup = useCallback(
    async (name: string) => {
      await createGroup(name);
      // Move active project into the new group if one is selected
      if (state.activeProject) {
        await setProjectGroup(state.activeProject.path, name);
      }
    },
    [createGroup, setProjectGroup, state.activeProject],
  );

  // Collapsed state for sidebar
  if (!isOpen) {
    return (
      <TooltipProvider delayDuration={300}>
        <div className="flex flex-col items-center py-2 w-10 border-r border-border bg-background">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="sm" className="p-2 h-9 w-9" onClick={onToggle}>
                <ChevronLeft className="w-4 h-4 rotate-180" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right">Show project sidebar</TooltipContent>
          </Tooltip>
          <Separator className="my-2 w-6" />
          {state.projects.length > 0 && (
            <div className="flex flex-col gap-1">
              {orderedProjects.slice(0, 5).map((p) => (
                <CollapsedProjectButton
                  key={p.path}
                  project={p}
                  isActive={state.activeProject?.path === p.path}
                  onSelect={() => selectProject(p)}
                />
              ))}
            </div>
          )}
        </div>
      </TooltipProvider>
    );
  }

  // Expanded state
  return (
    <div
      className="flex flex-col border-r border-border bg-background flex-shrink-0"
      style={width ? { width: `${width}px` } : undefined}
    >

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
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
            <SortableContext items={sortableIds} strategy={verticalListSortingStrategy}>
              <div className="py-1">
                {sortedItems.map((item) => {
                  if (item.type === "group") {
                    const group = item.data as ProjectGroup;
                    const groupProjects = item.groupProjects || [];
                    return (
                      <SortableGroupHeader
                        key={`group:${group.name}`}
                        group={group}
                        projectCount={groupProjects.length}
                        onToggle={() => toggleGroupCollapse(group.name, !!group.collapsed)}
                        onRename={(name) => renameGroup(group.name, name)}
                        onDelete={() => deleteGroup(group.name)}
                      />
                    );
                  }
                  const project = item.data as Project;
                  return (
                    <SortableProjectRow
                      key={project.path}
                      project={project}
                      isActive={state.activeProject?.path === project.path}
                      onSelect={() => selectProject(project)}
                      onRemove={() => removeProject(project.path)}
                      onRename={(name) => renameProject(project.path, name)}
                      onCreateGroup={async (name) => {
                        await createGroup(name);
                        await setProjectGroup(project.path, name);
                      }}
                      onAddToGroup={(group) => setProjectGroup(project.path, group)}
                      onRemoveFromGroup={() => setProjectGroup(project.path, "")}
                      groups={state.groups}
                    />
                  );
                })}
              </div>
            </SortableContext>
          </DndContext>
        )}
      </ScrollArea>

      {/* Add project */}
      <div className="border-t border-border p-3">
        {adding ? (
          <div className="flex flex-col gap-2">
            <div className="flex gap-2">
              <Input
                type="text"
                value={newPath}
                onChange={(e) => setNewPath(e.target.value)}
                placeholder="/path/to/project"
                className="h-8 text-xs flex-1"
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleAdd();
                  if (e.key === "Escape") setAdding(false);
                }}
                autoFocus
              />
              <Button
                variant="outline"
                size="sm"
                className="h-8 w-8 p-0 shrink-0"
                onClick={() => setBrowserOpen(true)}
                title="Browse for folder"
              >
                <FolderSearch className="w-3.5 h-3.5" />
              </Button>
            </div>
            <div className="flex gap-2">
              <Button size="sm" className="flex-1 h-7 text-xs" onClick={handleAdd}>
                Add
              </Button>
              <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => setAdding(false)}>
                Cancel
              </Button>
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-1">
            <Button
              variant="ghost"
              size="sm"
              className="w-full justify-start gap-2 h-8 text-xs text-muted-foreground"
              onClick={() => setAdding(true)}
            >
              <Plus className="w-3.5 h-3.5" />
              Add project
            </Button>
            {state.projects.length > 0 && (
              <Button
                variant="ghost"
                size="sm"
                className="w-full justify-start gap-2 h-8 text-xs text-muted-foreground"
                onClick={() => setCreatingGroup(true)}
              >
                <FolderTree className="w-3.5 h-3.5" />
                New group
              </Button>
            )}
          </div>
        )}
      </div>

      <CreateGroupDialog open={creatingGroup} onClose={() => setCreatingGroup(false)} onCreate={handleCreateGroup} />
      <DirectoryBrowser open={browserOpen} onOpenChange={setBrowserOpen} onSelect={(path) => { setNewPath(path); setBrowserOpen(false); }} />
    </div>
  );
}
