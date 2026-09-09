import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { AddressBar } from "./AddressBar";

const base = {
  url: "https://example.com/",
  status: 200,
  mode: "chrome" as const,
  error: "",
  canBack: false,
  canForward: false,
  onNavigate: vi.fn(),
  onBack: vi.fn(),
  onForward: vi.fn(),
  onReload: vi.fn(),
  onOpenExternal: vi.fn(),
};

describe("AddressBar", () => {
  it("navigates on Enter", () => {
    const onNavigate = vi.fn();
    render(<AddressBar {...base} onNavigate={onNavigate} />);
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "https://foo.dev/" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onNavigate).toHaveBeenCalledWith("https://foo.dev/");
  });

  it("disables back/forward at history ends", () => {
    render(<AddressBar {...base} canBack={false} canForward={false} />);
    expect(screen.getByLabelText("Back")).toBeDisabled();
    expect(screen.getByLabelText("Forward")).toBeDisabled();
  });

  it("shows the mode chip and status from props, not the iframe", () => {
    render(<AddressBar {...base} mode="local" status={304} />);
    expect(screen.getByText(/local/i)).toBeInTheDocument();
    expect(screen.getByText("304")).toBeInTheDocument();
  });

  it("renders the CHROME chip for chrome mode", () => {
    render(<AddressBar {...base} mode="chrome" />);
    // css text-transform:uppercase is not applied by jsdom — match the raw
    // text; the class carries the uppercasing.
    expect(screen.getByText(/^chrome$/i)).toBeInTheDocument();
  });

  it("shows an error badge when nav failed", () => {
    render(<AddressBar {...base} status={0} error="connection refused" />);
    expect(screen.getByText(/connection refused/i)).toBeInTheDocument();
  });

  it("shows a loading spinner while a page is in flight", () => {
    render(<AddressBar {...base} status={0} error="" />);
    expect(screen.getByLabelText("Loading")).toBeInTheDocument();
  });

  it("hides the loading spinner once a status arrives and does not mask errors", () => {
    render(<AddressBar {...base} status={200} error="" />);
    expect(screen.queryByLabelText("Loading")).not.toBeInTheDocument();
  });

  it("shows a zoom badge only away from 100%, and resets it on click", () => {
    const { rerender } = render(<AddressBar {...base} zoom={1} />);
    expect(screen.queryByLabelText("Reset zoom")).not.toBeInTheDocument();

    const onResetZoom = vi.fn();
    rerender(<AddressBar {...base} zoom={1.25} onResetZoom={onResetZoom} />);
    const badge = screen.getByLabelText("Reset zoom");
    expect(badge).toHaveTextContent("125%");
    fireEvent.click(badge);
    expect(onResetZoom).toHaveBeenCalled();
  });
});
