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
 *
 * Only ids registered by the bundled `monaco-editor` basic-languages
 * contribution are used; anything without a grammar stays `plaintext` (Monaco
 * logs a warning and falls back for an unregistered id, so a wrong id here is
 * worse than plaintext). `previewKind.ts` decides whether a file is text at
 * all; this map only picks the highlighter.
 */
const LANGUAGE_BY_EXT: Record<string, string> = {
  // JS/TS family.
  ts: "typescript",
  tsx: "typescript",
  js: "javascript",
  jsx: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  go: "go",
  py: "python",
  rb: "ruby",
  rs: "rust",
  java: "java",
  kt: "kotlin",
  kts: "kotlin",
  swift: "swift",
  cs: "csharp",
  fs: "fsharp",
  fsx: "fsharp",
  fsi: "fsharp",
  vb: "vb",
  php: "php",
  pl: "perl",
  pm: "perl",
  r: "r",
  lua: "lua",
  ex: "elixir",
  exs: "elixir",
  clj: "clojure",
  cljs: "clojure",
  cljc: "clojure",
  scala: "scala",
  sc: "scala",
  dart: "dart",
  jl: "julia",
  sol: "solidity",
  cypher: "cypher",
  cql: "cypher",
  sparql: "sparql",
  rq: "sparql",
  coffee: "coffee",
  c: "c",
  h: "c",
  cpp: "cpp",
  cc: "cpp",
  cxx: "cpp",
  "c++": "cpp",
  hpp: "cpp",
  hh: "cpp",
  hxx: "cpp",
  css: "css",
  scss: "scss",
  sass: "scss",
  less: "less",
  html: "html",
  htm: "html",
  xhtml: "html",
  vue: "html",
  svelte: "html",
  pug: "pug",
  jade: "pug",
  hbs: "handlebars",
  handlebars: "handlebars",
  twig: "twig",
  liquid: "liquid",
  rst: "restructuredtext",
  json: "json",
  jsonc: "json",
  xml: "xml",
  xsl: "xml",
  xslt: "xml",
  svg: "xml",
  yaml: "yaml",
  yml: "yaml",
  ini: "ini",
  cfg: "ini",
  conf: "ini",
  properties: "ini",
  editorconfig: "ini",
  md: "markdown",
  markdown: "markdown",
  mdx: "mdx",
  sql: "sql",
  ddl: "sql",
  dml: "sql",
  mysql: "mysql",
  pgsql: "pgsql",
  proto: "protobuf",
  tf: "hcl",
  tfvars: "hcl",
  hcl: "hcl",
  bat: "bat",
  cmd: "bat",
  ps1: "powershell",
  psm1: "powershell",
  psd1: "powershell",
  sh: "shell",
  bash: "shell",
  zsh: "shell",
  dockerfile: "dockerfile",
  graphql: "graphql",
  gql: "graphql",
  // No bundled grammar — keep explicit so intent is documented.
  toml: "plaintext",
};

/** Monaco language identifier for `filePath` (extension-based). */
export function languageForFile(filePath: string): string {
  const ext = filePath.split(".").pop()?.toLowerCase() || "";
  return LANGUAGE_BY_EXT[ext] ?? "plaintext";
}
