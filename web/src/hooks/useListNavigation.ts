import {
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type FocusEvent as ReactFocusEvent,
  type RefObject,
} from "react";
import {
  resolveListNavigation,
  type ListNavigationAction,
  type ListNavigationDirection,
} from "../lib/listNavigation";

export interface ListNavigationOptions {
  /** Stable, caller-owned identities in rendered row order. */
  itemIds: readonly string[];
  onActivate: (index: number) => void;
  onToggle?: (index: number) => void;
  multiple?: boolean;
  inputRef?: RefObject<HTMLElement | null>;
  returnFocusRef?: RefObject<HTMLElement | null>;
  onReturnFocus?: () => void;
  resetKey?: string | number;
  initialActiveId?: string | null;
  hasMore?: boolean;
  onReachEnd?: () => void;
}

export interface ListNavigationItemProps {
  ref: (element: HTMLElement | null) => void;
  tabIndex: number;
  "data-list-nav-row": true;
  "data-list-nav-id": string;
  "data-list-nav-active": boolean | undefined;
  onFocus: (event: ReactFocusEvent<HTMLElement>) => void;
}

export interface ListNavigation {
  getItemProps: (index: number) => ListNavigationItemProps;
  isActive: (index: number) => boolean;
  onKeyDown: (event: ReactKeyboardEvent<HTMLElement>) => void;
}

/**
 * Shared real-focus navigation for custom list popups. The pure movement rules
 * live in lib/listNavigation; this hook only owns DOM focus and row identity.
 */
export function useListNavigation({
  itemIds,
  onActivate,
  onToggle,
  multiple = false,
  inputRef,
  returnFocusRef,
  onReturnFocus,
  resetKey,
  initialActiveId,
  hasMore = false,
  onReachEnd,
}: ListNavigationOptions): ListNavigation {
  const itemIdsRef = useRef<readonly string[]>(itemIds);
  itemIdsRef.current = itemIds;
  const itemIdsKey = itemIds.join("\u0000");
  const rowRefs = useRef(new Map<string, HTMLElement>());
  const activeIdRef = useRef<string | null>(null);
  const activeIndexRef = useRef(-1);
  const pendingIndexRef = useRef<number | null>(null);
  const resetKeyRef = useRef(resetKey);
  const didMountRef = useRef(false);
  const [activeId, setActiveId] = useState<string | null>(null);

  const activateRef = useRef(onActivate);
  activateRef.current = onActivate;
  const toggleRef = useRef(onToggle);
  toggleRef.current = onToggle;
  const reachEndRef = useRef(onReachEnd);
  reachEndRef.current = onReachEnd;
  const returnFocusCallbackRef = useRef(onReturnFocus);
  returnFocusCallbackRef.current = onReturnFocus;

  const setActive = useCallback((id: string | null, index = id ? itemIdsRef.current.indexOf(id) : -1) => {
    activeIdRef.current = id;
    activeIndexRef.current = id ? index : -1;
    setActiveId(id);
  }, []);

  const focusRow = useCallback((id: string) => {
    const row = rowRefs.current.get(id);
    if (!row) return false;
    row.focus({ preventScroll: true });
    row.scrollIntoView({ block: "nearest" });
    return true;
  }, []);

  const focusInput = useCallback(() => {
    const input = inputRef?.current ?? returnFocusRef?.current;
    if (!input) return false;
    input.focus({ preventScroll: true });
    return true;
  }, [inputRef, returnFocusRef]);

  useLayoutEffect(() => {
    const ids = itemIdsRef.current;
    const activeElement = document.activeElement;
    const inputHasFocus = inputRef?.current != null && activeElement === inputRef.current;
    const activeRow = activeElement instanceof Element
      ? activeElement.closest<HTMLElement>("[data-list-nav-row]")
      : null;
    const shouldRestoreRowFocus =
      !inputHasFocus && (activeRow === activeElement || activeElement === document.body);
    const resetChanged = !didMountRef.current || resetKeyRef.current !== resetKey;
    didMountRef.current = true;
    if (resetChanged) {
      resetKeyRef.current = resetKey;
      pendingIndexRef.current = null;
      const next = initialActiveId && ids.includes(initialActiveId) ? initialActiveId : null;
      const nextIndex = next ? ids.indexOf(next) : -1;
      setActive(next, nextIndex);
      if (next && !inputHasFocus) focusRow(next);
      return;
    }

    const pendingIndex = pendingIndexRef.current;
    if (pendingIndex !== null) {
      const next = ids[pendingIndex];
      if (next !== undefined) {
        pendingIndexRef.current = null;
        setActive(next, pendingIndex);
        if (!inputHasFocus) focusRow(next);
        return;
      }
    }

    const current = activeIdRef.current;
    if (current && !ids.includes(current)) {
      const oldIndex = activeIndexRef.current < 0 ? 0 : activeIndexRef.current;
      const nextIndex = Math.max(0, Math.min(ids.length - 1, oldIndex));
      const next = ids[nextIndex];
      setActive(next ?? null, nextIndex);
      if (next && shouldRestoreRowFocus) focusRow(next);
      return;
    }
    if (current) {
      activeIndexRef.current = ids.indexOf(current);
      if (shouldRestoreRowFocus) focusRow(current);
    }
  }, [activeId, focusRow, initialActiveId, inputRef, itemIdsKey, resetKey, setActive]);

  const getItemProps = useCallback((index: number): ListNavigationItemProps => {
    const ids = itemIdsRef.current;
    const id = ids[index] ?? String(index);
    const active = activeId === id;
    const firstTabStop = activeId === null ? index === 0 : active;
    return {
      ref: (element) => {
        if (element) rowRefs.current.set(id, element);
        else rowRefs.current.delete(id);
      },
      tabIndex: firstTabStop ? 0 : -1,
      "data-list-nav-row": true,
      "data-list-nav-id": id,
      "data-list-nav-active": active || undefined,
      onFocus: (event) => {
        // Ignore focus events bubbling from nested controls inside a row.
        if (event.target === event.currentTarget && activeId !== id) {
          setActive(id, index);
        }
      },
    };
  }, [activeId, setActive]);

  const isActive = useCallback(
    (index: number) => activeId === itemIdsRef.current[index],
    [activeId],
  );

  const applyAction = useCallback((action: ListNavigationAction) => {
    if (action.type === "focus") {
      const id = itemIdsRef.current[action.index];
      if (id) {
        setActive(id, action.index);
        focusRow(id);
      }
      return;
    }
    if (action.type === "load-more") {
      if (pendingIndexRef.current === null && reachEndRef.current) {
        pendingIndexRef.current = action.nextIndex;
        reachEndRef.current();
      }
      return;
    }
    if (action.type === "return-to-input") {
      setActive(null);
      focusInput();
      returnFocusCallbackRef.current?.();
    }
  }, [focusInput, focusRow, setActive]);

  const onKeyDown = useCallback((event: ReactKeyboardEvent<HTMLElement>) => {
    if (event.defaultPrevented) return;
    const target = event.target;
    if (!(target instanceof Element)) return;

    // Only the designated search input may bridge into the list. Nested
    // controls intentionally have no data-list-nav-row marker.
    const row = target.closest<HTMLElement>("[data-list-nav-row]");
    const inputIsTarget = inputRef?.current === target;
    if (!row && !inputIsTarget) return;

    const ids = itemIdsRef.current;
    if (ids.length === 0) return;
    const rowId = row?.dataset.listNavId;
    const currentIndex = rowId
      ? ids.indexOf(rowId)
      : inputIsTarget
        ? -1
        : activeIdRef.current
          ? ids.indexOf(activeIdRef.current)
          : -1;

    const direction = keyDirection(event.key);
    if (inputIsTarget && !row && event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    if (direction) {
      event.preventDefault();
      event.stopPropagation();
      applyAction(
        resolveListNavigation({
          current: currentIndex,
          count: ids.length,
          direction,
          hasMore,
          hasInput: inputRef?.current != null || returnFocusRef?.current != null,
        }),
      );
      return;
    }

    if (!row || (event.key !== "Enter" && event.key !== " ")) return;
    event.preventDefault();
    event.stopPropagation();
    if (multiple) {
      (toggleRef.current ?? activateRef.current)(currentIndex);
    } else {
      activateRef.current(currentIndex);
    }
  }, [applyAction, hasMore, inputRef, multiple, returnFocusRef]);

  return { getItemProps, isActive, onKeyDown };
}

function keyDirection(key: string): ListNavigationDirection | null {
  switch (key) {
    case "ArrowDown":
      return "next";
    case "ArrowUp":
      return "previous";
    case "Home":
      return "first";
    case "End":
      return "last";
    default:
      return null;
  }
}
