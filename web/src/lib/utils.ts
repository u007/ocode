import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

// Base filename of a path, mirroring the TUI's filepath.Base.
// Handles both POSIX and Windows separators and trailing slashes.
export function basename(p: string): string {
  if (!p) return p;
  const parts = p.replace(/\\/g, "/").split("/").filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : p;
}
