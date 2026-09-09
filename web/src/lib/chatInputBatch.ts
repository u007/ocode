export const CHAT_INPUT_DEBOUNCE_MS = 1500;

export function joinChatInputBatch(inputs: string[]): string {
  return inputs.join("\n\n");
}
