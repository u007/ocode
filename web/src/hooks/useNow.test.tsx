import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";
import { activeTickerCount, useNow } from "./useNow";

function Probe({ onRender }: { onRender: (now: number) => void }) {
  onRender(useNow(1000));
  return null;
}

afterEach(() => vi.useRealTimers());

describe("useNow", () => {
  it("shares one interval across subscribers and stops it with the last one", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-06T10:00:00.000Z"));
    const setIntervalSpy = vi.spyOn(globalThis, "setInterval");
    const seen: number[] = [];
    const a = render(<Probe onRender={(n) => seen.push(n)} />);
    const b = render(<Probe onRender={() => {}} />);
    expect(setIntervalSpy).toHaveBeenCalledTimes(1);
    expect(activeTickerCount()).toBe(1);

    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(seen[seen.length - 1]).toBe(Date.parse("2026-08-06T10:00:02.000Z"));

    a.unmount();
    expect(activeTickerCount()).toBe(1);
    b.unmount();
    expect(activeTickerCount()).toBe(0);
    setIntervalSpy.mockRestore();
  });
});
