import { render, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { TerminalKeyboardPad } from "./TerminalKeyboardPad";

function makeTerm() {
  const input = vi.fn((data: string, wasUserInput: boolean) => {
    if (wasUserInput) term._data?.(data);
  });
  const modes: Record<number, boolean> = {};
  const term = {
    input,
    getMode: vi.fn((mode: number) => modes[mode] ?? false),
    _data: undefined as ((data: string) => void) | undefined,
  };
  return { term, input, modes };
}

function renderPad(
  enabled: boolean,
  term: ReturnType<typeof makeTerm>["term"],
) {
  const inputGuard = { noteInput: vi.fn() };
  return {
    inputGuard,
    render: render(
      <TerminalKeyboardPad
        getTerm={() => term}
        enabled={enabled}
        inputGuard={inputGuard}
      />,
    ),
  };
}

describe("TerminalKeyboardPad", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("is disabled when the shell is not attached", () => {
    const { term } = makeTerm();
    const { render: r } = renderPad(false, term);
    const button = r.container.querySelector(
      '[data-testid="terminal-keyboard-pad"]',
    );
    expect(button).not.toBeNull();
    expect(button).toBeDisabled();
  });

  it("sends an up arrow as CSI in normal cursor mode", () => {
    const { term, input } = makeTerm();
    term.getMode = vi.fn(() => false); // normal cursor mode
    const { render: r, inputGuard } = renderPad(true, term);

    fireEvent.click(
      r.container.querySelector('[data-testid="terminal-keyboard-pad"]')!,
    );
    // Menu opens: click the up-arrow item.
    fireEvent.click(
      document.body.querySelector(
        '[data-testid="terminal-keyboard-arrow-up"]',
      )!,
    );

    expect(input).toHaveBeenCalledWith("\x1b[A", true);
    expect(inputGuard.noteInput).toHaveBeenCalledTimes(1);
    expect(inputGuard.noteInput.mock.invocationCallOrder[0]).toBeLessThan(
      input.mock.invocationCallOrder[0],
    );
  });

  it("sends an up arrow in application-cursor mode when mode 1 is set", () => {
    const { term, input } = makeTerm();
    term.getMode = vi.fn((mode: number) => mode === 1); // DECCKM on
    const { render: r } = renderPad(true, term);

    fireEvent.click(
      r.container.querySelector('[data-testid="terminal-keyboard-pad"]')!,
    );
    fireEvent.click(
      document.body.querySelector(
        '[data-testid="terminal-keyboard-arrow-up"]',
      )!,
    );

    expect(input).toHaveBeenCalledWith("\x1bOA", true);
  });

  it("sends ^C for the interrupt item", () => {
    const { term, input } = makeTerm();
    const { render: r } = renderPad(true, term);

    fireEvent.click(
      r.container.querySelector('[data-testid="terminal-keyboard-pad"]')!,
    );
    fireEvent.click(
      document.body.querySelector('[data-testid="terminal-keyboard-^c"]')!,
    );

    expect(input).toHaveBeenCalledWith("\x03", true);
  });

  it("sends Esc for the Esc item", () => {
    const { term, input } = makeTerm();
    const { render: r } = renderPad(true, term);

    fireEvent.click(
      r.container.querySelector('[data-testid="terminal-keyboard-pad"]')!,
    );
    fireEvent.click(
      document.body.querySelector('[data-testid="terminal-keyboard-esc"]')!,
    );

    expect(input).toHaveBeenCalledWith("\x1b", true);
  });

  it("closes the popover after sending", () => {
    const { term } = makeTerm();
    const { render: r } = renderPad(true, term);

    const trigger = r.container.querySelector(
      '[data-testid="terminal-keyboard-pad"]',
    )!;
    fireEvent.click(trigger);
    expect(trigger.getAttribute("aria-expanded")).toBe("true");

    fireEvent.click(
      document.body.querySelector('[data-testid="terminal-keyboard-esc"]')!,
    );
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
  });

  it("does not send when disabled", () => {
    const { term, input } = makeTerm();
    const { render: r, inputGuard } = renderPad(false, term);

    fireEvent.click(
      r.container.querySelector('[data-testid="terminal-keyboard-pad"]')!,
    );
    expect(
      document.body.querySelector('[role="menu"]'),
    ).not.toBeInTheDocument();
    expect(input).not.toHaveBeenCalled();
    expect(inputGuard.noteInput).not.toHaveBeenCalled();
  });
});
