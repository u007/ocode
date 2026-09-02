// Shared keyword filtering — case-insensitive AND across whitespace-separated terms.
// Includes relevance scoring for ranked search results.

/**
 * Parse a search query into lowercase keywords split on whitespace.
 * Deduplicates to prevent "foo foo" from double-counting.
 */
export function parseKeywords(query: string): string[] {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const word of query.trim().toLowerCase().split(/\s+/).filter(Boolean)) {
    if (!seen.has(word)) {
      seen.add(word);
      result.push(word);
    }
  }
  return result;
}

/**
 * Check if all keywords appear in the haystack (case-insensitive AND).
 */
export function matchesKeywords(haystack: string, keywords: string[]): boolean {
  if (keywords.length === 0) return true;
  const lower = haystack.toLowerCase();
  return keywords.every((k) => lower.includes(k.toLowerCase()));
}

/**
 * Split a string into word tokens for boundary-aware matching.
 * Handles: camelCase, PascalCase, snake_case, kebab-case, dots, slashes, spaces.
 *
 * IMPORTANT: camelCase splitting happens BEFORE lowercasing so boundaries aren't lost.
 *
 * Examples:
 *   "FileTree" → ["file", "tree"]
 *   "OAuth" → ["o", "auth"]
 *   "auth_login" → ["auth", "login"]
 *   "auth-login" → ["auth", "login"]
 *   "FileTree.tsx" → ["file", "tree", "tsx"]
 *   "src/components/FileTree" → ["src", "components", "file", "tree"]
 *   "XMLParser" → ["xml", "parser"]
 *   "getHTTPSUrl" → ["get", "https", "url"]
 */
export function tokenize(s: string): string[] {
  const split = s
    // 1. Split camelCase/PascalCase BEFORE lowercasing (preserves boundaries)
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    // 2. Split consecutive uppercase before uppercase+lowercase (acronyms: HTTPS → HTTPS, XML → XML)
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    // 3. Lowercase AFTER camel splitting
    .toLowerCase()
    // 4. Split on common separators
    .replace(/[_\-/.\s]+/g, " ")
    .trim()
    .split(/\s+/)
    .filter(Boolean);
  return split;
}

/**
 * Check if keyword matches a whole word boundary in the text.
 * A "word" is defined by tokenization (camelCase, snake_case, etc.).
 */
function isWholeWordMatch(text: string, keyword: string): boolean {
  const tokens = tokenize(text);
  return tokens.includes(keyword);
}

/**
 * Score relevance of a haystack (filename + path) against keywords.
 *
 * Scoring formula (per keyword, summed):
 *   - Exact word match in filename: +10
 *   - Exact word match in path: +8
 *   - Substring match in filename: +5
 *   - Substring match in path: +1
 *
 * Exact word matches ALWAYS beat substring matches, regardless of filename vs path.
 * Filename matches beat path matches as tie-breaker.
 *
 * Returns total score (higher = more relevant).
 * Returns 0 if any keyword fails to match (AND semantics).
 * Returns -1 for empty keywords (matches everything equally).
 */
export function scoreMatch(haystack: string, keywords: string[]): number {
  if (keywords.length === 0) return 0; // empty query, no ranking

  const lower = haystack.toLowerCase();
  const lastSlash = lower.lastIndexOf("/");
  const filename = lastSlash >= 0 ? lower.slice(lastSlash + 1) : lower;
  const path = lastSlash >= 0 ? lower.slice(0, lastSlash) : "";

  // Use original case for word boundary detection (camelCase splitting needs it)
  const origLastSlash = haystack.lastIndexOf("/");
  const origFilename = origLastSlash >= 0 ? haystack.slice(origLastSlash + 1) : haystack;
  const origPath = origLastSlash >= 0 ? haystack.slice(0, origLastSlash) : "";

  let totalScore = 0;

  for (const kw of keywords) {
    const kwLower = kw.toLowerCase();

    // Check if keyword matches anywhere (AND requirement)
    if (!lower.includes(kwLower)) {
      return 0;
    }

    const inFilename = filename.includes(kwLower);
    const inPath = path.includes(kwLower);

    // Exact word match takes priority (highest weight)
    // Use original case for word boundary detection
    const exactInFilename = inFilename && isWholeWordMatch(origFilename, kwLower);
    const exactInPath = inPath && isWholeWordMatch(origPath, kwLower);

    if (exactInFilename) {
      totalScore += 10;
    } else if (exactInPath) {
      totalScore += 8;
    } else if (inFilename) {
      // Substring in filename
      totalScore += 5;
    } else if (inPath) {
      // Substring in path (lowest weight)
      totalScore += 1;
    }
  }

  return totalScore;
}
