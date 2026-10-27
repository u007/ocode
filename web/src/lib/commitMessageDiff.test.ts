import { describe, expect, it } from "vitest";
import type { GitDiffFile } from "@/api/types";
import {
  buildCommitMessageDiff,
  COMMIT_MESSAGE_DIFF_LIMIT,
  truncateCommitMessageDiff,
} from "./commitMessageDiff";

const utf8Encoder = new TextEncoder();

function file(path: string, patch: string, status = "modified"): GitDiffFile {
  return { path, status, patch };
}

describe("buildCommitMessageDiff", () => {
  it("prefers the staged patches when anything is staged", () => {
    const diff = buildCommitMessageDiff(
      [file("src/staged.ts", "diff --git a/src/staged.ts b/src/staged.ts\n+staged")],
      [file("src/unstaged.ts", "diff --git a/src/unstaged.ts b/src/unstaged.ts\n+unstaged")],
    );
    expect(diff).toContain("src/staged.ts");
    expect(diff).not.toContain("src/unstaged.ts");
  });

  it("falls back to the working tree when nothing is staged", () => {
    const diff = buildCommitMessageDiff(
      [],
      [
        file("src/unstaged.ts", "diff --git a/src/unstaged.ts b/src/unstaged.ts\n+unstaged"),
        file("src/untracked.txt", "diff --git a/src/untracked.txt b/src/untracked.txt\n+new", "untracked"),
      ],
    );
    expect(diff).toContain("src/unstaged.ts");
    expect(diff).toContain("src/untracked.txt");
  });

  it("keeps a header for a file without a patch (binary or rename)", () => {
    const diff = buildCommitMessageDiff([], [file("assets/logo.png", "", "modified")]);
    expect(diff).toBe("diff --git a/assets/logo.png b/assets/logo.png (modified)");
  });

  it("returns an empty string when there are no files at all", () => {
    expect(buildCommitMessageDiff([], [])).toBe("");
  });

  it("caps the diff at the server's limit", () => {
    const diff = buildCommitMessageDiff(
      [file("src/big.ts", "x".repeat(COMMIT_MESSAGE_DIFF_LIMIT + 500))],
      [],
    );
    expect(diff.length).toBe(COMMIT_MESSAGE_DIFF_LIMIT);
  });
});

describe("truncateCommitMessageDiff", () => {
  it("leaves short text untouched", () => {
    expect(truncateCommitMessageDiff("short", 10)).toBe("short");
  });

  it("counts the limit in UTF-8 bytes, not UTF-16 units", () => {
    // "é" is 2 UTF-8 bytes; "éé" is 4. A 3-byte limit keeps one whole "é".
    expect(truncateCommitMessageDiff("éé", 3)).toBe("é");
    // "😀" is 4 UTF-8 bytes (2 UTF-16 units). Bytes 2-5 of "ab😀cd" are the emoji.
    expect(truncateCommitMessageDiff("ab😀cd", 4)).toBe("ab");
    expect(truncateCommitMessageDiff("ab😀cd", 5)).toBe("ab");
    expect(truncateCommitMessageDiff("ab😀cd", 6)).toBe("ab😀");
  });

  it("never returns more than the byte limit", () => {
    const text = "日本語のテキスト😀".repeat(50);
    for (const limit of [1, 2, 3, 7, 100, 999]) {
      const out = truncateCommitMessageDiff(text, limit);
      expect(utf8Encoder.encode(out).length).toBeLessThanOrEqual(limit);
      expect(text.startsWith(out)).toBe(true);
    }
  });

  it("caps multi-byte diffs at the server's byte limit", () => {
    const diff = buildCommitMessageDiff(
      [file("src/unicode.ts", "é".repeat(COMMIT_MESSAGE_DIFF_LIMIT))],
      [],
    );
    expect(utf8Encoder.encode(diff).length).toBeLessThanOrEqual(COMMIT_MESSAGE_DIFF_LIMIT);
    expect(diff).not.toContain("\uFFFD");
  });
});
