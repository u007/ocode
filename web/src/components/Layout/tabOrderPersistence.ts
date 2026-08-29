// Persists the merged (chat session + terminal) tab order per project, so
// drag-reordering the unified tab bar survives reloads. Only stores an
// ordering of composite keys — never the tabs' own data (title, etc.),
// which stays owned by projectStore/terminalStore.

export type UnifiedTabKey = `chat:${string}` | `term:${string}`;

const ORDER_KEY = "ocode.ui.tabOrder.v1";

interface OrderFile {
  version: 1;
  projects: Record<string, UnifiedTabKey[]>;
}

function readOrderFile(): OrderFile {
  try {
    const raw = window.localStorage.getItem(ORDER_KEY);
    if (!raw) return { version: 1, projects: {} };
    const parsed = JSON.parse(raw) as OrderFile;
    if (!parsed || parsed.version !== 1 || typeof parsed.projects !== "object") {
      return { version: 1, projects: {} };
    }
    return parsed;
  } catch (err) {
    console.error("Failed to load tab order:", err);
    return { version: 1, projects: {} };
  }
}

export function loadTabOrder(projectPath: string): UnifiedTabKey[] {
  return readOrderFile().projects[projectPath] ?? [];
}

export function saveTabOrder(projectPath: string, order: UnifiedTabKey[]) {
  const file = readOrderFile();
  if (order.length === 0) {
    delete file.projects[projectPath];
  } else {
    file.projects[projectPath] = order;
  }
  try {
    window.localStorage.setItem(ORDER_KEY, JSON.stringify(file));
  } catch (err) {
    console.error("Failed to persist tab order:", err);
  }
}

/** Reconciles a saved order against the live id sets: drops stale keys, and
 *  appends any live id missing from the saved order (new tabs created since
 *  last save) at the end, in the order they appear in chatIds/terminalIds. */
export function reconcileTabOrder(
  saved: UnifiedTabKey[],
  chatIds: string[],
  terminalIds: string[],
): UnifiedTabKey[] {
  const liveKeys: UnifiedTabKey[] = [
    ...chatIds.map((id): UnifiedTabKey => `chat:${id}`),
    ...terminalIds.map((id): UnifiedTabKey => `term:${id}`),
  ];
  const liveKeySet = new Set(liveKeys);
  const kept = saved.filter((k) => liveKeySet.has(k));
  const keptSet = new Set(kept);
  const appended = liveKeys.filter((k) => !keptSet.has(k));
  return [...kept, ...appended];
}
