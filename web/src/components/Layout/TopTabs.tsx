import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { FolderGit2, GitBranch, Paperclip, CalendarClock, MessageSquare, MoreHorizontal, Settings, PanelLeft } from "lucide-react";
import { TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import SyncStatusWidget from "./SyncStatusWidget";
import PortMapsWidget from "./PortMapsWidget";
import { useProjectState } from "../../stores/projectStore";
import { loadProjectTerminals } from "../Terminal/terminalPersistence";
import { basename } from "@/lib/utils";
import { api } from "@/api/client";
import { eventBus } from "@/lib/eventBus";
import { tabLoadKey, type TabLoadingSnapshot } from "@/hooks/useKeyedLoad";
import { TabLoadingIndicator } from "@/components/common/TabLoadingIndicator";

interface Props {
  activeTab: string;
  onTabSelect: (value: string) => void;
  /**
   * Mobile-only: opens the project sidebar drawer. On phones the sidebar
   * defaults closed and has no inline rail, so this button is the only way
   * back to the project list once it is dismissed.
   */
  onMenuToggle?: () => void;
  loadingStates?: ReadonlyMap<string, TabLoadingSnapshot>;
}

const mainTabs = [
  { id: "sessions", label: "Sessions", icon: MessageSquare },
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "cron", label: "Cron", icon: CalendarClock },
  { id: "assets", label: "Assets", icon: Paperclip },
  { id: "settings", label: "Settings", icon: Settings },
];

export default function TopTabs({ activeTab, onTabSelect, onMenuToggle, loadingStates }: Props) {
  const { state: projectState } = useProjectState();
  const activeProjectPath = projectState.activeProject?.path ?? "";
  const activeProjectHost = projectState.activeProject?.host;
  const sessionsCount = activeProjectPath ? (projectState.tabsByProject[activeProjectPath]?.length ?? 0) : 0;
  const [terminalCount, setTerminalCount] = useState(() => {
    try {
      const saved = loadProjectTerminals(activeProjectPath, activeProjectHost);
      return saved?.terminals.length ?? 0;
    } catch {
      return 0;
    }
  });
  useEffect(() => {
    const update = () => {
      try {
        const saved = loadProjectTerminals(activeProjectPath, activeProjectHost);
        setTerminalCount(saved?.terminals.length ?? 0);
      } catch {
        setTerminalCount(0);
      }
    };
    update();
    const onStorage = (e: StorageEvent) => {
      if (e.key === "ocode.ui.terminals.project.v1") update();
    };
    const onCustom = () => update();
    window.addEventListener("storage", onStorage);
    window.addEventListener("ocode:terminals-changed", onCustom);
    return () => {
      window.removeEventListener("storage", onStorage);
      window.removeEventListener("ocode:terminals-changed", onCustom);
    };
  }, [activeProjectPath, activeProjectHost]);
  const tabsRef = useRef<HTMLDivElement | null>(null);
  const activeRef = useRef<HTMLButtonElement | null>(null);
  const [overflowing, setOverflowing] = useState(false);

  // Git working-tree counts for the active project. Lightweight
  // GET /api/git/status poll (staged_files + changed_files, untracked
  // included since the backend fix) plus instant refresh on the
  // server-push `git_status` bus event — same wording as GitPanel's
  // "N staged · M unstaged" header so the tab badge always matches.
  const [gitStaged, setGitStaged] = useState(0);
  const [gitUnstaged, setGitUnstaged] = useState(0);
  const [gitConflicted, setGitConflicted] = useState(0);
  const [gitAhead, setGitAhead] = useState(0);
  const [gitBehind, setGitBehind] = useState(0);
  const [gitHasUpstream, setGitHasUpstream] = useState(false);
  useEffect(() => {
    if (!activeProjectPath) {
      setGitStaged(0);
      setGitUnstaged(0);
      setGitConflicted(0);
      setGitAhead(0);
      setGitBehind(0);
      setGitHasUpstream(false);
      return;
    }
    let cancelled = false;
    const fetchCounts = async () => {
      try {
        const status = await api.getGitStatus(activeProjectPath, activeProjectHost);
        if (cancelled) return;
        setGitStaged(status.staged_files?.length ?? 0);
        setGitUnstaged(status.changed_files?.length ?? 0);
        // A conflicted path is no longer in either list, so add it here or
        // the badge would undercount exactly when the user most needs it.
        setGitConflicted(status.conflicts?.length ?? 0);
        setGitAhead(status.ahead ?? 0);
        setGitBehind(status.behind ?? 0);
        setGitHasUpstream(status.has_upstream ?? false);
      } catch {
        if (!cancelled) {
          setGitStaged(0);
          setGitUnstaged(0);
          setGitConflicted(0);
          setGitAhead(0);
          setGitBehind(0);
          setGitHasUpstream(false);
        }
      }
    };
    fetchCounts();
    const interval = setInterval(fetchCounts, 10000);
    const off = eventBus.on("git_status", (env) => {
      if (!env.project || env.project === activeProjectPath) fetchCounts();
    });
    return () => {
      cancelled = true;
      clearInterval(interval);
      off();
    };
  }, [activeProjectPath, activeProjectHost]);
  const gitTotal = gitStaged + gitUnstaged + gitConflicted;
  const gitTitle =
    gitConflicted > 0
      ? `${gitConflicted} conflicted · ${gitStaged} staged · ${gitUnstaged} unstaged`
      : `${gitStaged} staged · ${gitUnstaged} unstaged`;

  // Detect whether the tab strip overflows its container so the "More" menu
  // can be shown. Re-measured on resize.
  useLayoutEffect(() => {
    const el = tabsRef.current;
    if (!el) return;
    const measure = () => setOverflowing(el.scrollWidth > el.clientWidth + 1);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    window.addEventListener("resize", measure);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, []);

  // Keep the active tab in view (Chrome-style) when it changes.
  useLayoutEffect(() => {
    const el = tabsRef.current;
    const tab = activeRef.current;
    if (!el || !tab) return;
    const left = tab.getBoundingClientRect().left - el.getBoundingClientRect().left;
    const right = left + tab.getBoundingClientRect().width;
    if (left < el.scrollLeft) {
      el.scrollTo({ left });
    } else if (right > el.scrollLeft + el.clientWidth) {
      el.scrollTo({ left: right - el.clientWidth });
    }
  }, [activeTab]);

  // Translate vertical mouse-wheel input into horizontal scrolling on the
  // strip. Trackpad horizontal gestures scroll natively.
  const handleWheel = (e: React.WheelEvent<HTMLDivElement>) => {
    const el = tabsRef.current;
    if (!el || el.scrollWidth <= el.clientWidth + 1) return;
    const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
    if (delta === 0) return;
    const atLeft = el.scrollLeft <= 0;
    const atRight = el.scrollLeft + el.clientWidth >= el.scrollWidth - 1;
    if ((delta < 0 && atLeft) || (delta > 0 && atRight)) return;
    e.preventDefault();
    el.scrollLeft += delta;
  };

  return (
    <header className="flex items-center border-b border-border bg-card h-12 px-4 overflow-hidden">
      {/* Mobile: project-drawer launcher. Hidden ≥md, where the sidebar is an
          inline column with its own rail/collapse affordances. */}
      {onMenuToggle && (
        <button
          type="button"
          onClick={onMenuToggle}
          aria-label="Open projects sidebar"
          title="Open projects sidebar"
          className="md:hidden mr-2 -ml-1 p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted shrink-0"
        >
          <PanelLeft className="w-4 h-4" />
        </button>
      )}
      {/* Left: Logo */}
      <div className="flex items-center gap-2 mr-6 shrink-0">
        <div className="w-6 h-6 rounded bg-blue-600 flex items-center justify-center text-xs font-bold">
          o
        </div>
        <span className="font-semibold text-sm hidden sm:inline">{basename(activeProjectPath) || "ocode"}</span>
      </div>

      {/* Main tabs — single row, horizontally scrollable */}
      <TabsList
        ref={tabsRef}
        onWheel={handleWheel}
        className="bg-transparent p-0 h-auto gap-1 justify-start flex-1 min-w-0 overflow-x-auto overflow-y-hidden scrollbar-hide flex-nowrap touch-pan-x overscroll-x-contain"
        style={{ WebkitOverflowScrolling: "touch" } as React.CSSProperties}
      >
        {mainTabs.map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.id;
          const loadingState = loadingStates?.get(tabLoadKey(activeProjectHost, activeProjectPath, tab.id));
          const loading = loadingState?.phase === "refresh";
          const loadError = loadingState?.phase === "error";
          const count = tab.id === "sessions" ? sessionsCount + terminalCount : undefined;
          const gitCount = tab.id === "git" && gitTotal > 0 ? gitTotal : undefined;
          return (
            <TabsTrigger
              key={tab.id}
              value={tab.id}
              ref={isActive ? activeRef : undefined}
              aria-busy={loading || undefined}
              className="flex items-center gap-2 px-3 py-1.5 rounded-md text-sm font-semibold transition-colors whitespace-nowrap data-[state=active]:bg-accent data-[state=active]:text-accent-foreground data-[state=active]:shadow-none text-muted-foreground hover:text-foreground hover:bg-muted shrink-0"
            >
              <Icon className="w-4 h-4" />
              <span className="hidden sm:inline">{tab.label}</span>
              <TabLoadingIndicator active={loading} error={loadError} label={`Loading ${tab.label}`} />
              {count !== undefined && (
                <span
                  className={`inline-flex items-center justify-center min-w-[1.25rem] h-5 px-1 rounded-full text-xs font-semibold leading-none ${
                    isActive ? "bg-accent text-accent-foreground" : "bg-muted text-foreground"
                  }`}
                  aria-label={`${tab.label} count ${count}`}
                >
                  {count}
                </span>
              )}
              {gitCount !== undefined && (
                <span
                  title={gitTitle}
                  className="inline-flex items-center justify-center min-w-[1.25rem] h-5 px-1 rounded-full text-xs font-semibold leading-none bg-muted text-foreground"
                  aria-label={`Git count ${gitCount}`}
                >
                  {gitCount}
                </span>
              )}
              {tab.id === "git" && gitHasUpstream && (gitAhead > 0 || gitBehind > 0) && (
                <span
                  title={`${gitAhead > 0 ? `${gitAhead} to push` : ""}${gitAhead > 0 && gitBehind > 0 ? " · " : ""}${gitBehind > 0 ? `${gitBehind} to pull` : ""}`}
                  className="inline-flex items-center gap-0.5 text-xs font-semibold leading-none text-amber-500"
                  aria-label={`Git sync: ${gitAhead > 0 ? `↑${gitAhead}` : ""}${gitBehind > 0 ? `↓${gitBehind}` : ""}`}
                >
                  {gitAhead > 0 && <span>↑{gitAhead}</span>}
                  {gitBehind > 0 && <span>↓{gitBehind}</span>}
                </span>
              )}
            </TabsTrigger>
          );
        })}
      </TabsList>

      {/* Overflow "More" menu — appears only when the tabs don't fit */}
      {overflowing && (
        <div className="ml-1 shrink-0">
          <Select value={activeTab} onValueChange={onTabSelect}>
            <SelectTrigger
              aria-label="More tabs"
              className="h-8 w-8 justify-center border-0 bg-transparent p-0 text-muted-foreground hover:bg-muted hover:text-foreground [&>svg:last-child]:hidden"
            >
              <MoreHorizontal className="h-4 w-4" />
            </SelectTrigger>
            <SelectContent align="end" className="max-h-80">
              {mainTabs.map((tab) => {
                const Icon = tab.icon;
                const loadingState = loadingStates?.get(tabLoadKey(activeProjectHost, activeProjectPath, tab.id));
                const loading = loadingState?.phase === "refresh";
                const loadError = loadingState?.phase === "error";
                const count = tab.id === "sessions" ? sessionsCount + terminalCount : undefined;
                const gitCount = tab.id === "git" && gitTotal > 0 ? gitTotal : undefined;
                return (
                  <SelectItem key={tab.id} value={tab.id} aria-busy={loading || undefined}>
                    <span className="flex items-center gap-2">
                      <Icon className="w-3.5 h-3.5" />
                      {tab.label}
                      <TabLoadingIndicator active={loading} error={loadError} label={`Loading ${tab.label}`} />
                      {count !== undefined && (
                        <span className="ml-1 inline-flex items-center justify-center min-w-[1.1rem] h-4 px-1 rounded-full bg-accent text-[10px] font-semibold text-accent-foreground">
                          {count}
                        </span>
                      )}
                      {gitCount !== undefined && (
                        <span
                          title={gitTitle}
                          className="ml-1 inline-flex items-center justify-center min-w-[1.1rem] h-4 px-1 rounded-full bg-accent text-[10px] font-semibold text-accent-foreground"
                        >
                          {gitCount}
                        </span>
                      )}
                    </span>
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>
        </div>
      )}

      <div className="ml-auto flex items-center shrink-0">
        <PortMapsWidget />
        <SyncStatusWidget />
      </div>
    </header>
  );
}
