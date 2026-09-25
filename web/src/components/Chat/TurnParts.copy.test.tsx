import { act, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ThinkingBlock, NoticeBlock, StatusBlock, ToolBlock } from "./TurnParts";
import { ChatDisplayContext, type ChatDisclosureStore } from "./chatDisplayContext";
import { DEFAULT_CHAT_VERBOSITY_CONFIG, resolveChatDisplayPolicy } from "../../lib/chatVerbosity";

const copyTextToClipboard = vi.hoisted(() => vi.fn(async (_text: string) => true));
vi.mock("../../lib/clipboard", () => ({ copyTextToClipboard }));

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

async function copyDefault() {
  await act(async () => {
    fireEvent.click(screen.getByTestId("block-copy-default"));
  });
}

describe("chat block copy controls", () => {
  afterEach(() => copyTextToClipboard.mockClear());

  it("copies thinking text and labels the raw item as source", async () => {
    withDisplay(<ThinkingBlock text="some reasoning" />);
    await copyDefault();
    expect(copyTextToClipboard).toHaveBeenLastCalledWith("some reasoning");
    fireEvent.click(screen.getByTestId("block-copy-menu"));
    expect(await screen.findByText(/copy as raw source/i)).toBeInTheDocument();
  });

  it("copies tool args and output without the disclosure control labels", async () => {
    withDisplay(
      <ToolBlock
        tool="bash"
        command='{"command":"ls -la"}'
        output={"line one\nline two"}
      />,
    );
    await copyDefault();
    const calls = copyTextToClipboard.mock.calls;
    const copied = calls[calls.length - 1]?.[0] as string;
    expect(copied).toContain("ls -la");
    expect(copied).toContain("line one");
    expect(copied).not.toMatch(/show output/i);
    expect(copied).not.toMatch(/hide output/i);
  });

  it("copies status and notice text", async () => {
    withDisplay(
      <>
        <StatusBlock text="consulting the judge" />
        <NoticeBlock text="Discovered: a skill" />
      </>,
    );
    const buttons = screen.getAllByTestId("block-copy-default");
    await act(async () => {
      fireEvent.click(buttons[0]);
    });
    expect(copyTextToClipboard).toHaveBeenLastCalledWith("consulting the judge");
    await act(async () => {
      fireEvent.click(screen.getAllByTestId("block-copy-default")[1]);
    });
    expect(copyTextToClipboard).toHaveBeenLastCalledWith("Discovered: a skill");
  });
});
