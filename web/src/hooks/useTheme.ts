import { useState, useEffect } from "react";
import { api } from "@/api/client";
import type { ThemeColors } from "@/api/types";

// Tailwind consumes these variables as HSL component triplets — see
// tailwind.config.js `hsl(var(--x))` wrappers and index.css `:root` defaults.
// Server colors arrive as hex, so they must be converted to "H S% L%" form.
function hexToHslTriplet(hex: string): string | null {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) return null;
  const n = parseInt(m[1], 16);
  const r = ((n >> 16) & 0xff) / 255;
  const g = ((n >> 8) & 0xff) / 255;
  const b = (n & 0xff) / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  let h = 0;
  let s = 0;
  if (max !== min) {
    const d = max - min;
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    if (max === r) h = (g - b) / d + (g < b ? 6 : 0);
    else if (max === g) h = (b - r) / d + 2;
    else h = (r - g) / d + 4;
    h /= 6;
  }
  const round = (v: number) => Math.round(v * 10) / 10;
  return `${round(h * 360)} ${round(s * 100)}% ${round(l * 100)}%`;
}

function applyThemeColors(colors: ThemeColors) {
  const root = document.documentElement;
  const set = (name: string, hex: string) => {
    const triplet = hexToHslTriplet(hex);
    if (triplet === null) {
      console.warn(`Skipping theme variable ${name}: invalid hex color`, hex);
      return;
    }
    root.style.setProperty(name, triplet);
  };
  set("--background", colors.background);
  set("--foreground", colors.text);
  set("--primary", colors.user);
  set("--accent", colors.accent);
  set("--border", colors.border);
  set("--destructive", colors.error);
  set("--muted", colors.dim);
  set("--muted-foreground", colors.hint);
  set("--card", colors.background);
  set("--card-foreground", colors.text);
  set("--popover", colors.background);
  set("--popover-foreground", colors.text);
  set("--secondary", colors.selected_bg);
  set("--secondary-foreground", colors.selected_fg);
  set("--ring", colors.user);
}

export function useTheme() {
  const [serverColors, setServerColors] = useState<ThemeColors | null>(null);

  // Fetch server theme colors on mount. The web UI follows the terminal
  // theme; there is no separate light/dark toggle. On fetch failure the
  // stylesheet defaults in index.css remain untouched.
  useEffect(() => {
    api
      .getTheme()
      .then((resp) => {
        setServerColors(resp.colors);
        applyThemeColors(resp.colors);
      })
      .catch((err) => {
        console.warn("Failed to fetch server theme, keeping defaults:", err);
      });
  }, []);

  return { serverColors };
}
