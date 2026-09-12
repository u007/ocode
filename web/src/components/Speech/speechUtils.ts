export function sanitizeSpeechText(text: string) {
  return text
    .replace(/\u001b\][^\u0007]*(?:\u0007|\u001b\\)/g, "")
    .replace(/\u001b(?:\[[0-?]*[ -/]*[@-~]|[@-_])/g, "")
    .replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/g, "")
    .trim();
}

/**
 * Computes the next playback mode in the two-state cycle:
 * manual ↔ at-bottom. Any unknown or legacy value (e.g. "auto")
 * normalizes to "at-bottom" on click, never sending "auto" to the server
 * which does not accept it.
 */
export function nextSpeechMode(current: string | undefined): "manual" | "at-bottom" {
  return current === "at-bottom" ? "manual" : "at-bottom";
}

export function chunkSpeechText(text: string, maxLength = 240) {
  const normalized = sanitizeSpeechText(text);
  if (!normalized || maxLength <= 0) return [];
  const chunks: string[] = [];
  let remaining = normalized;
  while (remaining.length > maxLength) {
    let cut = remaining.lastIndexOf(" ", maxLength);
    if (cut < Math.floor(maxLength / 2)) cut = maxLength;
    chunks.push(remaining.slice(0, cut).trim());
    remaining = remaining.slice(cut).trim();
  }
  if (remaining) chunks.push(remaining);
  return chunks;
}
