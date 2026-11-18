import { describe, it, expect, vi, beforeAll } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import EditorTabBar from "./EditorTabBar";

const TABS = [{ id: "a", path: "/proj/src/app.ts", projectRoot: "/proj" }];

function renderBar(tab: (typeof TABS)[number] & { includeInContext?: boolean }) {
  const onToggleInclude = vi.fn();
  render(
    <EditorTabBar
      editorTabs={[tab]}
      activeEditorTabId={tab.id}
      onSelectTab={vi.fn()}
      onCloseTab={vi.fn()}
      onToggleInclude={onToggleInclude}
    />,
  );
  return { onToggleInclude };
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

describe("EditorTabBar chat-context checkbox", () => {
  it("shows an opened file as excluded from chat context by default", () => {
    renderBar(TABS[0]);
    expect(
      screen.getByRole("checkbox", { name: /excluded from chat context/i }),
    ).toHaveAttribute("aria-checked", "false");
  });

  it("asks the owner to include the file when the user checks it", () => {
    const { onToggleInclude } = renderBar({ ...TABS[0], includeInContext: false });
    fireEvent.click(screen.getByRole("checkbox", { name: /excluded from chat context/i }));
    expect(onToggleInclude).toHaveBeenCalledWith("a");
  });

  it("shows an opted-in file as checked", () => {
    renderBar({ ...TABS[0], includeInContext: true });
    expect(
      screen.getByRole("checkbox", { name: /included in chat context/i }),
    ).toHaveAttribute("aria-checked", "true");
  });
});
