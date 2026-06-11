import { useState, useEffect } from "react";
import { api } from "@/api/client";
import type { ThemeColors } from "@/api/types";

type Theme = "dark" | "light";

// Default colors (tokyonight)
const DEFAULT_COLORS: ThemeColors = {
  user: "#7aa2f7",
  assistant: "#bb9af7",
  header: "#7dcfff",
  border: "#3b4261",
  hint: "#565f89",
  text: "#c0caf5",
  background: "#1a1b26",
  status_bg: "#1a1b26",
  status_fg: "#787c99",
  selected_fg: "#1a1b26",
  selected_bg: "#7aa2f7",
  success: "#9ece6a",
  error: "#f7768e",
  accent: "#7dcfff",
  dim: "#3b4261",
  thinking: "#bb9af7",
};

function applyThemeColors(colors: ThemeColors) {
  const root = document.documentElement;
  root.style.setProperty("--background", colors.background);
  root.style.setProperty("--foreground", colors.text);
  root.style.setProperty("--primary", colors.user);
  root.style.setProperty("--accent", colors.accent);
  root.style.setProperty("--border", colors.border);
  root.style.setProperty("--destructive", colors.error);
  root.style.setProperty("--muted", colors.dim);
  root.style.setProperty("--muted-foreground", colors.hint);
  root.style.setProperty("--card", colors.background);
  root.style.setProperty("--card-foreground", colors.text);
  root.style.setProperty("--popover", colors.background);
  root.style.setProperty("--popover-foreground", colors.text);
  root.style.setProperty("--secondary", colors.selected_bg);
  root.style.setProperty("--secondary-foreground", colors.selected_fg);
  root.style.setProperty("--ring", colors.user);
}

export function useTheme() {
  const [theme, setTheme] = useState<Theme>(() => {
    if (typeof window !== "undefined") {
      return (localStorage.getItem("theme") as Theme) || "dark";
    }
    return "dark";
  });

  const [serverColors, setServerColors] = useState<ThemeColors | null>(null);

  // Apply dark/light mode class
  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    localStorage.setItem("theme", theme);
  }, [theme]);

  // Fetch server theme colors on mount
  useEffect(() => {
    api
      .getTheme()
      .then((resp) => {
        setServerColors(resp.colors);
        applyThemeColors(resp.colors);
      })
      .catch((err) => {
        console.warn("Failed to fetch server theme, using defaults:", err);
        applyThemeColors(DEFAULT_COLORS);
      });
  }, []);

  const toggle = () => setTheme((t) => (t === "dark" ? "light" : "dark"));

  return { theme, toggle, serverColors };
}
