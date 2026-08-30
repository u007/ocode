// Cmd/Ctrl+W behaviour in the desktop shell (see useKeyboard docs): the
// frontmost *browser* tab closes when a browser tab is focused, otherwise the
// active session tab keeps the existing behaviour.
import { describe, it, expect, vi } from "vitest";
import { renderHook, fireEvent } from "@testing-library/react";
import { useKeyboard } from "./useKeyboard";

vi.mock("@/lib/desktopShell", () => ({ isDesktopShell: () => true }));

function fireCmdW() {
  fireEvent.keyDown(window, { key: "w", metaKey: true });
}

describe("useKeyboard Cmd/Ctrl+W with browser focus", () => {
  it("closes the active browser tab on Cmd+W when a browser tab is focused", () => {
    const onCloseBrowserTab = vi.fn();
    const onCloseSession = vi.fn();
    renderHook(() =>
      useKeyboard({
        focusedKind: "browser",
        activeBrowserId: "b1",
        onCloseBrowserTab,
        onCloseSession,
      }),
    );
    fireCmdW();
    expect(onCloseBrowserTab).toHaveBeenCalledWith("b1");
    expect(onCloseSession).not.toHaveBeenCalled();
  });

  it("still closes the session tab when chat is focused", () => {
    const onCloseBrowserTab = vi.fn();
    const onCloseSession = vi.fn();
    renderHook(() =>
      useKeyboard({
        focusedKind: "chat",
        activeBrowserId: "b1",
        onCloseBrowserTab,
        onCloseSession,
      }),
    );
    fireCmdW();
    expect(onCloseSession).toHaveBeenCalled();
    expect(onCloseBrowserTab).not.toHaveBeenCalled();
  });

  it("falls back to session close when browser-focused but no active id", () => {
    const onCloseBrowserTab = vi.fn();
    const onCloseSession = vi.fn();
    renderHook(() =>
      useKeyboard({
        focusedKind: "browser",
        activeBrowserId: null,
        onCloseBrowserTab,
        onCloseSession,
      }),
    );
    fireCmdW();
    expect(onCloseBrowserTab).not.toHaveBeenCalled();
    expect(onCloseSession).toHaveBeenCalled();
  });
});
