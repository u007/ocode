export type ListNavigationDirection = "next" | "previous" | "first" | "last";

export type ListNavigationAction =
  | { type: "focus"; index: number }
  | { type: "load-more"; nextIndex: number }
  | { type: "return-to-input" }
  | { type: "none" };

export interface ListNavigationState {
  current: number;
  count: number;
  direction: ListNavigationDirection;
  hasMore?: boolean;
  hasInput?: boolean;
  disabled?: boolean;
}

/**
 * Resolve one keyboard navigation action without wrapping. The caller owns DOM
 * focus and rendering; this function only decides which row/action should win.
 */
export function resolveListNavigation({
  current,
  count,
  direction,
  hasMore = false,
  hasInput = false,
  disabled = false,
}: ListNavigationState): ListNavigationAction {
  if (disabled || !Number.isInteger(count) || count <= 0) return { type: "none" };
  if (direction === "first") return { type: "focus", index: 0 };
  if (direction === "last") return { type: "focus", index: count - 1 };

  if (direction === "next") {
    if (current < 0) return { type: "focus", index: 0 };
    if (current >= count - 1) {
      return hasMore ? { type: "load-more", nextIndex: count } : { type: "none" };
    }
    return { type: "focus", index: current + 1 };
  }

  if (current < 0) return { type: "focus", index: count - 1 };
  if (current <= 0) return hasInput ? { type: "return-to-input" } : { type: "none" };
  return { type: "focus", index: current - 1 };
}
