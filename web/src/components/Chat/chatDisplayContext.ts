import { createContext, useContext } from "react";
import type { ChatDisplayPolicy, ChatVerbosityConfig } from "@/api/types";

/** Per-block disclosure storage owned by ChatPanel. The store survives row
 *  unmount/remount while the transcript is virtualized. */
export interface ChatDisclosureStore {
  get(key: string, fallback: boolean): boolean;
  set(key: string, open: boolean): void;
  subscribe(key: string, listener: () => void): () => void;
  clear(): void;
}

export interface ChatDisplayContextValue {
  config: ChatVerbosityConfig;
  policy: ChatDisplayPolicy;
  disclosure: ChatDisclosureStore;
}

export const ChatDisplayContext = createContext<ChatDisplayContextValue | null>(null);

export function useChatDisplay(): ChatDisplayContextValue {
  const value = useContext(ChatDisplayContext);
  if (!value) {
    throw new Error("useChatDisplay must be used within ChatDisplayContext.Provider");
  }
  return value;
}
