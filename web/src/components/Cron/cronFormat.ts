import type { CronSchedule } from "@/api/types";

function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "0s";
  const parts: string[] = [];
  const units: Array<[string, number]> = [
    ["d", 24 * 60 * 60 * 1000],
    ["h", 60 * 60 * 1000],
    ["m", 60 * 1000],
    ["s", 1000],
  ];
  let remaining = Math.floor(ms);
  for (const [suffix, unit] of units) {
    if (remaining < unit && parts.length === 0 && suffix !== "s") continue;
    const value = Math.floor(remaining / unit);
    if (value > 0 || parts.length > 0 || suffix === "s") {
      parts.push(`${value}${suffix}`);
      remaining -= value * unit;
    }
  }
  return parts.join(" ");
}

export function describeSchedule(schedule: CronSchedule): string {
  switch (schedule.kind) {
    case "at":
      return schedule.at_ms ? `Once at ${new Date(schedule.at_ms).toLocaleString()}` : "Once";
    case "every":
      return schedule.every_ms ? `Every ${formatDuration(schedule.every_ms)}` : "Every";
    case "cron":
      return schedule.expr ? `Cron ${schedule.expr}${schedule.tz ? ` (${schedule.tz})` : ""}` : "Cron";
    default:
      return schedule.kind;
  }
}

export function nextRunLabel(ms?: number): string {
  if (!ms) return "—";
  return new Date(ms).toLocaleString();
}

export function lastRunLabel(ms?: number): string {
  if (!ms) return "—";
  return new Date(ms).toLocaleString();
}

export function datetimeLocalFromMs(ms?: number): string {
  if (!ms) return "";
  const date = new Date(ms);
  const offset = date.getTimezoneOffset();
  return new Date(date.getTime() - offset * 60_000).toISOString().slice(0, 16);
}

export function msFromDatetimeLocal(value: string): number {
  return value ? new Date(value).getTime() : 0;
}
