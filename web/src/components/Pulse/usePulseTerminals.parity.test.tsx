import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import parityFixture from "../../../../internal/server/testdata/pulse_screen_parity.json?raw";

// The restore is mocked to feed each fixture's bytes through onText, so the real
// libghostty wasm (no ghostty-web mock) and its screen read run. The fixture is shared with the server's
// TestPulseScreenParity, so the card and the terminal_screen tool are held to the
// same expectations.
const mockRestore = vi.fn();
vi.mock("../Terminal/terminalHistory", () => ({
  restoreTerminalHistory: (...args: unknown[]) => mockRestore(...args),
}));

import { usePulseTerminalScreen } from "./usePulseTerminals";

type ParityCase = {
  name: string;
  cols: number;
  rows: number;
  raw: string;
  expect: string[];
  known_gap?: string;
};

const cases: ParityCase[] = JSON.parse(parityFixture);

describe("pulse screen parity with terminal_screen", () => {
  for (const c of cases) {
    it(`${c.name} renders the expected screen`, async () => {
      mockRestore.mockImplementation(async (options: { onText: (text: string) => void | Promise<void> }) => {
        await options.onText(c.raw);
        return { kind: "restored", snapshotEnd: c.raw.length, state: "active" };
      });
      const { result } = renderHook(() =>
        usePulseTerminalScreen({ id: "term-1", project: "/project", cols: c.cols, rows: c.rows }),
      );
      await waitFor(() => expect(result.current.screen).not.toBeNull());
      expect(result.current.screen).toEqual(c.expect);
    });
  }
});
