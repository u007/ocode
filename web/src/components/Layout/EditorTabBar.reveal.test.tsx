import { describe, it, expect, vi, beforeAll } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import EditorTabBar from "./EditorTabBar";

const TABS = [
  { id: "a", path: "/proj/src/app/deep.ts", projectRoot: "/proj" },
  { id: "b", path: "/proj/docs/readme.pdf", projectRoot: "/proj" },
];

function renderBar(props: Partial<React.ComponentProps<typeof EditorTabBar>> = {}) {
  const onRevealInTree = vi.fn();
  const onSelectTab = vi.fn();
  render(
    <EditorTabBar
      editorTabs={TABS}
      activeEditorTabId="a"
      onSelectTab={onSelectTab}
      onCloseTab={vi.fn()}
      onRevealInTree={onRevealInTree}
      {...props}
    />,
  );
  return { onRevealInTree, onSelectTab };
}

beforeAll(() => {
  if (!(globalThis as any).PointerEvent) (globalThis as any).PointerEvent = MouseEvent;
  if (!(globalThis as any).ResizeObserver) {
    (globalThis as any).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

describe("EditorTabBar reveal in file tree", () => {
  it("offers 'Show in file tree' on right-click and hands over that tab's path and root", async () => {
    const { onRevealInTree } = renderBar();

    fireEvent.contextMenu(screen.getByText("readme.pdf"));
    const item = await screen.findByRole("menuitem", { name: "Show in file tree" });
    fireEvent.click(item);

    expect(onRevealInTree).toHaveBeenCalledTimes(1);
    // The whole tab object travels, so the tree knows which root the path is
    // relative to (a tab opened from an extra allowed path, not the project).
    expect(onRevealInTree).toHaveBeenCalledWith(TABS[1]);
  });

  it("reveals from the preview tab as well as the editor tab", async () => {
    const { onRevealInTree } = renderBar();

    fireEvent.contextMenu(screen.getByText("deep.ts"));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Show in file tree" }));

    expect(onRevealInTree).toHaveBeenCalledWith(TABS[0]);
  });

  it("makes the right-clicked tab active before revealing it", async () => {
    const { onSelectTab } = renderBar({ activeEditorTabId: "a" });

    fireEvent.contextMenu(screen.getByText("readme.pdf"));
    await screen.findByRole("menuitem", { name: "Show in file tree" });

    expect(onSelectTab).toHaveBeenCalledWith("b");
  });

  it("keeps left-click selection working", () => {
    const { onSelectTab, onRevealInTree } = renderBar();

    fireEvent.click(screen.getByText("readme.pdf"));

    expect(onSelectTab).toHaveBeenCalledWith("b");
    expect(onRevealInTree).not.toHaveBeenCalled();
  });

  it("hides the action when the tree cannot be driven", async () => {
    renderBar({ onRevealInTree: undefined });

    fireEvent.contextMenu(screen.getByText("deep.ts"));

    // The context menu still opens on the tab; it just has nothing to offer.
    await screen.findByText("deep.ts");
    expect(screen.queryByRole("menuitem", { name: "Show in file tree" })).toBeNull();
  });
});