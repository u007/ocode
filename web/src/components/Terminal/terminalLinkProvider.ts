import { buildPathRegex, splitPathToken, OPEN_FILE_EVENT, type OpenFileDetail } from "../../lib/fileLinks";
import type { Terminal, IBufferLine } from "@xterm/xterm";

// Exact copy of xterm WebLinkProvider's helpers (MIT) — see
// https://github.com/xtermjs/xterm.js/blob/master/addons/addon-web-links/src/WebLinkProvider.ts
// Reused for file-path links so wrapping and wide-char mapping stay correct.

function getWindowedLineStrings(lineIndex: number, terminal: Terminal): [string[], number] {
  let line: IBufferLine | undefined;
  let topIdx = lineIndex;
  let bottomIdx = lineIndex;
  let length = 0;
  let content = "";
  const lines: string[] = [];

  if ((line = terminal.buffer.active.getLine(lineIndex))) {
    const currentContent = line.translateToString(true);

    // expand top, stop on whitespaces or length > 2048
    if (line.isWrapped && currentContent[0] !== " ") {
      length = 0;
      while ((line = terminal.buffer.active.getLine(--topIdx)) && length < 2048) {
        content = line.translateToString(true);
        length += content.length;
        lines.push(content);
        if (!line.isWrapped || content.indexOf(" ") !== -1) {
          break;
        }
      }
      lines.reverse();
    }

    // append current line
    lines.push(currentContent);

    // expand bottom, stop on whitespaces or length > 2048
    length = 0;
    while ((line = terminal.buffer.active.getLine(++bottomIdx)) && line.isWrapped && length < 2048) {
      content = line.translateToString(true);
      length += content.length;
      lines.push(content);
      if (content.indexOf(" ") !== -1) {
        break;
      }
    }
  }
  return [lines, topIdx];
}

function mapStrIdx(terminal: Terminal, lineIndex: number, rowIndex: number, stringIndex: number): [number, number] {
  const buf = terminal.buffer.active;
  const cell = buf.getNullCell();
  let start = rowIndex;
  while (stringIndex) {
    const line = buf.getLine(lineIndex);
    if (!line) {
      return [-1, -1];
    }
    for (let i = start; i < line.length; ++i) {
      line.getCell(i, cell);
      const chars = cell.getChars();
      const width = cell.getWidth();
      if (width) {
        stringIndex -= chars.length || 1;

        // correct stringIndex for early wrapped wide chars:
        // - currently only happens at last cell
        // - cells to the right are reset with chars='' and width=1 in InputHandler.print
        // - follow-up line must be wrapped and contain wide char at first cell
        // --> if all these conditions are met, correct stringIndex by +1
        if (i === line.length - 1 && chars === "") {
          const nxt = buf.getLine(lineIndex + 1);
          if (nxt && nxt.isWrapped) {
            nxt.getCell(0, cell);
            if (cell.getWidth() === 2) {
              stringIndex += 1;
            }
          }
        }
      }
      if (stringIndex < 0) {
        return [lineIndex, i];
      }
    }
    lineIndex++;
    start = 0;
  }
  return [lineIndex, start];
}

function computeFileLinks(
  bufferLineNumber: number,
  regex: RegExp,
  terminal: Terminal,
  activate: (e: MouseEvent, text: string) => void,
) {
  const rex = new RegExp(regex.source, regex.flags.includes("g") ? regex.flags : regex.flags + "g");
  const [lines, startLineIndex] = getWindowedLineStrings(bufferLineNumber - 1, terminal);
  const line = lines.join("");
  let m: RegExpExecArray | null;
  const result: { range: { start: { x: number; y: number }; end: { x: number; y: number } }; text: string; activate: (e: MouseEvent, text: string) => void }[] = [];
  while ((m = rex.exec(line)) !== null) {
    const text = m[0];
    const [startY, startX] = mapStrIdx(terminal, startLineIndex, 0, m.index);
    const [endY, endX] = mapStrIdx(terminal, startY, startX, text.length);
    if (startY === -1 || startX === -1 || endY === -1 || endX === -1) continue;
    const range = {
      start: { x: startX + 1, y: startY + 1 },
      end: { x: endX, y: endY + 1 },
    };
    result.push({ range, text, activate });
  }
  return result;
}

export function registerFileLinkProvider(terminal: Terminal, projectPath: string | undefined) {
  if (typeof (terminal as unknown as { registerLinkProvider?: unknown }).registerLinkProvider !== "function") {
    return { dispose() {} };
  }
  const baseRegex = buildPathRegex();
  return (terminal as unknown as { registerLinkProvider: Terminal["registerLinkProvider"] }).registerLinkProvider({
    provideLinks: (bufferLineNumber, cb) => {
      const links = computeFileLinks(
        bufferLineNumber,
        baseRegex,
        terminal,
        (_e, text) => {
          const { path, line } = splitPathToken(text);
          const detail: OpenFileDetail = { path, line };
          if (projectPath) detail.projectRoot = projectPath;
          window.dispatchEvent(new CustomEvent<OpenFileDetail>(OPEN_FILE_EVENT, { detail }));
        },
      );
      cb(links.length ? links : undefined);
    },
  });
}
