import type { UmlSymbol } from "../../api/types";

export interface UmlFilter {
  query: string;
  langs: Set<string>;
  kinds: Set<string>;
  pkg: string; // "all" or a package id
}

export const DEFAULT_KINDS = ["struct", "interface", "func", "method"] as const;
export const DEFAULT_LANGS = ["Go"] as const;

export function matchesFilter(s: UmlSymbol, f: UmlFilter): boolean {
  if (!f.langs.has(s.lang)) return false;
  if (!f.kinds.has(s.kind)) return false;
  if (f.pkg !== "all" && s.pkg !== f.pkg) return false;
  const q = f.query.trim().toLowerCase();
  if (!q) return true;
  const hay = [s.name, s.file, s.pkg, s.sig ?? "", s.doc ?? "", s.receiver ?? "", ...(s.members ?? []).map((m) => m.name)]
    .join(" ")
    .toLowerCase();
  return q.split(/\s+/).every((tok) => hay.includes(tok));
}

/** Cap rendered rows; virtualization is unnecessary below this size. */
export const LIST_RENDER_CAP = 400;
