import { describe, it, expect } from "vitest";
import { renderHook, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { BrowserTabsProvider, useBrowserTabs } from "./browserTabsStore";

function wrap({ children }: { children: ReactNode }) {
  return <BrowserTabsProvider>{children}</BrowserTabsProvider>;
}

describe("browserTabsStore", () => {
  it("opens, lists, renames, and closes browser tabs per project", () => {
    const { result } = renderHook(() => useBrowserTabs("/proj/a"), { wrapper: wrap });

    let id = "";
    act(() => { id = result.current.openBrowserTab(); });
    expect(result.current.tabs).toHaveLength(1);
    expect(result.current.activeId).toBe(id);
    expect(result.current.tabs[0].title).toBe("New tab");

    act(() => { result.current.renameBrowserTab(id, "example.com"); });
    expect(result.current.tabs[0].title).toBe("example.com");

    act(() => { result.current.closeBrowserTab(id); });
    expect(result.current.tabs).toHaveLength(0);
    expect(result.current.activeId).toBeNull();
  });

  it("isolates tabs by project path", () => {
    const a = renderHook(() => useBrowserTabs("/proj/a"), { wrapper: wrap });
    act(() => { a.result.current.openBrowserTab(); });
    const b = renderHook(() => useBrowserTabs("/proj/b"), { wrapper: wrap });
    expect(b.result.current.tabs).toHaveLength(0);
  });
});
