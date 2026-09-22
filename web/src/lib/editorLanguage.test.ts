import { describe, expect, it } from "vitest";
import { languageForFile } from "./editorLanguage";

describe("languageForFile", () => {
  it("maps the Markdown family, including MDX", () => {
    expect(languageForFile("README.md")).toBe("markdown");
    expect(languageForFile("docs/guide.markdown")).toBe("markdown");
    // MDX gets Monaco's JSX-aware `mdx` grammar, not plain `markdown`.
    expect(languageForFile("docs/page.mdx")).toBe("mdx");
    expect(languageForFile("PAGE.MDX")).toBe("mdx");
  });

  it("maps common code languages", () => {
    expect(languageForFile("src/app.tsx")).toBe("typescript");
    expect(languageForFile("main.go")).toBe("go");
    expect(languageForFile("styles.scss")).toBe("scss");
    // A file literally named Dockerfile has no extension: `split(".")` yields
    // the whole name, which the map happens to recognize.
    expect(languageForFile("Dockerfile")).toBe("dockerfile");
  });

  it("falls back to plaintext for unknown or extensionless paths", () => {
    expect(languageForFile("Makefile")).toBe("plaintext");
    expect(languageForFile("data.unknownext")).toBe("plaintext");
    expect(languageForFile("")).toBe("plaintext");
  });
});
