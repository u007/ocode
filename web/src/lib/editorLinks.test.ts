import { describe, it, expect, beforeEach, vi } from "vitest";
import {
  isFileLike,
  hasKnownExtension,
  resolveImportPath,
  candidatePaths,
  splitPathToken,
  findLinksInLine,
  findLinksInModelText,
  findImportSymbolLinks,
  findDeclarationLine,
  ensureEditorLinks,
  __resetEditorLinksForTest,
} from "./editorLinks";
import { OPEN_FILE_EVENT, type OpenFileDetail } from "./fileLinks";

describe("editorLinks", () => {
  beforeEach(() => {
    __resetEditorLinksForTest();
    vi.restoreAllMocks();
  });

  describe("hasKnownExtension", () => {
    it("recognizes known extensions", () => {
      expect(hasKnownExtension("foo.ts")).toBe(true);
      expect(hasKnownExtension("a/b/c.tsx")).toBe(true);
      expect(hasKnownExtension("file.go")).toBe(true);
      expect(hasKnownExtension("note.md")).toBe(true);
      expect(hasKnownExtension("package.json")).toBe(true);
    });
    it("rejects unknown", () => {
      expect(hasKnownExtension("foo")).toBe(false);
      expect(hasKnownExtension("lodash/fp")).toBe(false);
      expect(hasKnownExtension("react")).toBe(false);
    });
  });

  describe("isFileLike", () => {
    it("accepts relative paths", () => {
      expect(isFileLike("./utils")).toBe(true);
      expect(isFileLike("../foo/bar")).toBe(true);
      expect(isFileLike("/absolute/path.ts")).toBe(true);
      expect(isFileLike("./utils.ts:10")).toBe(true);
    });
    it("accepts extension-bearing paths", () => {
      expect(isFileLike("src/foo.ts")).toBe(true);
      expect(isFileLike("web/src/App.tsx:42:5")).toBe(true);
      expect(isFileLike("utils.ts")).toBe(true);
    });
    it("rejects bare package imports", () => {
      expect(isFileLike("react")).toBe(false);
      expect(isFileLike("lodash/fp")).toBe(false);
      expect(isFileLike("react-dom/client")).toBe(false);
      expect(isFileLike("@scope/pkg")).toBe(false);
      expect(isFileLike("@babel/core")).toBe(false);
    });
    it("rejects URLs", () => {
      expect(isFileLike("https://host/foo.ts")).toBe(false);
      expect(isFileLike("http://example.com/bar.js")).toBe(false);
      expect(isFileLike("data:text/plain,hi")).toBe(false);
    });
    it("rejects slash without extension nor relative prefix", () => {
      expect(isFileLike("src/foo/bar")).toBe(false);
      expect(isFileLike("a/b")).toBe(false);
    });
  });

  describe("splitPathToken", () => {
    it("splits :line and :line:col", () => {
      expect(splitPathToken("src/foo.ts:12:3")).toEqual({ path: "src/foo.ts", line: 12, col: 3 });
      expect(splitPathToken("src/foo.ts:42")).toEqual({ path: "src/foo.ts", line: 42, col: undefined });
      expect(splitPathToken("src/foo.ts")).toEqual({ path: "src/foo.ts", line: undefined, col: undefined });
    });
  });

  describe("resolveImportPath", () => {
    it("resolves ./ relative to current file dir", () => {
      expect(resolveImportPath("./utils", "src/components/Files/FileEditor.tsx")).toBe("src/components/Files/utils");
      expect(resolveImportPath("./utils.ts", "src/components/Files/FileEditor.tsx")).toBe("src/components/Files/utils.ts");
    });
    it("resolves ../ relative", () => {
      expect(resolveImportPath("../other/file.ts", "src/components/Files/FileEditor.tsx")).toBe("src/components/other/file.ts");
      expect(resolveImportPath("../../lib/foo.ts", "src/components/Files/FileEditor.tsx")).toBe("src/lib/foo.ts");
    });
    it("resolves absolute / as project-root relative", () => {
      expect(resolveImportPath("/src/foo.ts", "src/bar.ts")).toBe("src/foo.ts");
    });
    it("resolves project-relative with slash and extension", () => {
      expect(resolveImportPath("src/bar.ts", "src/foo.ts")).toBe("src/bar.ts");
    });
    it("strips :line suffix before resolving", () => {
      expect(resolveImportPath("./utils.ts:10", "src/foo.ts")).toBe("src/utils.ts");
      expect(resolveImportPath("src/foo.ts:12:3", "src/bar.ts")).toBe("src/foo.ts");
    });
    it("rejects bare package", () => {
      expect(resolveImportPath("react", "src/foo.ts")).toBeNull();
      expect(resolveImportPath("lodash/fp", "src/foo.ts")).toBeNull();
      expect(resolveImportPath("@scope/pkg", "src/foo.ts")).toBeNull();
    });
    it("rejects URL", () => {
      expect(resolveImportPath("https://host/foo.ts", "src/foo.ts")).toBeNull();
    });
    it("rejects escape above root", () => {
      expect(resolveImportPath("../../../etc/passwd", "src/foo.ts")).toBeNull();
    });
  });

  describe("candidatePaths", () => {
    it("returns single path if already has extension", () => {
      expect(candidatePaths("src/foo.ts")).toEqual(["src/foo.ts"]);
    });
    it("generates extension probes for extensionless", () => {
      const cands = candidatePaths("src/components/Files/utils");
      expect(cands).toContain("src/components/Files/utils");
      expect(cands).toContain("src/components/Files/utils.ts");
      expect(cands).toContain("src/components/Files/utils.tsx");
      expect(cands).toContain("src/components/Files/utils/index.ts");
    });
  });

  describe("findLinksInLine", () => {
    it("finds quoted relative import", () => {
      const links = findLinksInLine('import x from "./utils"');
      expect(links.length).toBe(1);
      expect(links[0].path).toBe("./utils");
      expect(links[0].quoted).toBe(true);
    });
    it("finds quoted extension import", () => {
      const links = findLinksInLine('import {a} from "../other/file.ts"');
      expect(links.length).toBe(1);
      expect(links[0].path).toBe("../other/file.ts");
    });
    it("ignores bare package quoted", () => {
      expect(findLinksInLine('import React from "react"')).toEqual([]);
      expect(findLinksInLine('import fp from "lodash/fp"')).toEqual([]);
      expect(findLinksInLine('import x from "@scope/pkg"')).toEqual([]);
    });
    it("finds bare path token with line number", () => {
      const links = findLinksInLine("// see src/foo.ts:10 for details");
      expect(links.length).toBe(1);
      expect(links[0].raw).toBe("src/foo.ts:10");
      expect(links[0].line).toBe(10);
    });
    it("does not linkify URL interior", () => {
      const links = findLinksInLine("fetch https://host/foo.ts and check");
      // BARE_PATH_RE has guard, so should not match foo.ts inside URL
      const hasFoo = links.some((l) => l.path.includes("foo.ts"));
      expect(hasFoo).toBe(false);
    });
    it("finds multiple links", () => {
      const links = findLinksInLine('import a from "./a" and "./b.ts"');
      expect(links.length).toBe(2);
    });
    it("handles line:col suffix inside quotes", () => {
      const links = findLinksInLine('const p = "./foo.ts:12:3"');
      expect(links.length).toBe(1);
      expect(links[0].line).toBe(12);
      expect(links[0].col).toBe(3);
    });
    it("ignores plain word without path", () => {
      expect(findLinksInLine("hello world")).toEqual([]);
    });
  });

  describe("findLinksInModelText", () => {
    it("scans multiple lines", () => {
      const text = 'import x from "./a"\n// see web/src/App.tsx:42\nconst y = "react"';
      const all = findLinksInModelText(text);
      expect(all.length).toBe(2);
      expect(all[0].lineNumber).toBe(1);
      expect(all[1].lineNumber).toBe(2);
    });
  });

  describe("findImportSymbolLinks", () => {
    it("links default and named import symbols to the module path", () => {
      const links = findImportSymbolLinks('import Foo, { bar, baz as qux, type Kind } from "./mod";');
      expect(links.map((l) => [l.symbol, l.isDefault])).toEqual([
        ["Foo", true],
        ["bar", false],
        ["baz", false],
        ["Kind", false],
      ]);
      for (const l of links) expect(l.path).toBe("./mod");
      const line = 'import Foo, { bar, baz as qux, type Kind } from "./mod";';
      expect(line.slice(links[0].start, links[0].end)).toBe("Foo");
      expect(line.slice(links[2].start, links[2].end)).toBe("baz");
    });
    it("ignores bare package imports and namespace imports", () => {
      expect(findImportSymbolLinks('import React, { useState } from "react";')).toEqual([]);
      expect(findImportSymbolLinks('import * as ns from "./mod";')).toEqual([]);
    });
    it("handles re-exports", () => {
      const links = findImportSymbolLinks('export { thing } from "../lib/thing";');
      expect(links.map((l) => l.symbol)).toEqual(["thing"]);
    });
  });

  describe("findDeclarationLine", () => {
    const src = [
      "import x from './x';",
      "export interface Kind {}",
      "export const bar = 1;",
      "export async function baz() {}",
      "function local() {}",
      "export { local as renamed };",
      "export default function Foo() {}",
    ].join("\n");
    it("finds named declarations", () => {
      expect(findDeclarationLine(src, "Kind", false)).toBe(2);
      expect(findDeclarationLine(src, "bar", false)).toBe(3);
      expect(findDeclarationLine(src, "baz", false)).toBe(4);
      expect(findDeclarationLine(src, "renamed", false)).toBe(6);
    });
    it("finds default export", () => {
      expect(findDeclarationLine(src, "Whatever", true)).toBe(7);
    });
    it("returns null when missing", () => {
      expect(findDeclarationLine(src, "nope", false)).toBeNull();
    });
  });

  describe("ensureEditorLinks opener", () => {
    function fakeMonaco() {
      let opener: { open(uri: unknown): boolean | Promise<boolean> } | null = null;
      const monaco = {
        languages: { registerLinkProvider: () => ({ dispose() {} }) },
        editor: {
          registerLinkOpener(o: { open(uri: unknown): boolean | Promise<boolean> }) {
            opener = o;
            return { dispose() {} };
          },
        },
      };
      return { monaco, getOpener: () => opener! };
    }
    function payloadUri(payload: Record<string, unknown>) {
      // Monaco hands the opener a Uri whose query is already percent-decoded.
      return { scheme: "ocode-file", authority: "open", query: `d=${JSON.stringify(payload)}`, path: "" };
    }

    it("opens the resolved import target when handed a Monaco Uri", async () => {
      const { monaco, getOpener } = fakeMonaco();
      ensureEditorLinks(monaco as never);
      vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
        const url = String(input);
        const ok = url.includes("path=web%2Fsrc%2Fapi%2Fclient.ts");
        return new Response(ok ? JSON.stringify({ content: "export const api = 1;" }) : "nope", {
          status: ok ? 200 : 404,
        });
      });
      const events: OpenFileDetail[] = [];
      const handler = (e: Event) => events.push((e as CustomEvent<OpenFileDetail>).detail);
      window.addEventListener(OPEN_FILE_EVENT, handler);
      try {
        const handled = await getOpener().open(
          payloadUri({
            raw: "../../api/client",
            path: "../../api/client",
            currentPath: "web/src/components/Files/FileEditor.tsx",
            projectRoot: "/p",
          }),
        );
        expect(handled).toBe(true);
        expect(events).toEqual([{ path: "web/src/api/client.ts", line: undefined, projectRoot: "/p" }]);
      } finally {
        window.removeEventListener(OPEN_FILE_EVENT, handler);
      }
    });

    it("jumps to the declaration line for an imported symbol", async () => {
      const { monaco, getOpener } = fakeMonaco();
      ensureEditorLinks(monaco as never);
      vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
        const ok = String(input).includes("path=web%2Fsrc%2Fapi%2Fclient.ts");
        return new Response(
          ok ? JSON.stringify({ content: "const a = 1;\nexport function api() {}\n" }) : "nope",
          { status: ok ? 200 : 404 },
        );
      });
      const events: OpenFileDetail[] = [];
      const handler = (e: Event) => events.push((e as CustomEvent<OpenFileDetail>).detail);
      window.addEventListener(OPEN_FILE_EVENT, handler);
      try {
        const handled = await getOpener().open(
          payloadUri({
            raw: "../../api/client",
            path: "../../api/client",
            symbol: "api",
            isDefault: false,
            currentPath: "web/src/components/Files/FileEditor.tsx",
            projectRoot: "/p",
          }),
        );
        expect(handled).toBe(true);
        expect(events).toEqual([{ path: "web/src/api/client.ts", line: 2, projectRoot: "/p" }]);
      } finally {
        window.removeEventListener(OPEN_FILE_EVENT, handler);
      }
    });

    it("ignores non ocode-file uris", () => {
      const { monaco, getOpener } = fakeMonaco();
      ensureEditorLinks(monaco as never);
      expect(getOpener().open({ scheme: "https", query: "", path: "/x" })).toBe(false);
    });
  });
});
