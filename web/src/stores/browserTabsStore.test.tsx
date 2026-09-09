import { describe, it, expect } from "vitest";
import { renderHook, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { BrowserTabsProvider, useAllBrowserTabs, useBrowserTabs } from "./browserTabsStore";

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

  it("enumerates tabs across projects without changing their active pointers", () => {
    const { result } = renderHook(() => {
      const a = useBrowserTabs("/proj/a");
      const b = useBrowserTabs("/proj/b");
      return { a, b, all: useAllBrowserTabs() };
    }, { wrapper: wrap });

    let aId = "";
    let bId = "";
    act(() => {
      aId = result.current.a.openBrowserTab();
      bId = result.current.b.openBrowserTab();
    });

    expect(result.current.all.map(({ projectPath, tab }) => `${projectPath}:${tab.id}`)).toEqual([
      `/proj/a:${aId}`,
      `/proj/b:${bId}`,
    ]);
    expect(result.current.a.activeId).toBe(aId);
    expect(result.current.b.activeId).toBe(bId);
  });
});

describe("manual renames vs page titles", () => {
  it("rename marks the title manual; clearManualTitle releases it", () => {
    const { result } = renderHook(() => useBrowserTabs("/proj/manual"), { wrapper: wrap });
    let id = "";
    act(() => { id = result.current.openBrowserTab(); });
    expect(result.current.tabs[0].manualTitle).toBeNull();
    act(() => { result.current.renameBrowserTab(id, "My Label"); });
    expect(result.current.tabs[0].title).toBe("My Label");
    expect(result.current.tabs[0].manualTitle).toBe("My Label");
    act(() => { result.current.clearManualTitle(id); });
    expect(result.current.tabs[0].manualTitle).toBeNull();
    expect(result.current.tabs[0].title).toBe("My Label");
  });

  it("restore rehydrates tabs with manual flags and active pointer", () => {
    const { result } = renderHook(() => useBrowserTabs("/proj/restore"), { wrapper: wrap });
    act(() => {
      result.current.restoreBrowserTabs(
        [
          { id: "b1", title: "One", manualTitle: null },
          { id: "b2", title: "Mine", manualTitle: "Mine" },
        ],
        "b2",
      );
    });
    expect(result.current.tabs).toHaveLength(2);
    expect(result.current.activeId).toBe("b2");
    expect(result.current.tabs[1].manualTitle).toBe("Mine");
  });
});
