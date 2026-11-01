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

/** In-memory disclosure store. A scope that does not inherit ChatPanel's store
 *  (the agent-run surfaces render outside the chat transcript) owns one of
 *  these; keys are stable per block, so a remount keeps the user's choices. */
export function createChatDisclosureStore(): ChatDisclosureStore {
  const values = new Map<string, boolean>();
  const listeners = new Map<string, Set<() => void>>();
  return {
    get: (key, fallback) => values.get(key) ?? fallback,
    set: (key, open) => {
      if (values.get(key) === open) return;
      values.set(key, open);
      listeners.get(key)?.forEach((listener) => listener());
    },
    subscribe: (key, listener) => {
      const set = listeners.get(key) ?? new Set<() => void>();
      set.add(listener);
      listeners.set(key, set);
      return () => {
        set.delete(listener);
        if (set.size === 0) listeners.delete(key);
      };
    },
    clear: () => {
      values.clear();
      listeners.forEach((set) => set.forEach((listener) => listener()));
    },
  };
}

export function useChatDisplay(): ChatDisplayContextValue {
  const value = useContext(ChatDisplayContext);
  if (!value) {
    throw new Error("useChatDisplay must be used within ChatDisplayContext.Provider");
  }
  return value;
}
