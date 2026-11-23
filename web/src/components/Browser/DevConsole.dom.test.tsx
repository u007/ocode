import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DevConsole } from "./DevConsole";
import type { DomInspectState } from "../../lib/browserStore";
import type { DomNode } from "../../lib/domInspector";

const copyTextToClipboard = vi.hoisted(() => vi.fn(async (_text: string) => true));
vi.mock("../../lib/clipboard", () => ({ copyTextToClipboard }));

// html > body > div#app.root > (button#go, "hello text")
const tree: DomNode = {
  id: "1",
  tag: "html",
  attrs: {},
  children: [
    {
      id: "2",
      tag: "body",
      attrs: {},
      children: [
        {
          id: "3",
          tag: "div",
          attrs: { id: "app", class: "root" },
          children: [
            { id: "4", tag: "button", attrs: { id: "go" } },
            { id: "5", tag: "#text", attrs: {}, text: "hello text" },
          ],
        },
      ],
    },
  ],
};

function domState(over: Partial<DomInspectState> = {}): DomInspectState {
  return {
    mode: "local",
    status: "ready",
    error: null,
    root: tree,
    truncated: false,
    selectedId: null,
    hoverId: null,
    pick: false,
    gen: 1,
    ...over,
  };
}

describe("DevConsole DOM tab", () => {
  const base = {
    consoleEvents: [{ level: "log", text: "hello world", ts: 1 }],
    networkEvents: [],
    responseBodies: {},
    perfMetrics: {},
    perfRecording: false,
    perfAvailable: true,
    onClearConsole: vi.fn(),
    onClearNetwork: vi.fn(),
    onClearPerformance: vi.fn(),
    onTogglePerfRecording: vi.fn(),
    onRequestBody: vi.fn(),
  };

  const handlers = () => ({
    onSelectNode: vi.fn(),
    onHoverNode: vi.fn(),
    onSetPick: vi.fn(),
    onRefreshDom: vi.fn(),
  });

  const openDomTab = () => {
    fireEvent.click(screen.getByLabelText("Expand console"));
    fireEvent.click(screen.getByRole("tab", { name: "DOM" }));
  };

  afterEach(() => {
    copyTextToClipboard.mockClear();
    copyTextToClipboard.mockResolvedValue(true);
    vi.useRealTimers();
  });

  it("opens on the DOM tab when a DOM inspection starts loading", () => {
    const { rerender } = render(<DevConsole {...base} dom={domState({ status: "ready" })} />);
    expect(screen.getByLabelText("Expand console")).toBeInTheDocument();

    rerender(<DevConsole {...base} dom={domState({ status: "loading", root: null, gen: 2 })} />);

    expect(screen.getByLabelText("Collapse console")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "DOM" })).toHaveAttribute("aria-selected", "true");
  });

  it("opens on the DOM tab when a node becomes selected", () => {
    const { rerender } = render(<DevConsole {...base} dom={domState({ status: "ready", selectedId: null })} />);
    expect(screen.getByLabelText("Expand console")).toBeInTheDocument();

    rerender(<DevConsole {...base} dom={domState({ status: "ready", selectedId: "4" })} />);

    expect(screen.getByLabelText("Collapse console")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "DOM" })).toHaveAttribute("aria-selected", "true");
  });

  it("does not open on mount when a node is already selected", () => {
    render(<DevConsole {...base} dom={domState({ status: "ready", selectedId: "4" })} />);
    expect(screen.getByLabelText("Expand console")).toBeInTheDocument();
  });

  it("does not force the DOM tab again after the user switches tab and collapses", () => {
    const { rerender } = render(<DevConsole {...base} dom={domState({ status: "ready", selectedId: null })} />);
    rerender(<DevConsole {...base} dom={domState({ status: "ready", selectedId: "4" })} />);
    expect(screen.getByRole("tab", { name: "DOM" })).toHaveAttribute("aria-selected", "true");

    fireEvent.click(screen.getByRole("tab", { name: "Console" }));
    fireEvent.click(screen.getByLabelText("Collapse console"));
    expect(screen.getByLabelText("Expand console")).toBeInTheDocument();

    // Same inspection, same node: no transition, so the console stays closed.
    rerender(<DevConsole {...base} dom={domState({ status: "ready", selectedId: "4", hoverId: "5" })} />);
    expect(screen.getByLabelText("Expand console")).toBeInTheDocument();
  });

  it("re-opens for a second inspection that starts while the first is still loading", () => {
    const { rerender } = render(<DevConsole {...base} dom={domState({ status: "ready" })} />);
    rerender(<DevConsole {...base} dom={domState({ status: "loading", root: null, gen: 2 })} />);
    expect(screen.getByLabelText("Collapse console")).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Collapse console"));
    expect(screen.getByLabelText("Expand console")).toBeInTheDocument();

    rerender(<DevConsole {...base} dom={domState({ status: "loading", root: null, gen: 3 })} />);

    expect(screen.getByLabelText("Collapse console")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "DOM" })).toHaveAttribute("aria-selected", "true");
  });

  it("orders the DOM tab between network and performance", () => {
    render(<DevConsole {...base} />);
    const values = screen.getAllByRole("tab").map((t) => t.textContent);
    expect(values).toEqual(["Console", "Network", "DOM", "Performance"]);
  });

  it("hints how to inspect when there is no DOM state", () => {
    render(<DevConsole {...base} />);
    openDomTab();
    expect(screen.getByText("Right-click the page and choose Inspect")).toBeInTheDocument();
    expect(screen.getByLabelText("Pick element")).toBeDisabled();
  });

  it("hints when the state is idle or the tree is missing", () => {
    const { rerender } = render(<DevConsole {...base} dom={domState({ status: "idle", root: null })} />);
    openDomTab();
    expect(screen.getByText("Right-click the page and choose Inspect")).toBeInTheDocument();
    rerender(<DevConsole {...base} dom={domState({ root: null })} />);
    expect(screen.getByText("Right-click the page and choose Inspect")).toBeInTheDocument();
  });

  it("shows loading and error states", () => {
    const { rerender } = render(<DevConsole {...base} dom={domState({ status: "loading", root: null })} />);
    openDomTab();
    expect(screen.getByText("Loading DOM…")).toBeInTheDocument();
    rerender(<DevConsole {...base} dom={domState({ status: "error", root: null, error: "inspect socket closed" })} />);
    expect(screen.getByText("inspect socket closed")).toBeInTheDocument();
  });

  it("renders the tree rows and reveals the selected node's ancestors", () => {
    render(<DevConsole {...base} {...handlers()} dom={domState({ selectedId: "4" })} />);
    openDomTab();
    expect(screen.getByText("html")).toBeInTheDocument();
    expect(screen.getByText("body")).toBeInTheDocument();
    expect(screen.getByText("div#app.root")).toBeInTheDocument();
    expect(screen.getByText("button#go")).toBeInTheDocument();
    expect(screen.getByText('"hello text"')).toBeInTheDocument();
    expect(screen.getByRole("treeitem", { name: /button#go/ })).toHaveAttribute("aria-selected", "true");
  });

  it("expands and collapses a row from its disclosure triangle", () => {
    render(<DevConsole {...base} {...handlers()} dom={domState()} />);
    openDomTab();
    expect(screen.getByText("html")).toBeInTheDocument();
    // The root starts expanded so the first view is more than one row.
    expect(screen.getByText("body")).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Collapse html"));
    expect(screen.queryByText("body")).not.toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Expand html"));
    expect(screen.getByText("body")).toBeInTheDocument();
    expect(screen.queryByText("div#app.root")).not.toBeInTheDocument();
  });

  it("selects a row on click and reports hover enter/leave", () => {
    const h = handlers();
    render(<DevConsole {...base} {...h} dom={domState({ selectedId: "3" })} />);
    openDomTab();
    const row = screen.getByRole("treeitem", { name: /div#app/ });
    fireEvent.click(row);
    expect(h.onSelectNode).toHaveBeenCalledWith("3");
    fireEvent.mouseEnter(row);
    expect(h.onHoverNode).toHaveBeenCalledWith("3");
    fireEvent.mouseLeave(row);
    expect(h.onHoverNode).toHaveBeenLastCalledWith(null);
  });

  it("toggles the element picker", () => {
    const h = handlers();
    const { rerender } = render(<DevConsole {...base} {...h} dom={domState()} />);
    openDomTab();
    fireEvent.click(screen.getByLabelText("Pick element"));
    expect(h.onSetPick).toHaveBeenCalledWith(true);
    rerender(<DevConsole {...base} {...h} dom={domState({ pick: true })} />);
    expect(screen.getByLabelText("Pick element")).toHaveAttribute("aria-pressed", "true");
  });

  it("refreshes the tree", () => {
    const h = handlers();
    render(<DevConsole {...base} {...h} dom={domState()} />);
    openDomTab();
    fireEvent.click(screen.getByLabelText("Refresh DOM"));
    expect(h.onRefreshDom).toHaveBeenCalledTimes(1);
  });

  it("copies the selected node's selector and HTML", async () => {
    render(<DevConsole {...base} {...handlers()} dom={domState({ selectedId: "3" })} />);
    openDomTab();
    await act(async () => {
      fireEvent.click(screen.getByLabelText("Copy selector"));
    });
    expect(copyTextToClipboard).toHaveBeenCalledWith("#app");
    expect(screen.getByLabelText("Copy selector").dataset.state).toBe("copied");

    copyTextToClipboard.mockClear();
    await act(async () => {
      fireEvent.click(screen.getByLabelText("Copy HTML"));
    });
    const html = copyTextToClipboard.mock.calls[0][0] as string;
    expect(html).toContain('<div id="app" class="root">');
    expect(html).toContain("<button id=\"go\"></button>");
    expect(html).toContain("hello text");
    expect(screen.getByLabelText("Copy HTML").dataset.state).toBe("copied");
  });

  it("disables the copy actions without a selection", () => {
    render(<DevConsole {...base} {...handlers()} dom={domState()} />);
    openDomTab();
    expect(screen.getByLabelText("Copy selector")).toBeDisabled();
    expect(screen.getByLabelText("Copy HTML")).toBeDisabled();
  });

  it("scrolls the selected row into view without focusing it", () => {
    // jsdom has no scrollIntoView at all; the component's optional call is what
    // keeps the reveal safe there, so the spy has to be installed for real.
    const original = HTMLElement.prototype.scrollIntoView;
    const scroll = vi.fn();
    HTMLElement.prototype.scrollIntoView = scroll;
    try {
      render(<DevConsole {...base} {...handlers()} dom={domState({ selectedId: "4" })} />);
      openDomTab();
      expect(scroll).toHaveBeenCalled();
      expect(document.activeElement).not.toBe(screen.getByRole("treeitem", { name: /button#go/ }));
    } finally {
      HTMLElement.prototype.scrollIntoView = original;
    }
  });

  it("shows the truncated notice", () => {
    render(<DevConsole {...base} {...handlers()} dom={domState({ truncated: true })} />);
    openDomTab();
    expect(screen.getByText("Tree truncated — use the picker for the rest")).toBeInTheDocument();
  });

  it("hides the text-output controls on the DOM tab only", () => {
    render(<DevConsole {...base} {...handlers()} dom={domState()} />);
    openDomTab();
    expect(screen.queryByLabelText("Filter")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Copy dom output")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Console" }));
    expect(screen.getByLabelText("Filter")).toBeInTheDocument();
    expect(screen.getByLabelText("Copy console output")).toBeInTheDocument();
  });
});
