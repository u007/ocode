// Shared keyword filtering — case-insensitive AND across whitespace-separated terms.
export function parseKeywords(query: string): string[] {
  return query.trim().toLowerCase().split(/\s+/).filter(Boolean);
}

export function matchesKeywords(haystack: string, keywords: string[]): boolean {
  if (keywords.length === 0) return true;
  const lower = haystack.toLowerCase();
  return keywords.every((k) => lower.includes(k.toLowerCase()));
}
