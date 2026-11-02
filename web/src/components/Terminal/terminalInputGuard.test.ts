import { describe, expect, it } from "vitest";
import { createTerminalInputGuard, IME_DUPLICATE_WINDOW_MS, isTerminalGeneratedReply } from "./terminalInputGuard";

/**
 * Regression: a phone soft keyboard commits one word twice.
 *
 * Measured on the real terminal (Playwright + CDP `Input.imeSetComposition` /
 * `Input.insertText`, 2026-11-02): the SAME event sequence sends `ls` once when
 * the re-commit lands 60 ms after the first, and TWICE when it lands 250 ms
 * later. ghostty-web suppresses a duplicate `insertText` only inside its
 * `BEFORE_INPUT_IGNORE_MS` (100 ms) window, so on a slow phone the word reaches
 * the pty twice and the shell runs it twice.
 *
 * These tests pin the ocode-side guard that makes the dedupe content-based
 * instead of clock-based.
 */
describe("createTerminalInputGuard", () => {
  it("accepts the first composition commit", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("ls");
    expect(guard.accept("ls", 1010)).toBe(true);
  });

  it("suppresses the same commit arriving again inside the window", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("ls");
    expect(guard.accept("ls", 1010)).toBe(true);
    // The slow-phone re-commit: past ghostty-web's 100 ms window.
    expect(guard.accept("ls", 1250)).toBe(false);
  });

  it("suppresses a third and fourth re-commit measured from the first send", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("ls");
    expect(guard.accept("ls", 0)).toBe(true);
    expect(guard.accept("ls", 300)).toBe(false);
    expect(guard.accept("ls", 600)).toBe(false);
  });

  it("accepts the same word again once the window has elapsed", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("ls");
    expect(guard.accept("ls", 0)).toBe(true);
    expect(guard.accept("ls", IME_DUPLICATE_WINDOW_MS + 1)).toBe(true);
  });

  it("accepts a different word typed after an identical one", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("ls");
    expect(guard.accept("ls", 0)).toBe(true);
    guard.noteComposition("la");
    expect(guard.accept("la", 110)).toBe(true);
  });

  it("clears the pending commit when a keystroke frame arrives between commits", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("ls");
    expect(guard.accept("ls", 0)).toBe(true);
    // Enter (or any other typed frame) ends that commit: the next identical
    // word is a NEW word and must not be swallowed.
    expect(guard.accept("\r", 100)).toBe(true);
    expect(guard.accept("ls", 400)).toBe(true);
  });

  it("keeps a repeated CJK word typed a moment after the first commit", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("哈哈");
    expect(guard.accept("哈哈", 0)).toBe(true);
    // A deliberate second 哈哈 needs a fresh pinyin entry and candidate pick:
    // well past a phone re-delivery (250 ms), so it must not be swallowed.
    guard.noteComposition("哈哈");
    expect(guard.accept("哈哈", 800)).toBe(true);
  });

  it("treats a non-composition input between two identical commits as a new word", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("ok");
    expect(guard.accept("ok", 0)).toBe(true);
    // Space (or a keystroke/paste) went out, then the same word is composed again.
    guard.noteInput();
    guard.noteComposition("ok");
    expect(guard.accept("ok", 300)).toBe(true);
  });

  it("still drops a re-delivery when no non-composition input came between", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("ok");
    expect(guard.accept("ok", 0)).toBe(true);
    guard.noteComposition("ok");
    expect(guard.accept("ok", 250)).toBe(false);
  });

  it("never suppresses a payload with no recorded composition (typed keys, pastes)", () => {
    const guard = createTerminalInputGuard();
    expect(guard.accept("ls", 0)).toBe(true);
    expect(guard.accept("ls", 100)).toBe(true);
  });

  it("ignores an empty composition commit", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("");
    expect(guard.accept("ls", 10)).toBe(true);
    expect(guard.accept("ls", 20)).toBe(true);
  });

  it("never suppresses control-character frames (bracketed paste, arrows)", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("\x1b[200~ls\x1b[201~");
    expect(guard.accept("\x1b[200~ls\x1b[201~", 10)).toBe(true);
    expect(guard.accept("\x1b[200~ls\x1b[201~", 20)).toBe(true);
  });

  it("never suppresses a single-character commit (deliberate repeats are legitimate)", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("哈");
    expect(guard.accept("哈", 10)).toBe(true);
    expect(guard.accept("哈", 20)).toBe(true);
  });

  it("accepts empty data", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("ls");
    expect(guard.accept("ls", 0)).toBe(true);
    expect(guard.accept("", 10)).toBe(true);
  });

  it("forgets everything on reset (new socket / new terminal)", () => {
    const guard = createTerminalInputGuard();
    guard.noteComposition("ls");
    expect(guard.accept("ls", 0)).toBe(true);
    guard.reset();
    expect(guard.accept("ls", 10)).toBe(true);
  });
});

/**
 * The panel raises "input is paused" for keystrokes sent while detached. The
 * terminal also writes its own replies through onData (focus reports, DA and
 * CPR answers, mouse reports); those are not typing and must not raise it.
 */
describe("isTerminalGeneratedReply", () => {
  it.each([
    ["focus-in report", "\x1b[I"],
    ["focus-out report", "\x1b[O"],
    ["primary device-attribute reply", "\x1b[?62;22c"],
    ["secondary device-attribute reply", "\x1b[>0;95;0c"],
    ["cursor-position report", "\x1b[12;40R"],
    ["private cursor-position report", "\x1b[?12;40R"],
    ["SGR mouse press", "\x1b[<0;10;5M"],
    ["SGR mouse release", "\x1b[<0;10;5m"],
    ["X10 mouse report", "\x1b[M #!"],
    ["two focus reports in one frame", "\x1b[I\x1b[O"],
    ["a focus report followed by a mouse report", "\x1b[I\x1b[<0;3;4M"],
  ])("recognises a %s", (_name, data) => {
    expect(isTerminalGeneratedReply(data)).toBe(true);
  });

  it.each([
    ["a letter", "l"],
    ["a word", "ls"],
    ["Enter", "\r"],
    ["an up arrow", "\x1b[A"],
    ["a function key", "\x1b[15~"],
    ["Shift+Enter", "\x1b[13;2u"],
    ["a bracketed paste", "\x1b[200~ls\x1b[201~"],
    ["a reply with typed text after it", "\x1b[I;"],
    ["a focus report with a keystroke after it", "\x1b[Ix"],
    ["an empty frame", ""],
  ])("does not treat %s as a terminal reply", (_name, data) => {
    expect(isTerminalGeneratedReply(data)).toBe(false);
  });
});
