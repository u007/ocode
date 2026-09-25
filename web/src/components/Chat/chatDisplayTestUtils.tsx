import { useMemo, type ReactNode } from "react";
import type {
  ChatDisplayOverride,
  ChatDisplayPolicy,
  ChatVerbosityConfig,
  ChatVerbosityPreset,
} from "@/api/types";
import {
  DEFAULT_CHAT_VERBOSITY_CONFIG,
  normalizeChatVerbosityConfig,
  resolveChatDisplayPolicy,
} from "@/lib/chatVerbosity";
import {
  ChatDisplayContext,
  type ChatDisclosureStore,
  type ChatDisplayContextValue,
} from "./chatDisplayContext";

/** In-memory disclosure store matching the ChatPanel-owned implementation. */
export function makeTestDisclosureStore(): ChatDisclosureStore {
  const values = new Map<string, boolean>();
  const listeners = new Map<string, Set<() => void>>();
  return {
    get: (key, fallback) => (values.has(key) ? values.get(key)! : fallback),
    set: (key, open) => {
      values.set(key, open);
      listeners.get(key)?.forEach((listener) => listener());
    },
    subscribe: (key, listener) => {
      const set = listeners.get(key) ?? new Set<() => void>();
      set.add(listener);
      listeners.set(key, set);
      return () => set.delete(listener);
    },
    clear: () => {
      values.clear();
      listeners.forEach((set) => set.forEach((listener) => listener()));
    },
  };
}

export function makeTestChatDisplayValue(
  preset: ChatVerbosityPreset = "full",
  overrides: Partial<ChatVerbosityConfig["overrides"]> = {},
  disclosure: ChatDisclosureStore = makeTestDisclosureStore(),
): ChatDisplayContextValue {
  const config = normalizeChatVerbosityConfig({
    preset,
    overrides: { ...DEFAULT_CHAT_VERBOSITY_CONFIG.overrides, ...overrides },
  });
  const policy: ChatDisplayPolicy = resolveChatDisplayPolicy(config);
  return { config, policy, disclosure };
}

/**
 * Wraps a rendered component in a populated ChatDisplayContext. Production
 * code throws without the provider (the context has no silent default), so
 * direct component tests must supply this explicitly.
 */
export function ChatDisplayTestProvider({
  children,
  preset = "full",
  overrides,
  disclosure,
}: {
  children: ReactNode;
  preset?: ChatVerbosityPreset;
  overrides?: Partial<ChatVerbosityConfig["overrides"]>;
  disclosure?: ChatDisclosureStore;
}) {
  const value = useMemo(
    () => makeTestChatDisplayValue(preset, overrides, disclosure),
    [preset, overrides, disclosure],
  );
  return <ChatDisplayContext.Provider value={value}>{children}</ChatDisplayContext.Provider>;
}

/** Convenience for two-region tests that only need a default Full context. */
export const FULL_CHAT_DISPLAY_OVERRIDES: Record<keyof ChatVerbosityConfig["overrides"], ChatDisplayOverride> =
  DEFAULT_CHAT_VERBOSITY_CONFIG.overrides;
