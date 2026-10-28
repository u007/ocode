// Stateful scanner over the pty byte stream (already decoded to strings; chunk boundaries are
// arbitrary). ghostty-web exposes no OSC handler API, rings `onBell` whenever a written chunk
// contains BEL anywhere (including the BEL that terminates `ESC ] 0 ; title BEL`), and only
// sees titles that are complete inside ONE chunk. So the panel scans the stream itself and
// feeds the same bytes to `term.write`.
//
// Only the 7-bit forms are tracked. The 8-bit C1 introducers/terminators (0x9d OSC, 0x9c ST)
// are not handled: in a UTF-8 stream they arrive as U+009D/U+009C, which real shells and
// programs never emit for these purposes.

export interface TerminalStreamHandlers {
  /** OSC string complete: `code` is the leading numeric parameter (NaN when absent/non-numeric),
   *  `payload` is everything after the first `;` ("" when there is none). */
  onOsc(code: number, payload: string): void;
  /** A BEL (0x07) in the ground state. BEL that terminates an OSC/DCS/APC/PM/SOS string is NOT a bell. */
  onBell(): void;
}

export interface TerminalStreamScanner {
  scan(chunk: string): void;
  /** Drop any half-received control string (call on reset/reconnect). */
  reset(): void;
}

/** An OSC 52 clipboard payload can be large; anything past this is garbage or hostile. */
const MAX_OSC_BYTES = 1024 * 1024;

type State = "ground" | "esc" | "osc" | "oscEsc" | "string" | "stringEsc";

const ESC = "\x1b";
const BEL = "\x07";

export function createTerminalStreamScanner(handlers: TerminalStreamHandlers): TerminalStreamScanner {
  let state: State = "ground";
  // Collected as pieces so a long OSC does not re-copy on every character.
  let osc: string[] = [];
  let oscLen = 0;

  const finishOsc = () => {
    const body = osc.join("");
    osc = [];
    oscLen = 0;
    state = "ground";
    const semi = body.indexOf(";");
    const head = semi === -1 ? body : body.slice(0, semi);
    const payload = semi === -1 ? "" : body.slice(semi + 1);
    // Number("") is 0, which would read as "OSC 0"; an absent code is NaN.
    const code = /^\d+$/.test(head) ? Number(head) : NaN;
    handlers.onOsc(code, payload);
  };

  const dropOsc = () => {
    osc = [];
    oscLen = 0;
  };

  const step = (ch: string) => {
    switch (state) {
      case "ground":
        if (ch === ESC) state = "esc";
        else if (ch === BEL) handlers.onBell();
        return;
      case "esc":
        if (ch === "]") state = "osc";
        else if (ch === "P" || ch === "_" || ch === "^" || ch === "X") state = "string";
        else if (ch === ESC) state = "esc";
        else state = "ground";
        // CSI and the rest are not tracked. A CSI parameter byte is never ESC, "]" or BEL, so
        // processing the following bytes in ground is safe.
        return;
      case "osc":
        if (ch === BEL) {
          finishOsc();
        } else if (ch === ESC) {
          state = "oscEsc";
        } else {
          osc.push(ch);
          oscLen += 1;
          if (oscLen > MAX_OSC_BYTES) {
            console.warn(`terminal: OSC string exceeded ${MAX_OSC_BYTES} bytes; discarded`);
            dropOsc();
            state = "ground";
          }
        }
        return;
      case "oscEsc":
        if (ch === "\\") {
          finishOsc();
        } else {
          // An ESC inside an OSC can only legally start ST; anything else abandons the OSC and
          // the character begins a fresh escape sequence.
          dropOsc();
          state = "esc";
          step(ch);
        }
        return;
      case "string":
        if (ch === ESC) state = "stringEsc";
        return;
      case "stringEsc":
        if (ch === "\\") state = "ground";
        else if (ch === ESC) state = "stringEsc";
        else state = "string"; // BEL inside DCS/APC/PM/SOS is data
        return;
    }
  };

  return {
    scan(chunk: string) {
      for (let i = 0; i < chunk.length; i++) step(chunk[i]);
    },
    reset() {
      state = "ground";
      dropOsc();
    },
  };
}
