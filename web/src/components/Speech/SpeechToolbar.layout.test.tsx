import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import SpeechToolbar from "./SpeechToolbar";

vi.mock("./SpeechProvider", () => ({
  useSpeech: () => ({
    config: { mode: "manual", engine: "browser-native" },
    status: { engine: { label: "Browser Native", availability: "available" }, playback: null },
    isSpeaking: false,
    paused: false,
    error: null,
    currentText: "",
    position: 0,
    duration: 0,
    speak: vi.fn(),
    stop: vi.fn(),
    pause: vi.fn(),
    resume: vi.fn(),
    skip: vi.fn(),
    seek: vi.fn(),
    retry: vi.fn(),
    toolbarVisible: true,
    setToolbarVisible: vi.fn(),
    setMode: vi.fn(),
  }),
  playbackLabel: () => "Idle",
}));

describe("SpeechToolbar responsive layout", () => {
  it("wraps full-width on phones and keeps the centered nowrap pill from sm up", () => {
    // Regression: the toolbar was a single non-wrapping row, so on a 390px
    // viewport its controls/error text overflowed past the right edge.
    const { container } = render(<SpeechToolbar />);
    const bar = container.firstElementChild as HTMLElement;
    expect(bar.className).toMatch(/flex-wrap/);
    expect(bar.className).toMatch(/inset-x-2/);
    // ≥sm restores the original behavior (centered, single line).
    expect(bar.className).toMatch(/sm:flex-nowrap/);
    expect(bar.className).toMatch(/sm:left-1\/2/);
    // No base (mobile) left-1/2: it would re-center the full-width bar.
    expect(bar.className).not.toMatch(/(^|\s)left-1\/2(\s|$)/);
  });
});
