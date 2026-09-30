/**
 * Pure helpers for the "recent inputs" strip rendered above the chat composer
 * (web + desktop, which share this UI).
 *
 * Purpose: after a long turn or a compaction you can no longer see your own
 * prompts in the transcript, and re-deriving what you asked for is expensive.
 * The strip is a quiet, read-only reminder of the last couple of things you
 * actually typed.
 *
 * ── Why this is NOT `isCountableUserMessage` (lib/userMessageNav.ts) ────────
 * That predicate answers a different question — "does this message count
 * towards the alt+↑/↓ jump denominator, which must agree with the TUI" — so it
 * only excludes slash-command echoes. It deliberately still counts
 * system-injected user-role messages, and those are NOT hypothetical: they are
 * persisted with role "user" and dominate real transcripts. Measured over 400
 * recent session transcripts: 113 × "[advisor plan checkpoint]", 82 ×
 * "[advisor completion checkpoint]", 7 × "[ocode:event]".
 *
 * Showing those would fill a 2-line strip with advisor prose, so this module
 * composes that predicate and adds a marker denylist. **The two must not be
 * "unified"** — the jump readout staying consistent across surfaces is worth
 * more than it costs to keep a second, stricter rule here. When a NEW injected
 * marker is introduced, add it to INJECTED_PREFIXES below.
 */

import { isCountableUserMessage } from "./userMessageNav";

/** How many inputs the strip shows. */
export const RECENT_INPUTS_MAX = 2;

/**
 * Prefixes of persisted user-role messages the agent writes itself, so they are
 * not what the user typed. Matched against the TRIMMED content.
 *
 * - `"[ocode:"` is a prefix rule, not an enumeration: every injected tail
 *   (`[ocode:todo]`, `[ocode:lsp]`, `[ocode:notes]`, `[ocode:event]`,
 *   `[ocode:context]`, `[ocode:selection]`, `[ocode:discovery]`) is covered by
 *   it, including markers added after this file was written.
 * - The two advisor checkpoints are the other persisted user-role injections
 *   (internal/agent/advisor_checkpoint.go).
 */
const INJECTED_PREFIXES = ["[advisor plan checkpoint]", "[advisor completion checkpoint]", "[ocode:"];

type MessageLike = { role?: string; content?: string };

/** True when `msg` is a real typed prompt rather than chrome or an injection. */
export function isRecentUserInput(msg: MessageLike): boolean {
  if (!isCountableUserMessage(msg)) return false;
  const trimmed = (msg.content ?? "").trim();
  if (trimmed.length === 0) return false;
  return !INJECTED_PREFIXES.some((prefix) => trimmed.startsWith(prefix));
}

/**
 * The last `limit` real inputs, OLDEST → NEWEST, so the newest sits closest to
 * the composer caret. Walks backwards and stops at `limit`, so this is cheap on
 * a long transcript regardless of how many messages are loaded.
 */
export function recentUserInputs(
  messages: ReadonlyArray<MessageLike>,
  limit: number = RECENT_INPUTS_MAX,
): string[] {
  if (limit <= 0) return [];
  const picked: string[] = [];
  for (let i = messages.length - 1; i >= 0 && picked.length < limit; i--) {
    const msg = messages[i];
    if (!isRecentUserInput(msg)) continue;
    picked.push((msg.content ?? "").trim());
  }
  return picked.reverse();
}
