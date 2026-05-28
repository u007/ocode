import { useCallback, useRef } from "react";
import { connectChatSSE, type SSEEventHandler } from "../api/client";

export function useSSE() {
  const cleanupRef = useRef<(() => void) | null>(null);

  const send = useCallback(
    (message: string, session: string | undefined, onEvent: SSEEventHandler) => {
      cleanupRef.current?.();
      cleanupRef.current = connectChatSSE(message, session, onEvent);
    },
    [],
  );

  const close = useCallback(() => {
    cleanupRef.current?.();
    cleanupRef.current = null;
  }, []);

  return { send, close };
}
