/**
 * editorDraftGuard — the web half of the desktop quit guard.
 *
 * An editor tab's unsaved edits live in memory and are mirrored into
 * localStorage (see editorTabsPersistence). Until that mirror lands, the edits
 * exist only in memory, so quitting would lose them. Two states are tracked:
 *
 *   - pending: a debounced draft write is still in flight (or has not been
 *     scheduled yet after a keystroke). Nothing has gone wrong; the guard just
 *     must not let the user quit on top of unpersisted work.
 *   - failed: the localStorage write itself failed (quota exhausted, storage
 *     unavailable). Surfaced as a sticky error toast.
 *
 * Either state blocks quit on the desktop shell
 * (internal/desktop.QuitGuard) until it clears. The native side owns the
 * actual block (ShouldQuit + a WindowClosing hook); this module only reports
 * transitions over the minimal Wails bridge (`window._wails.invoke` →
 * application.RawMessageHandler). In a plain browser there is no bridge, so
 * only the toast shows.
 */
import { reportActionErrorMessage } from "./actionErrors";
import { invokeWails } from "./wails";

/** Tab id → file path, for every dirty tab whose draft failed to persist. */
const failed = new Map<string, string>();
/** Tab id → file path, for edits still in memory with no draft written yet. */
const pending = new Map<string, string>();
/** Whether we last told native to block; avoids re-sending on every keystroke. */
let nativeBlocked = false;

function baseName(path: string): string {
  const base = path.split("/").pop();
  return base && base.length > 0 ? base : path;
}

/** ": a.txt, b.txt, +2 more" (or "" when empty) for display in a message. */
function fileList(entries: Map<string, string>): string {
  const names = [...entries.values()].map(baseName);
  if (names.length === 0) return "";
  const shown = names.slice(0, 3).join(", ");
  const extra = names.length > 3 ? `, +${names.length - 3} more` : "";
  return `: ${shown}${extra}`;
}

function failureMessage(): string {
  return `Unsaved changes couldn't be saved locally (browser storage is full or unavailable)${fileList(failed)}. ocode won't quit until they're saved — press ⌘S / Ctrl+S to save them to disk.`;
}

function pendingMessage(): string {
  const edits = pending.size === 1 ? "An unsaved edit" : `${pending.size} unsaved edits`;
  return `${edits} hasn't been written to local storage yet${fileList(pending)}. ocode won't quit until it is — wait a moment, or use "Quit anyway".`;
}

function message(): string {
  if (failed.size > 0) return failureMessage();
  if (pending.size > 0) return pendingMessage();
  return "Unsaved changes couldn't be saved — ocode won't quit until they're saved.";
}

/** Report a transition to/from the blocked state to the desktop shell. */
function syncNative(): void {
  const block = failed.size > 0 || pending.size > 0;
  if (block === nativeBlocked) return;
  nativeBlocked = block;
  invokeWails(block ? `ocode:quit-guard:blocked:${message()}` : "ocode:quit-guard:clear");
}

/**
 * Record that `tabId` holds edits that are not written to localStorage yet.
 * Called on every keystroke, so it must stay cheap and silent — the user sees
 * nothing here; the toast only appears if a write actually fails or they try to
 * quit.
 */
export function noteDraftPersistPending(tabId: string, path: string): void {
  pending.set(tabId, path);
  syncNative();
}

/**
 * Record that `tabId`'s draft could not be written. Idempotent per tab:
 * re-failing keeps one entry and (re-)raises the sticky toast, but only the
 * first transition notifies native.
 */
export function noteDraftPersistFailure(tabId: string, path: string): void {
  pending.delete(tabId);
  failed.set(tabId, path);
  reportActionErrorMessage(message());
  syncNative();
}

/** Record that `tabId` is persisted again (draft written, saved to disk,
 *  reloaded, or the tab closed/discarded). */
export function noteDraftPersistSuccess(tabId: string): void {
  pending.delete(tabId);
  failed.delete(tabId);
  syncNative();
}

/**
 * Drop guard entries for tabs that are no longer dirty (or no longer open).
 * Called from a useEditorTabs effect so close/discard/reload/file-delete paths
 * cannot leave a stale block behind.
 */
export function reconcileDraftGuard(liveDirtyTabIds: Iterable<string>): void {
  const live = new Set(liveDirtyTabIds);
  for (const id of [...pending.keys()]) {
    if (!live.has(id)) pending.delete(id);
  }
  for (const id of [...failed.keys()]) {
    if (!live.has(id)) failed.delete(id);
  }
  syncNative();
}

/** True while at least one edit is in memory without a persisted draft. */
export function draftPersistenceBlocked(): boolean {
  return failed.size > 0 || pending.size > 0;
}

/** The message to re-surface when the native quit attempt is refused. */
export function draftPersistenceMessage(): string {
  return message();
}

/**
 * Listen for the desktop shell's "quit was refused" event and re-raise it as a
 * toast (the user may have dismissed the original one). Returns an uninstall
 * function. No-op in a plain browser: the event is simply never dispatched.
 */
export function installQuitBlockedListener(): () => void {
  const handler = () => reportActionErrorMessage(draftPersistenceMessage());
  window.addEventListener("ocode:quit-blocked", handler);
  return () => window.removeEventListener("ocode:quit-blocked", handler);
}

/** Test-only: reset the guard and its native-notification latch. */
export function __resetDraftGuardForTests(): void {
  failed.clear();
  pending.clear();
  nativeBlocked = false;
}
