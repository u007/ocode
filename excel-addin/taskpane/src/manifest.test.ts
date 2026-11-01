// Structural checks on the generated sideload manifest. Microsoft's own validator
// (`npx office-addin-manifest validate`) is the authority and is a desktop step in RESUME.md; this catches
// the mistakes that break sideloading silently: an unresolved resid, a non-HTTPS URL, a missing key.
// Every generation here writes into a temp directory: the real manifest/manifest.xml embeds the pane key and
// is written only by running the script by hand.
import { execFileSync } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

const root = join(__dirname, "../..");
const script = join(root, "scripts/gen-manifest.mjs");
const realManifest = join(root, "manifest/manifest.xml");
const realMtimeBefore = existsSync(realManifest) ? statSync(realManifest).mtimeMs : null;
const KEY = "abcdef0123456789abcdef";
const GUID = "11111111-2222-3333-4444-555555555555";
const flags = ["--base", "https://localhost:3443", "--key", KEY, "--guid", GUID];

const run = (args: string[]) => execFileSync("node", [script, ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
let dir = "";
const gen = (args: string[]) => run(["--out", join(dir, "manifest.xml"), ...args]);

describe("manifest", () => {
  let m = "";
  beforeAll(() => {
    dir = mkdtempSync(join(tmpdir(), "manifest-test-"));
    gen(flags);
    m = readFileSync(join(dir, "manifest.xml"), "utf8");
  });
  afterAll(() => { rmSync(dir, { recursive: true }); });

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

  it("the ribbon button names the auto-open pane, so a tagged workbook opens the pane with it", () => {
    expect(m).toContain("<TaskpaneId>Office.AutoShowTaskpaneWithDocument</TaskpaneId>");
    expect(m.match(/<TaskpaneId>/g)).toHaveLength(1);
    expect(m).toMatch(/<Action xsi:type="ShowTaskpane">\s*<!--[^>]*-->\s*<TaskpaneId>/);
  });

  it("refuses to generate with an http base or without a usable key", () => {
    expect(() => gen(["--base", "http://localhost:3443", "--key", KEY])).toThrow();
    expect(() => gen(["--base", "https://localhost:3443", "--key", "short"])).toThrow();
  });
});

describe("gen-manifest output path", () => {
  beforeAll(() => { dir = mkdtempSync(join(tmpdir(), "manifest-test-")); });
  afterAll(() => { rmSync(dir, { recursive: true }); });

  it("--out writes exactly that file, prints its path, and never touches the real manifest", () => {
    const target = join(dir, "elsewhere.xml");
    const stdout = run(["--out", target, ...flags]);
    expect(stdout).toContain(target);
    expect(readFileSync(target, "utf8")).toContain(`taskpane.html?key=${KEY}`);
    expect(existsSync(realManifest) ? statSync(realManifest).mtimeMs : null).toBe(realMtimeBefore);
  });

  it("with no --out it writes manifest/manifest.xml beside its own template (checked in a copy of the tree)", () => {
    const tree = join(dir, "tree");
    mkdirSync(join(tree, "scripts"), { recursive: true });
    mkdirSync(join(tree, "manifest"));
    copyFileSync(script, join(tree, "scripts/gen-manifest.mjs"));
    copyFileSync(join(root, "manifest/manifest.template.xml"), join(tree, "manifest/manifest.template.xml"));
    execFileSync("node", [join(tree, "scripts/gen-manifest.mjs"), ...flags], { stdio: ["ignore", "pipe", "pipe"] });
    expect(readFileSync(join(tree, "manifest/manifest.xml"), "utf8")).toContain(`taskpane.html?key=${KEY}`);
  });

  it("--out without a path is refused", () => {
    expect(() => run([...flags, "--out"])).toThrow();
  });
});
