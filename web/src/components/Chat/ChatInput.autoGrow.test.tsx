import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
import ChatInput from "./ChatInput";

// Controllable stand-in for the real useChat hook (mirrors ChatInput.test.tsx).
vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage: vi.fn().mockResolvedValue(true),
    executeShell: vi.fn().mockResolvedValue({ output: "", exitCode: 0, error: "" }),
    stop: vi.fn(),
    isStreaming: false,
    pendingPermission: null,
  }),
}));

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: { activeProject: { path: "/tmp/proj" } },
    dispatch: vi.fn(),
  }),
}));

function getTextarea(): HTMLTextAreaElement {
  return screen.getByPlaceholderText(/Type a message/i) as HTMLTextAreaElement;
}

/**
 * jsdom has no layout engine, so `scrollHeight` is always 0. Model the browser:
 * the content height when the box is auto-sized, and at least the current
 * explicit box height otherwise (which is why the fit has to reset to `auto`
 * before re-measuring, or a shrink would never take effect).
 */
function modelScrollHeight(el: HTMLTextAreaElement, content: () => number) {
  Object.defineProperty(el, "scrollHeight", {
    configurable: true,
    get: () => {
      const explicit = parseInt(el.style.height, 10);
      if (!el.style.height || el.style.height === "auto" || Number.isNaN(explicit)) {
        return content();
      }
      return Math.max(explicit, content());
    },
  });
}

describe("ChatInput auto-grow", () => {
  it("is a single 1.5em line by default (no fixed 2-row height)", () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();
    expect(ta.getAttribute("rows")).toBe("1");
    expect(ta.className).toContain("leading-[1.5em]");
    // Grows to an exact-fit pixel height, capped, then scrolls internally.
    expect(ta.className).toContain("max-h-40");
    expect(ta.className).toContain("overflow-y-auto");
  });

  it("expands the composer to the draft's scrollHeight", () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();
    let content = 0;
    modelScrollHeight(ta, () => content);

    content = 96;
    fireEvent.change(ta, { target: { value: "line1\nline2\nline3\nline4" } });
    expect(ta.style.height).toBe("96px");
  });

  it("resets to `auto` before measuring so the box shrinks again", () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();
    let content = 0;
    modelScrollHeight(ta, () => content);

    content = 96;
    fireEvent.change(ta, { target: { value: "a\nb\nc\nd" } });
    expect(ta.style.height).toBe("96px");

    // With the explicit 96px still applied the box would report >= 96; the fit
    // must clear it first so the smaller content wins.
    content = 40;
    fireEvent.change(ta, { target: { value: "a" } });
    expect(ta.style.height).toBe("40px");
  });

  it("re-fits when the window resizes (draft rewraps)", () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();
    let content = 0;
    modelScrollHeight(ta, () => content);

    content = 120;
    act(() => {
      window.dispatchEvent(new Event("resize"));
    });
    expect(ta.style.height).toBe("120px");
  });

  it("leaves the height untouched when the element has no layout (hidden tab)", () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();
    // jsdom default: scrollHeight 0, i.e. what a `display:none` tab reports.
    fireEvent.change(ta, { target: { value: "x\ny" } });
    expect(ta.style.height).toBe("");
  });
});
