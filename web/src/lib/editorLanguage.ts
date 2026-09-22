/**
 * Monaco language id for a file path, derived from its extension.
 *
 * Shared by every Monaco surface — `FileEditor` (editor tabs) and
 * `TextViewer` (the preview `text` kind) — so the two mappings cannot drift.
 * Unknown extensions fall back to `plaintext`.
 *
 * `.mdx` maps to Monaco's bundled `mdx` grammar (Markdown with JSX
 * awareness), not `markdown`: the grammar registers itself in the
 * `monaco-editor` basic-languages contribution, which `lib/monaco-setup.ts`
 * loads alongside the editor. `markdown` is included for the `.markdown`
 * extension, which `previewKind.ts` also treats as Markdown.
 */
const LANGUAGE_BY_EXT: Record<string, string> = {
  ts: "typescript",
  tsx: "typescript",
  js: "javascript",
  jsx: "javascript",
  go: "go",
  py: "python",
  rb: "ruby",
  rs: "rust",
  java: "java",
  kt: "kotlin",
  swift: "swift",
  c: "c",
  h: "c",
  cpp: "cpp",
  hpp: "cpp",
  css: "css",
  scss: "scss",
  less: "less",
  html: "html",
  json: "json",
  xml: "xml",
  yaml: "yaml",
  yml: "yaml",
  md: "markdown",
  markdown: "markdown",
  mdx: "mdx",
  sql: "sql",
  sh: "shell",
  bash: "shell",
  zsh: "shell",
  dockerfile: "dockerfile",
  toml: "plaintext",
  tf: "terraform",
  dart: "dart",
  vue: "html",
  svelte: "html",
  graphql: "graphql",
  gql: "graphql",
};

/** Monaco language identifier for `filePath` (extension-based). */
export function languageForFile(filePath: string): string {
  const ext = filePath.split(".").pop()?.toLowerCase() || "";
  return LANGUAGE_BY_EXT[ext] ?? "plaintext";
}
