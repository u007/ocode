import { useRef, useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent } from "react";
import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useListNavigation } from "./useListNavigation";

interface HarnessProps {
  ids?: string[];
  onActivate: (index: number) => void;
  onToggle?: (index: number) => void;
  multiple?: boolean;
  resetKey?: string;
  hasMore?: boolean;
  onReachEnd?: () => void;
  onRootKeyDown?: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
}

function focus(element: HTMLElement) {
  act(() => element.focus());
}

function Harness({
  ids = ["one", "two", "three"],
  onActivate,
  onToggle,
  multiple,
  resetKey,
  hasMore,
  onReachEnd,
  onRootKeyDown,
}: HarnessProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const navigation = useListNavigation({
    itemIds: ids,
    onActivate,
    inputRef,
    ...(onToggle === undefined ? {} : { onToggle }),
    ...(multiple === undefined ? {} : { multiple }),
    ...(resetKey === undefined ? {} : { resetKey }),
    ...(hasMore === undefined ? {} : { hasMore, onReachEnd }),
  });

  return (
    <div onKeyDown={onRootKeyDown}>
      <div onKeyDown={navigation.onKeyDown}>
        <input aria-label="Search" ref={inputRef} />
        <div role="listbox">
          {ids.map((id, index) => {
            const itemProps = navigation.getItemProps(index);
            return (
              <button key={id} type="button" {...itemProps}>
                {id}
              </button>
            );
          })}
        </div>
        <button type="button" data-testid="nested-action">
          Nested action
        </button>
        <button type="button" data-testid="external-action">
          External action
        </button>
      </div>
    </div>
  );
}

describe("useListNavigation", () => {
  it("moves from the search input through rows and returns at the top boundary", () => {
    render(<Harness onActivate={vi.fn()} />);
    const input = screen.getByRole("textbox", { name: "Search" });
    const rows = screen.getAllByRole("button", { name: /^(one|two|three)$/ });

    focus(input);
    const enterListEvent = new KeyboardEvent("keydown", {
      key: "ArrowDown",
      bubbles: true,
      cancelable: true,
    });
    act(() => input.dispatchEvent(enterListEvent));
    expect(enterListEvent.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(rows[0]);

    fireEvent.keyDown(rows[0], { key: "ArrowDown" });
    expect(document.activeElement).toBe(rows[1]);

    fireEvent.keyDown(rows[1], { key: "ArrowUp" });
    expect(document.activeElement).toBe(rows[0]);

    fireEvent.keyDown(rows[0], { key: "ArrowUp" });
    expect(document.activeElement).toBe(input);
  });

  it("supports Home and End without wrapping", () => {
    render(<Harness onActivate={vi.fn()} />);
    const rows = screen.getAllByRole("button", { name: /^(one|two|three)$/ });

    fireEvent.keyDown(rows[0], { key: "End" });
    expect(document.activeElement).toBe(rows[2]);
    fireEvent.keyDown(rows[2], { key: "Home" });
    expect(document.activeElement).toBe(rows[0]);
  });

  it("does nothing at a boundary when there is no input or more data", () => {
    const onReachEnd = vi.fn();
    const { rerender } = render(
      <Harness onActivate={vi.fn()} hasMore={false} onReachEnd={onReachEnd} />,
    );
    const first = screen.getByRole("button", { name: "one" });
    const last = screen.getByRole("button", { name: "three" });

    focus(first);
    fireEvent.keyDown(first, { key: "ArrowUp" });
    expect(document.activeElement).not.toBe(first);
    fireEvent.keyDown(last, { key: "ArrowDown" });
    expect(document.activeElement).not.toBe(last);
    expect(onReachEnd).not.toHaveBeenCalled();

    rerender(<Harness onActivate={vi.fn()} />);
    expect(screen.getByRole("button", { name: "one" })).toBeInTheDocument();
  });

  it("activates single rows and toggles multi-select rows", () => {
    const onActivate = vi.fn();
    const onToggle = vi.fn();
    const { rerender } = render(
      <Harness onActivate={onActivate} onToggle={onToggle} />,
    );
    const first = screen.getByRole("button", { name: "one" });

    fireEvent.keyDown(first, { key: "Enter" });
    expect(onActivate).toHaveBeenCalledWith(0);
    expect(onToggle).not.toHaveBeenCalled();

    rerender(<Harness multiple onActivate={onActivate} onToggle={onToggle} />);
    fireEvent.keyDown(screen.getByRole("button", { name: "one" }), { key: " " });
    fireEvent.keyDown(screen.getByRole("button", { name: "one" }), { key: "Enter" });
    expect(onToggle).toHaveBeenNthCalledWith(1, 0);
    expect(onToggle).toHaveBeenNthCalledWith(2, 0);
    expect(onActivate).toHaveBeenCalledTimes(1);
  });

  it("does not consume keys from nested actions or spaces typed in search", () => {
    const onActivate = vi.fn();
    render(<Harness onActivate={onActivate} />);
    const input = screen.getByRole("textbox", { name: "Search" });
    focus(input);
    const spaceEvent = new KeyboardEvent("keydown", {
      key: " ",
      bubbles: true,
      cancelable: true,
    });
    input.dispatchEvent(spaceEvent);

    const nested = screen.getByTestId("nested-action");
    fireEvent.keyDown(nested, { key: "Enter" });
    fireEvent.keyDown(nested, { key: "ArrowDown" });

    expect(onActivate).not.toHaveBeenCalled();
    expect(spaceEvent.defaultPrevented).toBe(false);
    expect(document.activeElement).toBe(input);
  });

  it("consumes handled keys and preserves focus on the nearest surviving row", () => {
    const onRootKeyDown = vi.fn();
    const { rerender } = render(
      <Harness onActivate={vi.fn()} onRootKeyDown={onRootKeyDown} />,
    );
    const first = screen.getByRole("button", { name: "one" });
    const second = screen.getByRole("button", { name: "two" });

    fireEvent.keyDown(first, { key: "ArrowDown" });
    expect(document.activeElement).toBe(second);
    expect(onRootKeyDown).not.toHaveBeenCalled();

    rerender(
      <Harness
        ids={["zero", "two", "three", "four"]}
        onActivate={vi.fn()}
        onRootKeyDown={onRootKeyDown}
      />,
    );
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "two" }));

    rerender(<Harness ids={["one", "three"]} onActivate={vi.fn()} />);
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "three" }));
  });

  it("starts at the first row when the search input is refocused", () => {
    render(<Harness onActivate={vi.fn()} />);
    const input = screen.getByRole("textbox", { name: "Search" });
    const first = screen.getByRole("button", { name: "one" });
    const second = screen.getByRole("button", { name: "two" });
    focus(first);
    fireEvent.keyDown(first, { key: "ArrowDown" });
    expect(document.activeElement).toBe(second);

    focus(input);
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(document.activeElement).toBe(first);
  });

  it("updates the active identity when a row receives mouse focus", () => {
    const { rerender } = render(<Harness onActivate={vi.fn()} />);
    const first = screen.getByRole("button", { name: "one" });
    const second = screen.getByRole("button", { name: "two" });
    const third = screen.getByRole("button", { name: "three" });
    focus(first);
    fireEvent.keyDown(first, { key: "ArrowDown" });
    expect(second).toHaveAttribute("data-list-nav-active", "true");

    focus(third);
    expect(third).toHaveAttribute("data-list-nav-active", "true");
    rerender(
      <Harness
        ids={["zero", "two", "three", "four"]}
        onActivate={vi.fn()}
      />,
    );
    expect(document.activeElement).toBe(third);
  });

  it("does not steal focus from a non-row control when rows change", () => {
    const { rerender } = render(<Harness onActivate={vi.fn()} />);
    const input = screen.getByRole("textbox", { name: "Search" });
    const first = screen.getByRole("button", { name: "one" });
    focus(first);
    fireEvent.keyDown(first, { key: "ArrowDown" });
    const external = screen.getByTestId("external-action");
    focus(external);

    rerender(
      <Harness
        ids={["zero", "two", "three", "four"]}
        onActivate={vi.fn()}
      />,
    );
    expect(document.activeElement).toBe(external);
    expect(input).toBeInTheDocument();
  });

  it("focuses the nearest row when the active row is removed", () => {
    const { rerender } = render(<Harness onActivate={vi.fn()} />);
    const first = screen.getByRole("button", { name: "one" });
    focus(first);
    fireEvent.keyDown(first, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "two" }));

    rerender(<Harness ids={["one", "three"]} onActivate={vi.fn()} />);
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "three" }));
  });

  it("does not steal search focus when a filter change preserves the active row", () => {
    const { rerender } = render(<Harness onActivate={vi.fn()} />);
    const input = screen.getByRole("textbox", { name: "Search" });
    const second = screen.getByRole("button", { name: "two" });
    focus(second);
    fireEvent.keyDown(second, { key: "ArrowDown" });
    focus(input);

    rerender(
      <Harness
        ids={["zero", "two", "three", "four"]}
        onActivate={vi.fn()}
      />,
    );
    expect(document.activeElement).toBe(input);
  });

  it("keeps composite identities distinct when the same label appears twice", () => {
    render(
      <Harness
        ids={["recent:one", "favorite:one"]}
        onActivate={vi.fn()}
      />,
    );
    const rows = screen.getAllByRole("button", { name: /^(recent:one|favorite:one)$/ });
    expect(rows[0]).toHaveAttribute("data-list-nav-id", "recent:one");
    expect(rows[1]).toHaveAttribute("data-list-nav-id", "favorite:one");

    fireEvent.keyDown(rows[0], { key: "ArrowDown" });
    expect(document.activeElement).toBe(rows[1]);
  });

  it("requests more rows and focuses the first new row after it renders", async () => {
    function GrowingHarness() {
      const [count, setCount] = useState(2);
      const ids = count === 2 ? ["one", "two"] : ["one", "two", "three"];
      return (
        <Harness
          ids={ids}
          onActivate={vi.fn()}
          hasMore={count === 2}
          onReachEnd={() => setCount(3)}
        />
      );
    }

    const scrollSpy = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
    render(<GrowingHarness />);
    const second = screen.getByRole("button", { name: "two" });
    fireEvent.keyDown(second, { key: "ArrowDown" });

    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole("button", { name: "three" }));
    });
    expect(scrollSpy).toHaveBeenCalledWith({ block: "nearest" });
    scrollSpy.mockRestore();
  });
});
