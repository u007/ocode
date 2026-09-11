import { describe, expect, it } from "vitest";
import PreviewSurface from "./PreviewSurface";

const kinds = ["pdf", "docx", "pptx", "excel", "mermaid", "markdown", "text", "image"] as const;

describe("PreviewSurface renderer kinds", () => {
  for (const kind of kinds) {
    it(`accepts kind=${kind}`, () => {
      expect(() => PreviewSurface({ path: "/test", kind })).not.toThrow();
    });
  }
});
