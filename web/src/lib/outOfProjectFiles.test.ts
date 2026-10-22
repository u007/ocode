import { describe, expect, it } from "vitest";
import {
  isOutsideRootError,
  looksLikeAbsolutePath,
  normalizeUserPath,
} from "./outOfProjectFiles";

describe("outOfProjectFiles", () => {
  describe("isOutsideRootError", () => {
    it("recognises the server's containment refusals", () => {
      // The read endpoint.
      expect(isOutsideRootError("path is outside the project root")).toBe(true);
      // The save endpoint, without an anchor.
      expect(isOutsideRootError("path is outside the workspace")).toBe(true);
      // The OS-open endpoint, without an anchor.
      expect(isOutsideRootError("path is outside the working directory")).toBe(true);
      // A project_root the server does not accept.
      expect(isOutsideRootError("project_root is not an allowed project root")).toBe(true);
    });

    it("does NOT recognise unrelated failures", () => {
      // A missing file is NOT grantable — offering a grant for it would be
      // misleading (granting the directory would not create the file).
      expect(isOutsideRootError("file not found")).toBe(false);
      expect(isOutsideRootError("Failed to load file (404)")).toBe(false);
      expect(isOutsideRootError("Unauthorized")).toBe(false);
      expect(isOutsideRootError("")).toBe(false);
      expect(isOutsideRootError(undefined)).toBe(false);
      expect(isOutsideRootError(null)).toBe(false);
    });

    it("is case-insensitive", () => {
      expect(isOutsideRootError("PATH IS OUTSIDE THE PROJECT ROOT")).toBe(true);
    });
  });

  describe("looksLikeAbsolutePath", () => {
    it("accepts POSIX absolute paths", () => {
      expect(looksLikeAbsolutePath("/etc/hosts")).toBe(true);
      expect(looksLikeAbsolutePath("/Users/x/Downloads/notes.md")).toBe(true);
    });

    it("accepts home-relative paths", () => {
      expect(looksLikeAbsolutePath("~/notes.md")).toBe(true);
      expect(looksLikeAbsolutePath("  ~/notes.md  ")).toBe(true);
    });

    it("accepts Windows drive and UNC paths", () => {
      expect(looksLikeAbsolutePath("C:\\Users\\x\\notes.md")).toBe(true);
      expect(looksLikeAbsolutePath("c:/Users/x/notes.md")).toBe(true);
    });

    it("rejects relative paths and blanks", () => {
      expect(looksLikeAbsolutePath("notes.md")).toBe(false);
      expect(looksLikeAbsolutePath("src/notes.md")).toBe(false);
      expect(looksLikeAbsolutePath("./notes.md")).toBe(false);
      expect(looksLikeAbsolutePath("../notes.md")).toBe(false);
      expect(looksLikeAbsolutePath("")).toBe(false);
      expect(looksLikeAbsolutePath("   ")).toBe(false);
    });
  });

  describe("normalizeUserPath", () => {
    it("trims surrounding whitespace", () => {
      expect(normalizeUserPath("  /etc/hosts  ")).toBe("/etc/hosts");
    });
  });
});
