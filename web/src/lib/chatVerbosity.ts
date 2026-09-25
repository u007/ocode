import { useEffect, useState } from "react";
import { api } from "@/api/client";
import { eventBus } from "@/lib/eventBus";
import type {
  ChatDisplayOverride,
  ChatDisplayPolicy,
  ChatVerbosityConfig,
  ChatVerbosityPreset,
  ChatVerbosityResponse,
} from "@/api/types";

export const CHAT_VERBOSITY_PRESETS: readonly ChatVerbosityPreset[] = ["full", "balanced", "quiet"];
export const CHAT_DISPLAY_OVERRIDES: readonly ChatDisplayOverride[] = ["preset", "expanded", "collapsed"];

export const DEFAULT_CHAT_VERBOSITY_CONFIG: ChatVerbosityConfig = {
  preset: "full",
  overrides: {
    older_thinking: "preset",
    tool_calls: "preset",
    tool_output: "preset",
    activity_notices: "preset",
  },
};

export interface ChatVerbosityState {
  config: ChatVerbosityConfig;
  policy: ChatDisplayPolicy;
  revision: string;
  loading: boolean;
  error: string | null;
}

type ConfigInput = Partial<Omit<ChatVerbosityConfig, "overrides">> & {
  overrides?: Partial<ChatVerbosityConfig["overrides"]>;
};

type Listener = (state: ChatVerbosityState) => void;

let cached: ChatVerbosityState | null = null;
let inFlight: Promise<ChatVerbosityState> | null = null;
const listeners = new Set<Listener>();

function isPreset(value: unknown): value is ChatVerbosityPreset {
  return value === "full" || value === "balanced" || value === "quiet";
}

function isOverride(value: unknown): value is ChatDisplayOverride {
  return value === "preset" || value === "expanded" || value === "collapsed";
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function stateFromConfig(config: ChatVerbosityConfig, error: string | null = null, loading = false): ChatVerbosityState {
  const normalized = normalizeChatVerbosityConfig(config);
  const policy = resolveChatDisplayPolicy(normalized);
  return {
    config: normalized,
    policy,
    revision: chatPolicyRevision(policy),
    loading,
    error,
  };
}

function publish(next: ChatVerbosityState): ChatVerbosityState {
  cached = next;
  for (const listener of listeners) listener(next);
  return next;
}

function defaultState(error: string | null = null, loading = false): ChatVerbosityState {
  return stateFromConfig(DEFAULT_CHAT_VERBOSITY_CONFIG, error, loading);
}

/** Fill omitted fields while preserving unknown values for validation to reject. */
export function normalizeChatVerbosityConfig(input?: ConfigInput | null): ChatVerbosityConfig {
  const overrides = input?.overrides ?? {};
  return {
    preset: (input?.preset || "full") as ChatVerbosityPreset,
    overrides: {
      older_thinking: (overrides.older_thinking || "preset") as ChatDisplayOverride,
      tool_calls: (overrides.tool_calls || "preset") as ChatDisplayOverride,
      tool_output: (overrides.tool_output || "preset") as ChatDisplayOverride,
      activity_notices: (overrides.activity_notices || "preset") as ChatDisplayOverride,
    },
  };
}

function assertValidConfig(config: ChatVerbosityConfig): void {
  if (!isPreset(config.preset)) {
    throw new Error(`Invalid chat verbosity preset: ${String(config.preset)}`);
  }
  for (const [name, value] of Object.entries(config.overrides)) {
    if (!isOverride(value)) {
      throw new Error(`Invalid chat display override for ${name}: ${String(value)}`);
    }
  }
}

/** Resolve persisted intent into the renderer policy. Latest thinking is always expanded. */
export function resolveChatDisplayPolicy(input?: ConfigInput | null): ChatDisplayPolicy {
  const config = normalizeChatVerbosityConfig(input);
  assertValidConfig(config);

  let olderThinking: ChatDisplayOverride = "expanded";
  let toolCalls: ChatDisplayOverride = "expanded";
  let toolOutput: ChatDisplayOverride = "expanded";
  let notices: ChatDisplayOverride = "expanded";
  if (config.preset === "balanced") {
    olderThinking = "collapsed";
  } else if (config.preset === "quiet") {
    olderThinking = "collapsed";
    toolCalls = "collapsed";
    toolOutput = "collapsed";
    notices = "collapsed";
  }

  const resolveOverride = (
    value: ChatDisplayOverride,
    presetValue: "expanded" | "collapsed",
  ): "expanded" | "collapsed" => (value === "preset" ? presetValue : value);

  return {
    older_thinking: resolveOverride(config.overrides.older_thinking, olderThinking),
    latest_thinking: "expanded",
    tool_calls: resolveOverride(config.overrides.tool_calls, toolCalls),
    tool_output: resolveOverride(config.overrides.tool_output, toolOutput),
    notices: resolveOverride(config.overrides.activity_notices, notices),
    status: "expanded",
  };
}

/** Stable key used to rebase disclosure state only when effective modes change. */
export function chatPolicyRevision(policy: ChatDisplayPolicy): string {
  return [
    policy.older_thinking,
    policy.latest_thinking,
    policy.tool_calls,
    policy.tool_output,
    policy.notices,
    policy.status,
  ].join("|");
}

function stateFromResponse(response: ChatVerbosityResponse): ChatVerbosityState {
  return stateFromConfig(response);
}

/** Refresh from the local server. Event payloads are intentionally ignored. */
export async function refreshChatVerbosity(): Promise<ChatVerbosityState> {
  if (inFlight) return inFlight;
  inFlight = api
    .getChatVerbosityConfig()
    .then((response) => publish(stateFromResponse(response)))
    .catch((error: unknown) => {
      const message = errorMessage(error);
      if (cached) {
        console.error("chat verbosity config request failed; retaining cached policy", error);
        return publish({ ...cached, loading: false, error: message });
      }
      console.error("chat verbosity config request failed; using Full compatibility default", {
        error,
        reason: "no-cached-policy",
      });
      return publish(defaultState(message));
    })
    .finally(() => {
      inFlight = null;
    });
  return inFlight;
}

/** Persist a new policy and publish the returned server state to all consumers. */
export async function saveChatVerbosityConfig(config: ChatVerbosityConfig): Promise<ChatVerbosityState> {
  const response = await api.setChatVerbosityConfig(normalizeChatVerbosityConfig(config));
  return publish(stateFromResponse(response));
}

export function useChatVerbosity(): ChatVerbosityState {
  const [state, setState] = useState<ChatVerbosityState>(() => cached ?? defaultState(null, true));
  const [error, setError] = useState<string | null>(state.error);

  useEffect(() => {
    const listener: Listener = (next) => {
      setState(next);
      setError(next.error);
    };
    listeners.add(listener);

    const offChanged = eventBus.on("chat_verbosity_changed", () => {
      void refreshChatVerbosity();
    });
    const offReconnect = eventBus.onReconnect(() => {
      void refreshChatVerbosity();
    });

    if (cached === null) void refreshChatVerbosity();

    return () => {
      listeners.delete(listener);
      offChanged();
      offReconnect();
    };
  }, []);

  return error === state.error ? state : { ...state, error };
}

export function __resetChatVerbosityForTests(): void {
  cached = null;
  inFlight = null;
  listeners.clear();
}
