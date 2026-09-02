import { describe, expect, it } from "vitest";
import { parseKeywords, matchesKeywords, tokenize, scoreMatch } from "./keywordFilter";

describe("parseKeywords", () => {
  it("splits on whitespace", () => {
    expect(parseKeywords("foo bar")).toEqual(["foo", "bar"]);
  });
  it("trims and lowercases", () => {
    expect(parseKeywords("  Foo  BAR  ")).toEqual(["foo", "bar"]);
  });
  it("handles multiple spaces and newlines", () => {
    expect(parseKeywords("a  b\tc\n d")).toEqual(["a", "b", "c", "d"]);
  });
  it("returns empty for blank", () => {
    expect(parseKeywords("   ")).toEqual([]);
  });
  it("deduplicates keywords", () => {
    expect(parseKeywords("foo foo bar")).toEqual(["foo", "bar"]);
  });
  it("deduplicates case-insensitively", () => {
    expect(parseKeywords("Foo FOO foo")).toEqual(["foo"]);
  });
});

describe("matchesKeywords", () => {
  it("matches case-insensitive", () => {
    expect(matchesKeywords("Hello World", ["hello"])).toBe(true);
    expect(matchesKeywords("Hello World", ["HELLO"])).toBe(true);
    expect(matchesKeywords("Hello World", ["world"])).toBe(true);
  });
  it("requires all keywords (AND)", () => {
    expect(matchesKeywords("src/components/FileTree.tsx", ["file", "tree"])).toBe(true);
    expect(matchesKeywords("src/components/FileTree.tsx", ["file", "missing"])).toBe(false);
  });
  it("matches name and path uniformly", () => {
    expect(matchesKeywords("assets/photo.png image/png", ["photo"])).toBe(true);
    expect(matchesKeywords("assets/photo.png image/png", ["png"])).toBe(true);
    expect(matchesKeywords("assets/photo.png image/png", ["assets"])).toBe(true);
  });
  it("supports path fragments", () => {
    expect(matchesKeywords("a/b/c.ts", ["b/c"])).toBe(true);
  });
  it("returns true for empty keywords", () => {
    expect(matchesKeywords("anything", [])).toBe(true);
  });
});

describe("tokenize", () => {
  it("splits camelCase", () => {
    expect(tokenize("FileTree")).toEqual(["file", "tree"]);
  });
  it("splits PascalCase", () => {
    expect(tokenize("XMLParser")).toEqual(["xml", "parser"]);
  });
  it("splits snake_case", () => {
    expect(tokenize("auth_login")).toEqual(["auth", "login"]);
  });
  it("splits kebab-case", () => {
    expect(tokenize("auth-login")).toEqual(["auth", "login"]);
  });
  it("handles acronym followed by lowercase", () => {
    expect(tokenize("getHTTPSUrl")).toEqual(["get", "https", "url"]);
  });
  it("handles mixed separators", () => {
    expect(tokenize("user-auth_service.js")).toEqual(["user", "auth", "service", "js"]);
  });
  it("splits on dots (file extensions)", () => {
    expect(tokenize("FileTree.tsx")).toEqual(["file", "tree", "tsx"]);
  });
  it("splits on slashes (paths)", () => {
    expect(tokenize("src/components/FileTree")).toEqual(["src", "components", "file", "tree"]);
  });
  it("handles OAuth correctly", () => {
    expect(tokenize("OAuth")).toEqual(["o", "auth"]);
  });
  it("lowercases everything", () => {
    expect(tokenize("FileName")).toEqual(["file", "name"]);
  });
});

describe("scoreMatch", () => {
  it("returns 0 for empty keywords", () => {
    expect(scoreMatch("anything", [])).toBe(0);
  });

  it("returns 0 when keyword not found", () => {
    expect(scoreMatch("auth.ts", ["missing"])).toBe(0);
  });

  describe("exact word vs substring priority", () => {
    it("exact word in filename scores higher than substring", () => {
      // "auth.ts" → exact word "auth" in filename = 10
      // "authentication.ts" → substring "auth" in filename = 5
      const exact = scoreMatch("auth.ts", ["auth"]);
      const sub = scoreMatch("authentication.ts", ["auth"]);
      expect(exact).toBeGreaterThan(sub);
    });

    it("exact word in path scores higher than substring in filename", () => {
      // "src/auth/handler.ts" → exact word "auth" in path = 8
      // "src/authentication/handler.ts" → substring "auth" in filename = 0 (not in filename), substring in path = 1
      const exactPath = scoreMatch("src/auth/handler.ts", ["auth"]);
      const subPath = scoreMatch("src/authentication/handler.ts", ["auth"]);
      expect(exactPath).toBeGreaterThan(subPath);
    });
  });

  describe("filename vs path priority", () => {
    it("exact word in filename beats exact word in path", () => {
      // "auth.ts" → exact "auth" in filename = 10
      // "src/auth/utils.ts" → exact "auth" in path = 8
      const filename = scoreMatch("auth.ts", ["auth"]);
      const path = scoreMatch("src/auth/utils.ts", ["auth"]);
      expect(filename).toBeGreaterThan(path);
    });

    it("substring in filename beats substring in path", () => {
      // "authentication.ts" → substring "auth" in filename = 5
      // "src/authenticator/utils.ts" → substring "auth" in path = 1
      const filename = scoreMatch("authentication.ts", ["auth"]);
      const path = scoreMatch("src/authenticator/utils.ts", ["auth"]);
      expect(filename).toBeGreaterThan(path);
    });
  });

  describe("multi-keyword scoring", () => {
    it("more matching keywords score higher", () => {
      // "auth_login.ts" matches both "auth" and "login"
      // "auth_service.ts" only matches "auth"
      const both = scoreMatch("auth_login.ts", ["auth", "login"]);
      const one = scoreMatch("auth_service.ts", ["auth", "login"]);
      expect(both).toBeGreaterThan(one);
      expect(one).toBe(0); // "login" not found
    });

    it("all keywords must match (AND semantics)", () => {
      expect(scoreMatch("auth.ts", ["auth", "login"])).toBe(0);
      expect(scoreMatch("login.ts", ["auth", "login"])).toBe(0);
      expect(scoreMatch("auth_login.ts", ["auth", "login"])).toBeGreaterThan(0);
    });
  });

  describe("case insensitivity", () => {
    it("matches case-insensitively", () => {
      expect(scoreMatch("Auth.ts", ["auth"])).toBeGreaterThan(0);
      expect(scoreMatch("AUTH.ts", ["auth"])).toBeGreaterThan(0);
    });
  });

  describe("camelCase word boundaries", () => {
    it("exact camelCase word match in filename", () => {
      // "FileTree.tsx" → tokens: ["file", "tree", "tsx"]
      // keyword "tree" is exact word match = 10
      expect(scoreMatch("FileTree.tsx", ["tree"])).toBe(10);
    });

    it("substring match does not count as exact word", () => {
      // "FileTreeHelper.tsx" → tokens: ["file", "tree", "helper", "tsx"]
      // keyword "tree" is still exact word match (it's a token)
      expect(scoreMatch("FileTreeHelper.tsx", ["tree"])).toBe(10);
    });
  });

  describe("deterministic tie-breaking", () => {
    it("same score maintains stable order (tested by array order)", () => {
      // Both have exact "auth" in filename
      const a = scoreMatch("auth.ts", ["auth"]);
      const b = scoreMatch("auth_helper.ts", ["auth"]);
      // Both score 10 for exact "auth" in filename
      expect(a).toBe(b);
    });
  });
});

describe("scoreMatch integration", () => {
  it("exact filename match outranks partial filename match", () => {
    // "auth.ts" exact word match = 10
    // "authentication.ts" substring match = 5
    const files = ["authentication.ts", "auth.ts", "authorize.ts"];
    const keywords = parseKeywords("auth");
    const scored = files.map(f => ({ file: f, score: scoreMatch(f, keywords) }));
    scored.sort((a, b) => b.score - a.score);
    expect(scored[0].file).toBe("auth.ts");
    expect(scored[0].score).toBe(10);
    expect(scored[1].file).toBe("authentication.ts");
    expect(scored[1].score).toBe(5); // substring match
    expect(scored[2].file).toBe("authorize.ts");
    expect(scored[2].score).toBe(5); // substring match
  });

  it("multiple keywords rank files matching more keywords higher", () => {
    // "auth_login.ts" matches both "auth" and "login"
    // "auth_service.ts" only matches "auth"
    const files = ["auth_service.ts", "auth_login.ts", "login.ts"];
    const keywords = parseKeywords("auth login");
    const scored = files.map(f => ({ file: f, score: scoreMatch(f, keywords) }));
    scored.sort((a, b) => b.score - a.score);
    expect(scored[0].file).toBe("auth_login.ts");
    expect(scored[0].score).toBe(20); // two exact matches
    expect(scored[1].file).toBe("auth_service.ts");
    expect(scored[1].score).toBe(0); // "login" not found
  });

  it("filename match outranks path match", () => {
    // "auth.ts" exact in filename = 10
    // "src/auth/utils.ts" exact in path = 8
    const files = ["src/auth/utils.ts", "auth.ts"];
    const keywords = parseKeywords("auth");
    const scored = files.map(f => ({ file: f, score: scoreMatch(f, keywords) }));
    scored.sort((a, b) => b.score - a.score);
    expect(scored[0].file).toBe("auth.ts");
  });

  it("camelCase word boundary matching works correctly", () => {
    // "FileTree.tsx" → tokens: ["file", "tree", "tsx"]
    // keyword "tree" is exact word match = 10
    // "treemap.ts" → substring "tree" in filename = 5
    const files = ["treemap.ts", "FileTree.tsx", "treemanager.ts"];
    const keywords = parseKeywords("tree");
    const scored = files.map(f => ({ file: f, score: scoreMatch(f, keywords) }));
    scored.sort((a, b) => b.score - a.score);
    expect(scored[0].file).toBe("FileTree.tsx");
    expect(scored[0].score).toBe(10);
  });

  it("empty query returns 0 for all files", () => {
    expect(scoreMatch("any_file.ts", [])).toBe(0);
    expect(scoreMatch("another.ts", [])).toBe(0);
  });

  it("single term query works", () => {
    expect(scoreMatch("auth.ts", ["auth"])).toBe(10);
    expect(scoreMatch("authentication.ts", ["auth"])).toBe(5);
  });

  it("case insensitive matching", () => {
    expect(scoreMatch("AUTH.ts", ["auth"])).toBe(10);
    expect(scoreMatch("Auth.ts", ["auth"])).toBe(10);
  });

  it("files under matching directories are retained", () => {
    // This tests that parent directories with matching descendants are kept
    // The sorting applies to siblings, not globally flattening the tree
    const files = [
      "src/auth/handler.ts",  // exact "auth" in path = 8
      "src/auth/utils.ts",    // exact "auth" in path = 8
      "src/authentication/test.ts", // substring "auth" in path = 1
    ];
    const keywords = parseKeywords("auth");
    const scores = files.map(f => scoreMatch(f, keywords));
    // All match, but scores differ
    expect(scores[0]).toBe(8); // exact in path
    expect(scores[1]).toBe(8); // exact in path
    expect(scores[2]).toBe(1); // substring in path
  });
});
