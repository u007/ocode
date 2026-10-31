export type Choice = "ours" | "theirs" | "both";

/** One conflict region of the live buffer, 1-based inclusive line numbers.
 *  `base` is the diff3 `|||||||` line, 0 when absent. */
export interface Block {
  start: number;
  base: number;
  sep: number;
  end: number;
}

/** Scans the buffer (not the server's chunk list) so blocks stay correct while
 *  the user hand-edits, and so a resolved block disappears with no round trip. */
export function parseBlocks(text: string): Block[] {
  const lines = text.split("\n");
  const blocks: Block[] = [];
  let start = 0;
  let base = 0;
  let sep = 0;
  lines.forEach((line, i) => {
    const n = i + 1;
    if (line.startsWith("<<<<<<<")) {
      start = n;
      base = 0;
      sep = 0;
    } else if (start && !sep && line.startsWith("|||||||")) {
      base = n;
    } else if (start && !sep && line === "=======") {
      sep = n;
    } else if (start && sep && line.startsWith(">>>>>>>")) {
      blocks.push({ start, base, sep, end: n });
      start = 0;
    }
  });
  return blocks;
}

/** The text that replaces a block's marker-to-marker range for a choice. */
export function resolveBlockText(lines: string[], b: Block, choice: Choice): string[] {
  const oursEnd = (b.base || b.sep) - 1;
  const ours = lines.slice(b.start, oursEnd);
  const theirs = lines.slice(b.sep, b.end - 1);
  if (choice === "ours") return ours;
  if (choice === "theirs") return theirs;
  return [...ours, ...theirs];
}
