export interface FileSearchFilters {
  exts: string;
  ignore: string;
  regex: boolean;
  caseSensitive: boolean;
  wholeWord: boolean;
  includeIgnored: boolean;
}

const STORAGE_KEY = "ocode.ui.fileSearchFilters.v1";

interface PersistedFile {
  version: 1;
  projects: Record<string, FileSearchFilters>;
}

const DEFAULTS: FileSearchFilters = {
  exts: "",
  ignore: "",
  regex: false,
  caseSensitive: false,
  wholeWord: false,
  includeIgnored: false,
};

function readFile(): PersistedFile {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return { version: 1, projects: {} };
    const parsed = JSON.parse(raw) as PersistedFile;
    if (!parsed || parsed.version !== 1 || typeof parsed.projects !== "object" || Array.isArray(parsed.projects) || parsed.projects === null) {
      return { version: 1, projects: {} };
    }
    return parsed;
  } catch {
    return { version: 1, projects: {} };
  }
}

function normalize(f: unknown): FileSearchFilters {
  const r = f as Partial<FileSearchFilters> | null;
  if (!r || typeof r !== "object" || Array.isArray(r)) return { ...DEFAULTS };
  return {
    exts: typeof r.exts === "string" ? r.exts : DEFAULTS.exts,
    ignore: typeof r.ignore === "string" ? r.ignore : DEFAULTS.ignore,
    regex: typeof r.regex === "boolean" ? r.regex : DEFAULTS.regex,
    caseSensitive: typeof r.caseSensitive === "boolean" ? r.caseSensitive : DEFAULTS.caseSensitive,
    wholeWord: typeof r.wholeWord === "boolean" ? r.wholeWord : DEFAULTS.wholeWord,
    includeIgnored: typeof r.includeIgnored === "boolean" ? r.includeIgnored : DEFAULTS.includeIgnored,
  };
}

export function loadFileSearchFilters(projectPath: string | undefined): FileSearchFilters {
  if (!projectPath) return { ...DEFAULTS };
  const file = readFile();
  const raw = file.projects[projectPath];
  if (!raw) return { ...DEFAULTS };
  return normalize(raw);
}

export function saveFileSearchFilters(projectPath: string | undefined, filters: FileSearchFilters): void {
  if (!projectPath) return;
  const file = readFile();
  // Normalize to avoid persisting stray fields
  const normalized = normalize(filters);
  const isDefault =
    normalized.exts === DEFAULTS.exts &&
    normalized.ignore === DEFAULTS.ignore &&
    normalized.regex === DEFAULTS.regex &&
    normalized.caseSensitive === DEFAULTS.caseSensitive &&
    normalized.wholeWord === DEFAULTS.wholeWord &&
    normalized.includeIgnored === DEFAULTS.includeIgnored;
  if (isDefault) {
    delete file.projects[projectPath];
  } else {
    file.projects[projectPath] = normalized;
  }
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(file));
  } catch {
    // ignore
  }
}

export const FILE_SEARCH_FILTERS_DEFAULTS = DEFAULTS;
