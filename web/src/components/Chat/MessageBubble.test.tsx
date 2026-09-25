import { useState, type ReactElement } from "react";
import { render as rtlRender, screen, act } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import MessageBubble from "./MessageBubble";
import type { Message } from "../../api/types";
import { ChatDisplayTestProvider } from "./chatDisplayTestUtils";

// MessageBubble's tool/assistant-with-tools paths read the controlled
// ChatDisplayContext; wrap every render so the throwing hook has a provider.
function render(ui: ReactElement) {
  return rtlRender(<ChatDisplayTestProvider>{ui}</ChatDisplayTestProvider>);
}

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

describe("MessageBubble compaction notice", () => {
  it("renders the persisted compaction summary as an inline notice, not a raw marker", () => {
    render(
      <MessageBubble
        message={{
          role: "system",
          content: "[ocode:compaction-summary]\nCompacted summary covering 24 messages\n\nbody text",
        }}
      />,
    );
    const notice = screen.getByTestId("compaction-notice");
    expect(notice).toHaveTextContent("Compacted summary covering 24 messages");
    expect(screen.queryByText(/\[ocode:compaction-summary\]/)).not.toBeInTheDocument();
  });
});


// Thinking models (DeepSeek v4.1 flash) emit bare newlines as the content of a
// tool-calling step while the prose lives in reasoning_content / the final
// answer. Those messages used to render as an empty bg-muted bubble carrying
// only its "Speak message" button (screenshot report 2026-09-23). Every
// AssistantText site must gate on hasRenderableText.
describe("MessageBubble whitespace-only assistant content", () => {
  it("does not render an empty text bubble (or its Speak button) on a tool-calling step", () => {
    render(
      <MessageBubble
        message={{
          role: "assistant",
          content: "\n\n",
          reasoning_content: "let me commit and push",
          tool_calls: [
            {
              id: "call_1",
              type: "function",
              function: { name: "bash", arguments: '{"command":"git push"}' },
            },
          ],
        }}
      />,
    );
    // Reasoning + the tool call still render…
    expect(screen.getByText("🧠 Thinking")).toBeInTheDocument();
    expect(screen.getByText(/git push/)).toBeInTheDocument();
    // …but no empty assistant bubble with a Speak control.
    expect(screen.queryByLabelText("Speak message")).not.toBeInTheDocument();
  });

  it("renders nothing for a whitespace-only assistant message with no tools or reasoning", () => {
    const { container } = render(
      <MessageBubble message={{ role: "assistant", content: "   \n\t" }} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("still renders real text that merely has surrounding whitespace", () => {
    const { container } = render(
      <MessageBubble message={{ role: "assistant", content: "\n\nDone.\n" }} />,
    );
    expect(container.querySelector("div.bg-muted")!.textContent).toContain("Done.");
  });
});
