import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentRunDisplayScope } from "./agentRunDisplay";
import { ThinkingBlock } from "../Chat/TurnParts";
import { ChatDisplayTestProvider } from "../Chat/chatDisplayTestUtils";

// The real hook fetches from the server; here we pin the policy it would
// resolve, so the scope's two behaviours are observable.
const mockPreset = vi.hoisted(() => ({ value: "full" as "full" | "quiet" }));

vi.mock("../../lib/chatVerbosity", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../lib/chatVerbosity")>();
  return {
    ...actual,
    useChatVerbosity: () => {
      const config = actual.normalizeChatVerbosityConfig({
        preset: mockPreset.value,
        overrides: actual.DEFAULT_CHAT_VERBOSITY_CONFIG.overrides,
      });
      return {
        config,
        policy: actual.resolveChatDisplayPolicy(config),
        revision: mockPreset.value,
        loading: false,
        error: null,
      };
    },
  };
});

function reasoning() {
  return <ThinkingBlock text="private reasoning" blockKey="run:1:0:thinking" answerStarted={false} />;
}

describe("AgentRunDisplayScope", () => {
  it("supplies the chat display setting when no ChatPanel provider exists", () => {
    mockPreset.value = "quiet";
    render(<AgentRunDisplayScope>{reasoning()}</AgentRunDisplayScope>);

    // Quiet collapses thinking: the header stays, the body does not.
    expect(screen.getByText(/Thinking/)).toBeInTheDocument();
    expect(screen.queryByText("private reasoning")).toBeNull();
  });

  it("passes an inherited provider through instead of replacing it", () => {
    // The mocked hook says quiet, but the enclosing provider says full: a run
    // card rendered inline in the chat must keep the chat's policy/store.
    mockPreset.value = "quiet";
    render(
      <ChatDisplayTestProvider preset="full">
        <AgentRunDisplayScope>{reasoning()}</AgentRunDisplayScope>
      </ChatDisplayTestProvider>,
    );

    expect(screen.getByText("private reasoning")).toBeInTheDocument();
  });
});
