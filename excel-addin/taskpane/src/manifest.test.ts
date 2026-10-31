// Structural checks on the generated sideload manifest. Microsoft's own validator
// (`npx office-addin-manifest validate`) is the authority and is a desktop step in RESUME.md; this catches
// the mistakes that break sideloading silently: an unresolved resid, a non-HTTPS URL, a missing key.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const root = join(__dirname, "../..");
const KEY = "abcdef0123456789abcdef";
const gen = (args: string[]) => execFileSync("node", [join(root, "scripts/gen-manifest.mjs"), ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
const xml = () => readFileSync(join(root, "manifest/manifest.xml"), "utf8");

describe("manifest", () => {
  gen(["--base", "https://localhost:3443", "--key", KEY, "--guid", "11111111-2222-3333-4444-555555555555"]);
  const m = xml();

  it("is well formed enough: one root, balanced tags, no unreplaced placeholders", () => {
    expect(m).not.toMatch(/\{\{[A-Z_]+\}\}/);
    const stack: string[] = [];
    for (const t of m.matchAll(/<(\/?)([A-Za-z][\w:.-]*)((?:"[^"]*"|[^>"])*?)(\/?)>/g)) {
      const [, closing, name, , selfClose] = t;
      if (selfClose) continue;
      if (!closing) stack.push(name);
      else expect(stack.pop(), `unexpected </${name}>`).toBe(name);
    }
    expect(stack).toEqual([]);
    expect(m.match(/<OfficeApp\b/g)).toHaveLength(1);
  });

  it("every resid refers to a defined resource", () => {
    const defined = new Set([...m.matchAll(/<bt:(?:Image|Url|String) id="([^"]+)"/g)].map((x) => x[1]));
    const used = [...m.matchAll(/resid="([^"]+)"/g)].map((x) => x[1]);
    expect(used.length).toBeGreaterThan(5);
    expect(used.filter((r) => !defined.has(r))).toEqual([]);
  });

  it("every URL is https (Office refuses http task panes) and the task pane carries the key", () => {
    const urls = [...m.matchAll(/(?:DefaultValue|<AppDomain>)="?(https?:\/\/[^"<]+)/g)].map((x) => x[1]);
    expect(urls.length).toBeGreaterThan(4);
    expect(urls.filter((u) => !u.startsWith("https://"))).toEqual([]);
    expect(m).toContain(`taskpane.html?key=${KEY}`);
  });

  it("targets Excel with ExcelApi >= 1.8 (PivotTables) and declares read/write", () => {
    expect(m).toMatch(/<Host Name="Workbook"\/>/);
    expect(m).toMatch(/<Set Name="ExcelApi" MinVersion="1.8"\/>/);
    expect(m).toContain("<Permissions>ReadWriteDocument</Permissions>");
  });

  it("refuses to generate with an http base or without a usable key", () => {
    expect(() => gen(["--base", "http://localhost:3443", "--key", KEY])).toThrow();
    expect(() => gen(["--base", "https://localhost:3443", "--key", "short"])).toThrow();
  });
});
