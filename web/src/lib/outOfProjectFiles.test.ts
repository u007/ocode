import { describe, expect, it } from "vitest";
import {
  isOutsideRootError,
  anchorForGrant,
  looksLikeAbsolutePath,
  normalizeUserPath,
  relativeToRoot,
  resolveAgainstRoot,
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

describe("relativeToRoot", () => {
  it("returns the path relative to the root", () => {
    expect(relativeToRoot("/Users/x/Downloads/notes.md", "/Users/x/Downloads")).toBe("notes.md");
    expect(relativeToRoot("/Users/x/Downloads/sub/a.md", "/Users/x/Downloads")).toBe("sub/a.md");
  });

  it("ignores a trailing separator on the root", () => {
    expect(relativeToRoot("/Users/x/Downloads/notes.md", "/Users/x/Downloads/")).toBe("notes.md");
  });

  it("requires a SEPARATOR boundary (a plain prefix is not containment)", () => {
    // /a/bc/d is NOT inside /a/b — slicing root.length would wrongly yield "c/d".
    expect(relativeToRoot("/a/bc/d", "/a/b")).toBeNull();
  });

  it("returns '.' for the root itself", () => {
    expect(relativeToRoot("/a/b", "/a/b")).toBe(".");
  });

  it("returns null for a path outside the root", () => {
    expect(relativeToRoot("/etc/hosts", "/Users/x/Downloads")).toBeNull();
  });

  it("handles Windows separators", () => {
    expect(relativeToRoot("C:\\Users\\x\\notes.md", "C:\\Users\\x")).toBe("notes.md");
  });

  it("handles mixed separators on a Windows root", () => {
    expect(relativeToRoot("C:/Users\\x/notes.md", "C:\\Users\\x")).toBe("notes.md");
    expect(relativeToRoot("C:\\Users/x\\sub/a.md", "C:/Users/x")).toBe("sub/a.md");
  });

  it("matches Windows drive paths case-insensitively", () => {
    expect(relativeToRoot("c:\\users\\X\\notes.md", "C:\\Users\\x")).toBe("notes.md");
    expect(relativeToRoot("C:\\Users\\x\\notes.md", "c:\\users\\X")).toBe("notes.md");
    expect(relativeToRoot("D:\\Users\\x\\notes.md", "C:\\Users\\x")).toBeNull();
  });

  it("keeps POSIX roots case-sensitive", () => {
    expect(relativeToRoot("/Users/x/notes.md", "/users/x")).toBeNull();
    expect(relativeToRoot("/users/x/notes.md", "/users/x")).toBe("notes.md");
  });

  it("does not treat a backslash in a POSIX path as a separator", () => {
    expect(relativeToRoot("/a/b\\c", "/a")).toBe("b\\c");
    expect(relativeToRoot("/a\\b/c", "/a")).toBeNull();
  });

  it("resolves '..' segments before the containment check", () => {
    expect(relativeToRoot("/a/b/../b/c.md", "/a/b")).toBe("c.md");
    expect(relativeToRoot("/a/b/./c.md", "/a/b")).toBe("c.md");
    expect(relativeToRoot("/a/b/sub/../c.md", "/a/b")).toBe("c.md");
  });

  it("returns null for a path that escapes the root with '..'", () => {
    expect(relativeToRoot("/a/b/../../etc/hosts", "/a/b")).toBeNull();
    expect(relativeToRoot("/a/b/../c/x.md", "/a/b")).toBeNull();
    expect(relativeToRoot("C:\\Users\\x\\..\\..\\Windows\\win.ini", "C:\\Users\\x")).toBeNull();
  });

  it("resolves '..' in the root as well", () => {
    expect(relativeToRoot("/a/b/c.md", "/a/x/../b")).toBe("c.md");
  });

  it("treats a relative path or root as not containable", () => {
    expect(relativeToRoot("a/b.md", "/a")).toBeNull();
    expect(relativeToRoot("/a/b.md", "a")).toBeNull();
  });

  it("uses a POSIX filesystem root '/' as the root, not as an empty string", () => {
    expect(relativeToRoot("/etc/hosts", "/")).toBe("etc/hosts");
    expect(relativeToRoot("/", "/")).toBe(".");
    expect(relativeToRoot("/etc/hosts", "//")).toBe("etc/hosts");
  });

  it("uses a Windows drive root 'C:\\' as the root, keeping the remainder relative", () => {
    expect(relativeToRoot("C:\\Users\\x\\notes.md", "C:\\")).toBe("Users/x/notes.md");
    expect(relativeToRoot("C:/Users/x/notes.md", "C:/")).toBe("Users/x/notes.md");
    expect(relativeToRoot("c:\\users\\a.md", "C:\\")).toBe("users/a.md");
    expect(relativeToRoot("C:\\", "C:\\")).toBe(".");
    expect(relativeToRoot("D:\\a.md", "C:\\")).toBeNull();
  });

  it("returns null for empty inputs", () => {
    expect(relativeToRoot("", "/a")).toBeNull();
    expect(relativeToRoot("/a/b", "")).toBeNull();
  });
});

describe("anchorForGrant", () => {
  it("anchors a granted file to its granted root and relative path", () => {
    expect(anchorForGrant({ root: "/Users/x/Downloads", path: "/Users/x/Downloads/a/notes.md" })).toEqual({
      projectRoot: "/Users/x/Downloads",
      path: "a/notes.md",
    });
  });

  it("returns null when the file is not inside the granted root", () => {
    expect(anchorForGrant({ root: "/a/b", path: "/a/bc/d" })).toBeNull();
  });

  it("returns null for a relative path (the caller decides)", () => {
    expect(anchorForGrant({ root: "/a/b", path: "c/d" })).toBeNull();
  });
});

describe("resolveAgainstRoot", () => {
  it("resolves a parent-relative path against the root, lexically", () => {
    expect(resolveAgainstRoot("/Users/x/proj", "../x/a.png")).toBe("/Users/x/x/a.png");
  });

  it("resolves a plain relative path under the root", () => {
    expect(resolveAgainstRoot("/Users/x/proj", "sub/a.png")).toBe("/Users/x/proj/sub/a.png");
  });

  it("returns an absolute path unchanged", () => {
    expect(resolveAgainstRoot("/Users/x/proj", "/elsewhere/a.png")).toBe("/elsewhere/a.png");
  });

  it("returns a home-relative path unchanged", () => {
    expect(resolveAgainstRoot("/Users/x/proj", "~/a.png")).toBe("~/a.png");
  });

  it("returns null when there is no root to anchor to", () => {
    expect(resolveAgainstRoot("", "a.png")).toBeNull();
  });

  it("returns null when the root is not absolute or is an unexpanded home path", () => {
    expect(resolveAgainstRoot("proj", "a.png")).toBeNull();
    expect(resolveAgainstRoot("~/proj", "a.png")).toBeNull();
  });

  it("clamps a parent walk at the filesystem root, as the OS does", () => {
    expect(resolveAgainstRoot("/a", "../../b.png")).toBe("/b.png");
  });

  it("resolves a relative path under a Windows drive root", () => {
    expect(resolveAgainstRoot("C:\\Users\\x\\proj", "..\\Downloads\\pic.png")).toBe("C:/Users/x/Downloads/pic.png");
  });
});
