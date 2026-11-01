/** How full a context window is: "warn" from 70%, "bad" from 90%. */
export type GaugeLevel = "ok" | "warn" | "bad";

export function contextLevel(current: number, max: number): GaugeLevel {
  if (!(max > 0)) throw new RangeError(`context window must be a positive number of tokens, got ${max}`);
  const ratio = current / max;
  if (ratio >= 0.9) return "bad";
  if (ratio >= 0.7) return "warn";
  return "ok";
}
