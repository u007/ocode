import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import ErrorBoundary from "./ErrorBoundary";

function Boom(): never {
  throw new Error("boom");
}

describe("ErrorBoundary", () => {
  it("catches a render error and shows a retry fallback", () => {
    // Suppress React's expected error logging in jsdom.
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    render(
      <ErrorBoundary scope="test">
        <Boom />
      </ErrorBoundary>,
    );
    expect(screen.getByText(/something went wrong/i)).toBeTruthy();
    expect(screen.getByText(/boom/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /retry/i })).toBeTruthy();
    errSpy.mockRestore();
  });

  it("reset clears the error state so children can render again", () => {
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    let shouldThrow = true;
    function Child() {
      if (shouldThrow) throw new Error("boom2");
      return <span>recovered</span>;
    }
    render(
      <ErrorBoundary>
        <Child />
      </ErrorBoundary>,
    );
    expect(screen.getByText(/boom2/)).toBeTruthy();
    shouldThrow = false;
    fireEvent.click(screen.getByRole("button", { name: /retry/i }));
    expect(screen.getByText("recovered")).toBeTruthy();
    errSpy.mockRestore();
  });
});

describe("ErrorBoundary containment", () => {
  it("an error in one boundary does not unmount a sibling boundary", () => {
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    function Bad(): never {
      throw new Error("project A boom");
    }
    render(
      <div>
        <ErrorBoundary scope="project:A" resetKey="A">
          <Bad />
        </ErrorBoundary>
        <ErrorBoundary scope="project:B" resetKey="B">
          <span>project B ok</span>
        </ErrorBoundary>
      </div>,
    );
    expect(screen.getByText(/project A boom/)).toBeTruthy();
    expect(screen.getByText("project B ok")).toBeTruthy();
    errSpy.mockRestore();
  });

  it("resetKey change clears a previously caught error", () => {
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    let resetKey = "A";
    let shouldThrow = true;
    function Child() {
      if (shouldThrow) throw new Error("boom rk");
      return <span>recovered rk</span>;
    }
    const { rerender } = render(
      <ErrorBoundary resetKey={resetKey}>
        <Child />
      </ErrorBoundary>,
    );
    expect(screen.getByText(/boom rk/)).toBeTruthy();
    shouldThrow = false;
    rerender(<ErrorBoundary resetKey="B"><Child /></ErrorBoundary>);
    expect(screen.getByText("recovered rk")).toBeTruthy();
    errSpy.mockRestore();
  });
});
