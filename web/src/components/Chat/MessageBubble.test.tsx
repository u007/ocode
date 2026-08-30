import { useState } from "react";
import { render, screen, act } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import MessageBubble from "./MessageBubble";
import type { Message } from "../../api/types";

// Guards the fix for the desktop-app CPU spike: every streamed "thinking"
// delta replaced the `live` array reference, re-rendering ChatPanel and (pre
// -memo) re-invoking ReactMarkdown for every already-committed message on
// every single token. The render-count assertion below is the actual check
// -- a passing typecheck alone doesn't prove the memo bails.
const renderSpy = vi.fn();
vi.mock("react-markdown", () => ({
  default: (props: { children?: unknown }) => {
    renderSpy();
    return <div>{String(props.children ?? "")}</div>;
  },
}));

function Harness({ message }: { message: Message }) {
  const [tick, setTick] = useState(0);
  return (
    <div>
      <button onClick={() => setTick((t) => t + 1)}>tick</button>
      <span data-testid="tick">{tick}</span>
      <MessageBubble message={message} />
    </div>
  );
}

describe("MessageBubble memoization", () => {
  it("does not re-render (or re-run ReactMarkdown) when props are referentially unchanged", () => {
    const message: Message = { role: "assistant", content: "hello world" };
    render(<Harness message={message} />);
    expect(renderSpy).toHaveBeenCalledTimes(1);

    // Simulate the LIVE_DELTA case: the parent re-renders repeatedly (as it
    // does on every streamed thinking/text token) while this message's own
    // object reference never changes.
    for (let i = 0; i < 20; i++) {
      act(() => {
        screen.getByText("tick").parentElement?.querySelector("button")?.click();
      });
    }

    expect(screen.getByTestId("tick").textContent).toBe("20");
    expect(renderSpy).toHaveBeenCalledTimes(1);
  });
});

// The web theme mapping is contrast-safe (see useTheme.computeThemeVars):
// foregrounds on colored surfaces must come from the paired *-foreground
// token, never the inherited foreground. These tests pin the class selection
// for the surfaces the LCARS gray-on-orange fix touched, so a refactor can't
// silently reintroduce text-foreground on bg-primary/bg-accent.
describe("MessageBubble theme-surface classes", () => {
  it("renders user messages on the primary surface with text-primary-foreground", () => {
    const { container } = render(
      <MessageBubble message={{ role: "user", content: "change.\nsecond line" }} />,
    );
    const bubble = container.querySelector("div.bg-primary");
    expect(bubble).not.toBeNull();
    expect(bubble!.className).toContain("text-primary-foreground");
    expect(bubble!.className).not.toContain("text-muted-foreground");
    // Content is shown verbatim and wraps (multiline + long text).
    expect(bubble!.textContent).toContain("change.");
    expect(bubble!.textContent).toContain("second line");
    const pre = bubble!.querySelector("pre");
    expect(pre).not.toBeNull();
    expect(pre!.className).toContain("whitespace-pre-wrap");
  });

  it("renders assistant text on the muted surface with the foreground token", () => {
    const { container } = render(
      <MessageBubble message={{ role: "assistant", content: "hello world" }} />,
    );
    const bubble = container.querySelector("div.bg-muted");
    expect(bubble).not.toBeNull();
    expect(bubble!.className).toContain("text-foreground");
    expect(bubble!.textContent).toContain("hello world");
  });
});
