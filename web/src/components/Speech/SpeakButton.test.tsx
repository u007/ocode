import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SpeakButton, type SpeakAction } from "./SpeakButton";
import type { SpeechOutcome } from "./SpeechProvider";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

function renderButton(onSpeak: SpeakAction, getText: () => string | undefined = () => "hello") {
  return render(<SpeakButton getText={getText} onSpeak={onSpeak} ariaLabel="Speak message" title="Speak message" />);
}

const button = () => screen.getByRole("button", { name: /speak message/i });

afterEach(() => {
  vi.restoreAllMocks();
});

describe("SpeakButton", () => {
  it("disables itself, marks aria-busy and spins while the request is processing", async () => {
    const gate = deferred<SpeechOutcome>();
    const onSpeak = vi.fn(() => gate.promise);
    renderButton(onSpeak);

    fireEvent.click(button());

    expect(onSpeak).toHaveBeenCalledWith("hello");
    expect(button()).toBeDisabled();
    expect(button()).toHaveAttribute("aria-busy", "true");
    expect(button().textContent).toContain("Speaking…");

    await act(async () => {
      gate.resolve({ ok: true });
      await gate.promise;
    });

    expect(button()).not.toBeDisabled();
    expect(button()).toHaveAttribute("aria-busy", "false");
    expect(button().textContent).toContain("Speak");
  });

  it("re-enables and shows the failure reason when the request fails", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    const onSpeak = vi.fn().mockResolvedValue({ ok: false, error: "synthesis failed" });
    renderButton(onSpeak);

    await act(async () => {
      fireEvent.click(button());
    });

    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("synthesis failed"));
    expect(button()).not.toBeDisabled();
    expect(button().getAttribute("title")).toContain("synthesis failed");
    expect(warn).toHaveBeenCalled();
  });

  it("does not leave the button stuck when the action throws", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => undefined);
    const onSpeak = vi.fn().mockRejectedValue(new Error("boom"));
    renderButton(onSpeak);

    await act(async () => {
      fireEvent.click(button());
    });

    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("boom"));
    expect(button()).not.toBeDisabled();
  });

  it("ignores a second click while the first request is still pending", async () => {
    const gate = deferred<SpeechOutcome>();
    const onSpeak = vi.fn(() => gate.promise);
    renderButton(onSpeak);

    fireEvent.click(button());
    fireEvent.click(button());
    expect(onSpeak).toHaveBeenCalledTimes(1);

    await act(async () => {
      gate.resolve({ ok: true });
      await gate.promise;
    });
  });

  it("does nothing when there is no text to speak", () => {
    const onSpeak = vi.fn();
    renderButton(onSpeak, () => "   ");
    fireEvent.click(button());
    expect(onSpeak).not.toHaveBeenCalled();
  });

  it("honors an external disabled condition (e.g. nothing selected)", () => {
    const onSpeak = vi.fn();
    render(
      <SpeakButton
        getText={() => "hello"}
        onSpeak={onSpeak}
        ariaLabel="Speak selection"
        title="Speak selected chat text"
        idleLabel="Speak selection"
        disabled
      />,
    );
    const selection = screen.getByRole("button", { name: /speak selection/i });
    expect(selection).toBeDisabled();
    fireEvent.click(selection);
    expect(onSpeak).not.toHaveBeenCalled();
  });
});
