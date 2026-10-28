import { describe, it, expect, vi, afterEach } from "vitest";
import { createTerminalStreamScanner } from "./terminalStream";

function setup() {
  const oscs: Array<[number, string]> = [];
  let bells = 0;
  const scanner = createTerminalStreamScanner({
    onOsc: (code, payload) => oscs.push([code, payload]),
    onBell: () => {
      bells += 1;
    },
  });
  return { scanner, oscs, bells: () => bells };
}

afterEach(() => vi.restoreAllMocks());

describe("terminalStream", () => {
  it("reports OSC 0 and 2 titles with BEL terminator and no bell", () => {
    const t = setup();
    t.scanner.scan("\x1b]0;my title\x07\x1b]2;other\x07");
    expect(t.oscs).toEqual([[0, "my title"], [2, "other"]]);
    expect(t.bells()).toBe(0);
  });

  it("reports OSC with ESC \\ terminator", () => {
    const t = setup();
    t.scanner.scan("\x1b]2;title\x1b\\after");
    expect(t.oscs).toEqual([[2, "title"]]);
    expect(t.bells()).toBe(0);
  });

  it("is invariant to chunk boundaries at every split index", () => {
    const sample = "hi\x07\x1b]0;a;b title\x07x\x1b[31mred\x1b]52;c;QUJD\x1b\\\x1bPq\x07data\x1b\\\x07end";
    for (let i = 0; i <= sample.length; i++) {
      const t = setup();
      t.scanner.scan(sample.slice(0, i));
      t.scanner.scan(sample.slice(i));
      expect(t.oscs).toEqual([[0, "a;b title"], [52, "c;QUJD"]]);
      expect(t.bells()).toBe(2); // leading and trailing BEL; the DCS BEL is data
    }
  });

  it("splits OSC 52 payload into targets;base64", () => {
    const t = setup();
    t.scanner.scan("\x1b]52;c;SGVsbG8=\x07");
    expect(t.oscs).toEqual([[52, "c;SGVsbG8="]]);
  });

  it("reports OSC 9, 777 and 99", () => {
    const t = setup();
    t.scanner.scan("\x1b]9;done\x07\x1b]777;notify;t;b\x07\x1b]99;i=1;hello\x1b\\");
    expect(t.oscs).toEqual([[9, "done"], [777, "notify;t;b"], [99, "i=1;hello"]]);
  });

  it("gives NaN code for non-numeric or absent code and empty payload without semicolon", () => {
    const t = setup();
    t.scanner.scan("\x1b]abc;x\x07\x1b];y\x07\x1b]8\x07");
    expect(t.oscs.map(([c]) => Number.isNaN(c))).toEqual([true, true, false]);
    expect(t.oscs[0][1]).toBe("x");
    expect(t.oscs[1][1]).toBe("y");
    expect(t.oscs[2]).toEqual([8, ""]);
  });

  it("fires bell for BEL in the ground state", () => {
    const t = setup();
    t.scanner.scan("a\x07b\x07");
    expect(t.bells()).toBe(2);
  });

  it("does not fire bell for the BEL terminating an OSC", () => {
    const t = setup();
    t.scanner.scan("\x1b]0;t\x07");
    expect(t.bells()).toBe(0);
  });

  it("does not fire bell for BEL inside DCS, and recovers after ST", () => {
    const t = setup();
    t.scanner.scan("\x1bPabc\x07def\x1b\\\x07");
    expect(t.bells()).toBe(1);
    expect(t.oscs).toEqual([]);
  });

  it("swallows APC, PM and SOS strings", () => {
    const t = setup();
    t.scanner.scan("\x1b_a\x07\x1b\\\x1b^b\x07\x1b\\\x1bXc\x07\x1b\\");
    expect(t.bells()).toBe(0);
  });

  it("abandons an OSC interrupted by a non-ST escape and reprocesses the char", () => {
    const t = setup();
    t.scanner.scan("\x1b]0;half\x1b]2;full\x07");
    expect(t.oscs).toEqual([[2, "full"]]);
  });

  it("discards an overflowing OSC, warns once, and recovers", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const t = setup();
    t.scanner.scan("\x1b]52;c;" + "A".repeat(1024 * 1024 + 10));
    expect(warn).toHaveBeenCalledTimes(1);
    t.scanner.scan("\x07\x1b]0;ok\x07");
    // The tail of the discarded payload and its BEL are ground-state bytes now.
    expect(t.oscs).toEqual([[0, "ok"]]);
  });

  it("reset drops a half-received OSC", () => {
    const t = setup();
    t.scanner.scan("\x1b]0;partial");
    t.scanner.reset();
    t.scanner.scan("\x07");
    expect(t.oscs).toEqual([]);
    expect(t.bells()).toBe(1);
  });
});
