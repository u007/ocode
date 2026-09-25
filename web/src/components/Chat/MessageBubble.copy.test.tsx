import { act, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import MessageBubble from "./MessageBubble";
import { ChatDisplayContext, type ChatDisclosureStore } from "./chatDisplayContext";
import { DEFAULT_CHAT_VERBOSITY_CONFIG, resolveChatDisplayPolicy } from "../../lib/chatVerbosity";

const copyTextToClipboard = vi.hoisted(() => vi.fn(async (_text: string) => true));
vi.mock("../../lib/clipboard", () => ({ copyTextToClipboard }));

describe("MessageBubble copy", () => {
  afterEach(() => copyTextToClipboard.mockClear());

  function withDisplay(ui: ReactNode) {
    const disclosure: ChatDisclosureStore = {
      get: (_key, fallback) => fallback,
      set: () => {},
      subscribe: () => () => {},
      clear: () => {},
    };
    return render(
      <ChatDisplayContext.Provider
        value={{
          config: DEFAULT_CHAT_VERBOSITY_CONFIG,
          policy: resolveChatDisplayPolicy(DEFAULT_CHAT_VERBOSITY_CONFIG),
          disclosure,
        }}
      >
        {ui}
      </ChatDisplayContext.Provider>,
    );
  }

  it("copies rendered text by default and raw Markdown from the menu", async () => {
    render(<MessageBubble message={{ role: "assistant", content: "# Title\n\nBody **bold**" }} />);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /copy message/i }));
    });
    expect(copyTextToClipboard).toHaveBeenLastCalledWith("Title\nBody bold");

    fireEvent.click(screen.getByTestId("block-copy-menu"));
    const raw = await screen.findByTestId("block-copy-raw");
    await act(async () => {
      fireEvent.click(raw);
    });
    expect(copyTextToClipboard).toHaveBeenLastCalledWith("# Title\n\nBody **bold**");
  });

  it("copies user messages verbatim", async () => {
    render(<MessageBubble message={{ role: "user", content: "hello **world**" }} />);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /copy message/i }));
    });
    expect(copyTextToClipboard).toHaveBeenLastCalledWith("hello **world**");
  });

  it("labels the raw item as raw source on a tool result", async () => {
    withDisplay(
      <MessageBubble message={{ role: "tool", content: "tool output" }} toolName="bash" />,
    );
    fireEvent.click(screen.getByTestId("block-copy-menu"));
    expect(await screen.findByText(/copy as raw source/i)).toBeInTheDocument();
  });
});
