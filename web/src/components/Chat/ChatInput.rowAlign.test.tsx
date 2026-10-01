import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import ChatInput from "./ChatInput";

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
  findTabForSession: () => undefined,
  useProjectState: () => ({
    state: { activeProject: { path: "/tmp/proj" } },
    dispatch: vi.fn(),
  }),
}));

function getTextarea(): HTMLTextAreaElement {
  return screen.getByPlaceholderText(/Type a message/i) as HTMLTextAreaElement;
}

/** jsdom has no layout engine, so `scrollHeight` is always 0 and the fit bails
 *  out early. Model the browser: report the content height while the box is
 *  auto-sized, and at least the current explicit box height otherwise. */
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

/**
 * The height of the box that is neither content nor padding — the borders (plus
 * any horizontal scrollbar). `offsetHeight - clientHeight` in a real browser.
 */
function modelChrome(el: HTMLTextAreaElement, chrome: number) {
  Object.defineProperty(el, "offsetHeight", { configurable: true, get: () => chrome });
  Object.defineProperty(el, "clientHeight", { configurable: true, get: () => 0 });
}

describe("ChatInput input-row alignment", () => {
  it("gives every control in the row the same composer-control height", () => {
    render(<ChatInput sessionTabId="new-1" />);

    // The textarea, the attach button and the action button must all resolve to
    // one shared height, or the row renders ragged (a short bottom-aligned
    // action button next to the taller input box).
    const attach = screen.getByRole("button", { name: /attach files/i });
    const send = screen.getByRole("button", { name: "Send" });
    for (const el of [getTextarea(), attach, send]) {
      expect(el.className).toContain("composer-control");
    }
    // And they share the input's radius, so the row reads as one unit.
    expect(send.className).toContain("rounded-lg");
    expect(attach.className).toContain("rounded-lg");
  });

  it("pins the controls to the textarea's bottom edge as it grows", () => {
    render(<ChatInput sessionTabId="new-1" />);
    // `items-end` (not `items-center`) so a growing composer keeps the controls
    // on its baseline instead of drifting to the vertical middle.
    const row = getTextarea().parentElement as HTMLElement;
    expect(row.className).toContain("items-end");
    expect(row.className).not.toContain("items-center");
    // The textarea still owns the growth; the buttons must not stretch with it.
    expect(getTextarea().className).toContain("max-h-40");
    expect(screen.getByRole("button", { name: "Send" }).className).toContain("shrink-0");
  });

  it("adds the border to the auto-fit height so an empty box never scrolls", () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();
    let content = 0;
    modelScrollHeight(ta, () => content);
    // 1px border top + bottom.
    modelChrome(ta, 2);

    content = 45;
    fireEvent.change(ta, { target: { value: "one line" } });
    // `scrollHeight` (content + padding) excludes the border, but `height` is
    // border-box — so applying it raw leaves the box 2px short of its content,
    // which pins a scrollbar track on an EMPTY composer.
    expect(ta.style.height).toBe("47px");
  });

  it("keeps an empty composer at ONE line instead of measuring the placeholder", () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();
    let content = 0;
    modelScrollHeight(ta, () => content);
    modelChrome(ta, 2);

    // Chrome counts the PLACEHOLDER in `scrollHeight`, and this composer's
    // placeholder is 110 characters, so on a narrow viewport it wrapped and
    // inflated the EMPTY box to 3-5 lines (measured 110px at a 320px viewport
    // vs 47px at 1400px). An empty draft must never be measured.
    content = 131;
    fireEvent.change(ta, { target: { value: "" } });
    expect(ta.style.height).toBe("");

    // ...and clearing a tall draft must collapse it back to one line, not leave
    // the last measured (multi-line) height stuck on the element.
    content = 131;
    fireEvent.change(ta, { target: { value: "a\nb\nc\nd\ne" } });
    expect(ta.style.height).toBe("133px");
    content = 131;
    fireEvent.change(ta, { target: { value: "" } });
    expect(ta.style.height).toBe("");
  });

  it("keeps the placeholder short and the shortcut list on the title", () => {
    render(<ChatInput sessionTabId="new-1" />);
    const ta = getTextarea();
    // jsdom cannot observe a WRAPPED placeholder (no layout engine), so pin the
    // invariant at its source instead: Chrome counts placeholder text in
    // `scrollHeight`, so a placeholder carrying the shortcut list re-inflates the
    // empty composer at narrow widths and paints a scrollbar on it. The list
    // must live on `title`, where it cannot own the box's height.
    expect(ta.getAttribute("placeholder")).toBe("Type a message…");
    expect(ta.getAttribute("placeholder")).not.toMatch(/Shift\+Enter/);
    const title = ta.getAttribute("title") ?? "";
    for (const hint of ["Enter to send", "Shift+Enter", "history", "/ for commands", "! for shell"]) {
      expect(title).toContain(hint);
    }
    // Sanity: short enough to stay on one line even at a 320px viewport.
    expect((ta.getAttribute("placeholder") ?? "").length).toBeLessThan(30);
  });
});