import { useEffect, useState } from "react";
import {
  Archive,
  BookOpen,
  Bug,
  ChartNoAxesColumn,
  Eye,
  FileCode,
  FileText,
  FlaskConical,
  Gauge,
  GitBranch,
  Hammer,
  ListChecks,
  MessagesSquare,
  Package,
  Play,
  Rocket,
  RefreshCw,
  Scissors,
  Search,
  ShieldCheck,
  Terminal,
  WandSparkles,
  Wrench,
  Zap,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { api } from "@/api/client";
import { eventBus } from "@/lib/eventBus";
import type { QuickActionChip, QuickActionMode, QuickActionSeed } from "@/api/types";

/**
 * quickActions — the composer's pill strip: pure helpers, the icon map, and a
 * module-level config store.
 *
 * Ownership: the SERVER seeds the three starters and is the validation
 * authority (spec §"Seed ownership — Go is the single source"). Nothing here
 * writes a default to disk and nothing here invents a chip; `SEED_CHIPS` is
 * only the degraded fallback a failed first fetch renders.
 */

/** Validation cap. The server rejects more than this; the form stops at it. */
export const QUICK_ACTIONS_MAX = 20;

/**
 * Icon keys, mirroring `quickActionIconAllowlist` in
 * `internal/config/quick_actions.go`. The duplication is deliberate: Go must
 * validate a key without a network round-trip, and TS must render a chip
 * before its fetch resolves. `npm run typecheck` is the gate for a bad lucide
 * import, and the allowlist tests are the gate for a key present in one side
 * and not the other.
 */
export const QUICK_ACTION_ICONS = [
  "zap",
  "archive",
  "play",
  "file-text",
  "search",
  "refresh-cw",
  "terminal",
  "git-branch",
  "hammer",
  "bug",
  "flask-conical",
  "shield-check",
  "list-checks",
  "wand-sparkles",
  "package",
  "book-open",
  "file-code",
  "messages-square",
  "rocket",
  "scissors",
  "wrench",
  "eye",
  "gauge",
  "chart-no-axes-column",
] as const;

const ICON_COMPONENTS: Record<string, LucideIcon> = {
  zap: Zap,
  archive: Archive,
  play: Play,
  "file-text": FileText,
  search: Search,
  "refresh-cw": RefreshCw,
  terminal: Terminal,
  "git-branch": GitBranch,
  hammer: Hammer,
  bug: Bug,
  "flask-conical": FlaskConical,
  "shield-check": ShieldCheck,
  "list-checks": ListChecks,
  "wand-sparkles": WandSparkles,
  package: Package,
  "book-open": BookOpen,
  "file-code": FileCode,
  "messages-square": MessagesSquare,
  rocket: Rocket,
  scissors: Scissors,
  wrench: Wrench,
  eye: Eye,
  gauge: Gauge,
  "chart-no-axes-column": ChartNoAxesColumn,
};

/** The default icon, reused both as the omitted-icon default and the fallback. */
const DEFAULT_ICON = "zap";

/**
 * Never throws: an unknown key renders the default icon rather than a blank
 * pill. A `lucide-react` upgrade that drops a key must degrade, not crash the
 * whole composer (spec Review Focus #5).
 */
export function quickActionIconComponent(key: string): LucideIcon {
  return ICON_COMPONENTS[key] ?? Zap;
}

const SEEDS: readonly QuickActionSeed[] = ["compact", "continue", "recap"];

/**
 * Ids the seeded starters occupy. A minted id must never collide with one,
 * because two chips sharing an id share a React key and one silently vanishes
 * (Review Focus #2).
 */
export const QUICK_ACTION_RESERVED_IDS: readonly string[] = ["compact", "continue", "recap"];

export function isQuickActionSeed(value: unknown): value is QuickActionSeed {
  return typeof value === "string" && (SEEDS as readonly string[]).includes(value);
}

export function isQuickActionMode(value: unknown): value is QuickActionMode {
  return value === "fill" || value === "send";
}

/**
 * DEGRADED FALLBACK ONLY — explicitly NOT a second authority. Go seeds the
 * starters and the GET returns them; this exists so a failed first fetch
 * renders the pre-feature strip instead of blanking it (spec §"Seed
 * ownership"). It must never be used as the source of truth, and it must never
 * be written to disk. If you change the starters in Go, change them here too
 * and say why in the commit.
 */
export const SEED_CHIPS: readonly QuickActionChip[] = [
  { id: "compact", label: "Compact", icon: "archive", message: "/compact", mode: "send", seed: "compact" },
  { id: "continue", label: "Continue", icon: "play", message: "continue", mode: "send", seed: "continue" },
  { id: "recap", label: "Recap", icon: "file-text", message: "/recap", mode: "send", seed: "recap" },
];

const text = (value: unknown): string => (typeof value === "string" ? value : "");

/** `null` means "this chip is not renderable": blank id, label, or message. */
export function normalizeQuickActionChip(input: unknown): QuickActionChip | null {
  if (!input || typeof input !== "object" || Array.isArray(input)) return null;
  const raw = input as Record<string, unknown>;
  const id = text(raw.id).trim();
  const label = text(raw.label).trim();
  // The message body is kept verbatim, like Go's Normalize: it is the exact
  // text dispatched, and every derived helper trims defensively instead.
  const message = text(raw.message);
  if (!id || !label || !message.trim()) return null;
  const icon = text(raw.icon);
  const mode = raw.mode;
  const seed = raw.seed;
  return {
    id,
    label,
    icon: icon || DEFAULT_ICON,
    message,
    // An ABSENT mode normalizes to "send" (today's behaviour for all three
    // starters). An EXPLICITLY invalid mode is preserved rather than repaired,
    // so the server rejects it and the user sees why instead of this layer
    // silently substituting a different value.
    mode: isQuickActionMode(mode)
      ? mode
      : mode === undefined || mode === null || mode === ""
        ? "send"
        : (mode as QuickActionMode),
    ...(isQuickActionSeed(seed) ? { seed } : {}),
  };
}

/** Array order is the user's sort order, so it is preserved verbatim. */
export function normalizeQuickActions(input: unknown): QuickActionChip[] {
  const chips = (input as { chips?: unknown } | null)?.chips;
  if (!Array.isArray(chips)) return [];
  return chips.map(normalizeQuickActionChip).filter((c): c is QuickActionChip => c !== null);
}

/** True when clicking this chip runs a compaction, so the pill can dim. */
export function chipDispatchesCompact(chip: QuickActionChip): boolean {
  const trimmed = chip.message.trim();
  return trimmed === "/compact" || trimmed.startsWith("/compact ");
}

/** Seeded chips inherit the hide-until-history rule; custom chips never hide. */
export function chipRequiresHistory(chip: QuickActionChip): boolean {
  return chip.seed != null;
}

/** A leading `/` routes to the slash-command pipeline; anything else is text. */
export function chipDispatchKind(chip: QuickActionChip): "command" | "message" {
  return chip.message.trim().startsWith("/") ? "command" : "message";
}

/**
 * Per-chip visibility. The old gate wrapped the whole strip in `hasConversation`,
 * which would hide an always-useful custom chip along with the seeded three.
 */
export function visibleChips(chips: readonly QuickActionChip[], hasConversation: boolean): QuickActionChip[] {
  return chips.filter((chip) => hasConversation || !chipRequiresHistory(chip));
}

/**
 * Collision-proof against both the existing ids and the reserved starter slugs.
 * Two chips sharing an id share a React key and one vanishes with no error.
 */
export function mintQuickActionId(existing: readonly QuickActionChip[]): string {
  const taken = new Set<string>([...QUICK_ACTION_RESERVED_IDS, ...existing.map((c) => c.id)]);
  let n = 1;
  let candidate = `chip-${n}`;
  while (taken.has(candidate)) {
    n += 1;
    candidate = `chip-${n}`;
  }
  return candidate;
}

export interface QuickActionsState {
  chips: QuickActionChip[];
  loading: boolean;
  error: string | null;
  revision: string;
}

type Listener = (state: QuickActionsState) => void;

let cached: QuickActionsState | null = null;
let inFlight: Promise<QuickActionsState> | null = null;
const listeners = new Set<Listener>();

/** Stable key so consumers rebase only when a rendered field actually changed. */
export function quickActionsRevision(chips: readonly QuickActionChip[]): string {
  return chips.map((c) => `${c.id}:${c.label}:${c.icon}:${c.message}:${c.mode}:${c.seed ?? ""}`).join("|");
}

function stateFromChips(
  chips: QuickActionChip[],
  error: string | null = null,
  loading = false,
): QuickActionsState {
  return { chips, error, loading, revision: quickActionsRevision(chips) };
}

function publish(next: QuickActionsState): QuickActionsState {
  cached = next;
  for (const listener of listeners) listener(next);
  return next;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Refresh from the local server. Event payloads are intentionally ignored; the
 * GET is the authority, so a stale payload cannot win over fresh state.
 */
export async function refreshQuickActions(): Promise<QuickActionsState> {
  if (inFlight) return inFlight;
  inFlight = api
    .getQuickActionsConfig()
    .then((response) => publish(stateFromChips(normalizeQuickActions(response))))
    .catch((error: unknown) => {
      const message = errorMessage(error);
      if (cached) {
        // Retain: a transient failure must not blank a strip the user is
        // actively using, and must not resurrect starters a user deleted.
        console.warn("[quick-actions] config request failed; retaining cached chips", { error });
        return publish({ ...cached, loading: false, error: message });
      }
      console.warn("[quick-actions] config request failed; using the pre-feature default strip", {
        error,
        reason: "no-cached-chips",
      });
      return publish(stateFromChips([...SEED_CHIPS], message));
    })
    .finally(() => {
      inFlight = null;
    });
  return inFlight;
}

/** Persist a new strip and publish the SERVER's answer, never the local draft. */
export async function saveQuickActions(chips: QuickActionChip[]): Promise<QuickActionsState> {
  const response = await api.setQuickActionsConfig({ chips });
  return publish(stateFromChips(normalizeQuickActions(response)));
}

export function useQuickActions(): QuickActionsState {
  const [state, setState] = useState<QuickActionsState>(() => cached ?? stateFromChips([], null, true));

  useEffect(() => {
    const listener: Listener = (next) => setState(next);
    listeners.add(listener);

    // A GLOBAL config event, published with an empty session id exactly like
    // `chat_verbosity_changed`. It is therefore NOT a session-scoped event and
    // must not be added to SESSION_SCOPED_EVENTS in sessionEvents.ts.
    const offChanged = eventBus.on("quick_actions_changed", () => {
      void refreshQuickActions();
    });
    const offReconnect = eventBus.onReconnect(() => {
      void refreshQuickActions();
    });

    if (cached === null) void refreshQuickActions();

    return () => {
      listeners.delete(listener);
      offChanged();
      offReconnect();
    };
  }, []);

  return state;
}

export function __resetQuickActionsForTests(): void {
  cached = null;
  inFlight = null;
  listeners.clear();
}