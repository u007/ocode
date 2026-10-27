import { describe, expect, it } from "vitest";
import type { GitDiffFile } from "@/api/types";
import {
  buildCommitMessageDiff,
  COMMIT_MESSAGE_DIFF_LIMIT,
  truncateCommitMessageDiff,
} from "./commitMessageDiff";

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

  it("does not split a surrogate pair", () => {
    // "ab😀" is a,b,high-surrogate,low-surrogate: a limit of 3 lands between
    // the two halves, so the pair is dropped whole.
    expect(truncateCommitMessageDiff("ab😀cd", 3)).toBe("ab");
    // A limit of 4 keeps the whole pair.
    expect(truncateCommitMessageDiff("ab😀cd", 4)).toBe("ab😀");
  });
});
