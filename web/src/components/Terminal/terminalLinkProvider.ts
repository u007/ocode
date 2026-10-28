import { buildPathRegex, splitPathToken, OPEN_FILE_EVENT, type OpenFileDetail } from "../../lib/fileLinks";
import type { IDisposable, ILink, Terminal } from "ghostty-web";

// IBufferLine is not exported by ghostty-web; derive it from the public buffer API.
type BufferLine = NonNullable<ReturnType<Terminal["buffer"]["active"]["getLine"]>>;

// Adapted from xterm WebLinkProvider's helpers (MIT) — see
// https://github.com/xtermjs/xterm.js/blob/master/addons/addon-web-links/src/WebLinkProvider.ts
// Reused for file-path and URL links so wrapping and wide-char mapping stay correct. Ported to
// ghostty-web's buffer API: rows are 0-based absolute, getCell(x) returns the cell (or
// undefined past the line) instead of filling one in.

function getWindowedLineStrings(lineIndex: number, terminal: Terminal): [string[], number] {
  let line: BufferLine | undefined;
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
  let start = rowIndex;
  while (stringIndex) {
    const line = buf.getLine(lineIndex);
    if (!line) {
      return [-1, -1];
    }
    for (let i = start; i < line.length; ++i) {
      const cell = line.getCell(i);
      if (!cell) break; // past the end of the line
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
            const first = nxt.getCell(0);
            if (first && first.getWidth() === 2) {
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

interface Match {
  text: string;
  range: ILink["range"];
}

/** Regex matches over the windowed line strings around `row` (0-based absolute), mapped back to
 *  buffer cells. `refine` may shorten a match (e.g. strip trailing punctuation) before mapping. */
function computeMatches(row: number, regex: RegExp, terminal: Terminal, refine?: (text: string) => string): Match[] {
  const rex = new RegExp(regex.source, regex.flags.includes("g") ? regex.flags : regex.flags + "g");
  const [lines, startLineIndex] = getWindowedLineStrings(row, terminal);
  const line = lines.join("");
  let m: RegExpExecArray | null;
  const result: Match[] = [];
  while ((m = rex.exec(line)) !== null) {
    const text = refine ? refine(m[0]) : m[0];
    if (!text) continue;
    const [startY, startX] = mapStrIdx(terminal, startLineIndex, 0, m.index);
    const [endY, endX] = mapStrIdx(terminal, startY, startX, text.length);
    if (startY === -1 || startX === -1 || endY === -1 || endX === -1) continue;
    // mapStrIdx's end is exclusive; ghostty-web ranges are inclusive. A link that ends exactly
    // at a row boundary maps to column 0 of the next row, so step back to the last cell of the
    // row it actually occupies.
    let end = { x: endX - 1, y: endY };
    if (endX === 0 && endY > startY) {
      const prev = terminal.buffer.active.getLine(endY - 1);
      if (!prev) continue;
      end = { x: prev.length - 1, y: endY - 1 };
    }
    result.push({ text, range: { start: { x: startX, y: startY }, end } });
  }
  return result;
}

// ghostty-web's registerLinkProvider returns void and only unregisters providers on
// term.dispose(), so our disposer has nothing to undo.
const NOOP_DISPOSABLE: IDisposable = { dispose() {} };

export function registerFileLinkProvider(
  terminal: Terminal,
  projectPath: string | undefined,
  guard?: () => boolean,
): IDisposable {
  const baseRegex = buildPathRegex();
  terminal.registerLinkProvider({
    provideLinks: (row, cb) => {
      const links: ILink[] = computeMatches(row, baseRegex, terminal).map(({ text, range }) => ({
        text,
        range,
        activate: (e: MouseEvent) => {
          if (!(e.ctrlKey || e.metaKey)) return;
          if (guard && guard()) return;
          const { path, line } = splitPathToken(text);
          const detail: OpenFileDetail = { path, line };
          if (projectPath) detail.projectRoot = projectPath;
          window.dispatchEvent(new CustomEvent<OpenFileDetail>(OPEN_FILE_EVENT, { detail }));
        },
      }));
      cb(links.length ? links : undefined);
    },
  });
  return NOOP_DISPOSABLE;
}

const URL_REGEX = /https?:\/\/[^\s<>"'`]+/;
const TRAILING_PUNCTUATION = /[.,;:!?)\]}>'"]+$/;

/** Plain http(s) URLs in the output. A left click opens (ctrl/cmd-click too) — that is the
 *  existing ocode behaviour, no modifier required. `guard()` true means the pointer dragged. */
export function registerUrlLinkProvider(
  terminal: Terminal,
  open: (uri: string) => void,
  guard?: () => boolean,
): IDisposable {
  terminal.registerLinkProvider({
    provideLinks: (row, cb) => {
      const links: ILink[] = computeMatches(row, URL_REGEX, terminal, (t) => t.replace(TRAILING_PUNCTUATION, "")).map(
        ({ text, range }) => ({
          text,
          range,
          activate: (e: MouseEvent) => {
            if (e.button !== 0 || guard?.()) return;
            open(text);
          },
        }),
      );
      cb(links.length ? links : undefined);
    },
  });
  return NOOP_DISPOSABLE;
}
