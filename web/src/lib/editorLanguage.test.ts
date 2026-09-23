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
    expect(languageForFile("schema.sql")).toBe("sql");
    expect(languageForFile("Main.java")).toBe("java");
    expect(languageForFile("Program.cs")).toBe("csharp");
    expect(languageForFile("app.ex")).toBe("elixir");
    expect(languageForFile("query.proto")).toBe("protobuf");
    expect(languageForFile("config.ini")).toBe("ini");
    expect(languageForFile("deploy.ps1")).toBe("powershell");
    expect(languageForFile("run.bat")).toBe("bat");
    // Terraform has no Monaco grammar of its own; the bundled HCL grammar is
    // the correct id (a bare "terraform" id is unregistered and falls back).
    expect(languageForFile("infra.tf")).toBe("hcl");
    expect(languageForFile("infra.tfvars")).toBe("hcl");
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
