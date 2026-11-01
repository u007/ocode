import { useContext, useMemo, type ReactNode } from "react";
import { useChatVerbosity } from "../../lib/chatVerbosity";
import {
  ChatDisplayContext,
  createChatDisclosureStore,
  type ChatDisplayContextValue,
} from "../Chat/chatDisplayContext";

// Agent-run surfaces (the chat's Agents rail and the Agents tab) render OUTSIDE
// ChatPanel, so they do not inherit its ChatDisplayContext. They still render
// the chat's own ThinkingBlock / ToolBlock / AssistantText (see RunNode), which
// read that context, so this scope supplies the SAME Chat display setting plus
// a disclosure store of its own.
//
// The boundary is a pass-through when a provider already exists: a run card
// rendered inline in the main transcript (TurnParts' task block) must keep
// using the chat's policy AND its disclosure store, so expanding a block there
// stays one shared choice.
export function AgentRunDisplayScope({ children }: { children: ReactNode }) {
  const inherited = useContext(ChatDisplayContext);
  if (inherited) return <>{children}</>;
  return <AgentRunDisplayFallback>{children}</AgentRunDisplayFallback>;
}

function AgentRunDisplayFallback({ children }: { children: ReactNode }) {
  const { config, policy } = useChatVerbosity();
  const disclosure = useMemo(createChatDisclosureStore, []);
  const value = useMemo<ChatDisplayContextValue>(
    () => ({ config, policy, disclosure }),
    [config, policy, disclosure],
  );
  return <ChatDisplayContext.Provider value={value}>{children}</ChatDisplayContext.Provider>;
}
