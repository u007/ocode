import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import HtmlViewer from "./HtmlViewer";

describe("HtmlViewer", () => {
  it("renders an iframe with srcDoc", () => {
    render(<HtmlViewer content="<h1>Hello</h1>" />);
    const iframe = screen.getByTitle("HTML preview");
    expect(iframe.tagName).toBe("IFRAME");
    expect(iframe.getAttribute("srcDoc")).toContain("<h1>Hello</h1>");
    // The find agent is appended to the srcDoc (see htmlFindAgent.ts).
    expect(iframe.getAttribute("srcDoc")).toContain("__ocodeFind");
  });

  it("uses sandbox=allow-scripts (no allow-same-origin)", () => {
    render(<HtmlViewer content="<p>safe</p>" />);
    const iframe = screen.getByTitle("HTML preview");
    expect(iframe).toHaveAttribute("sandbox", "allow-scripts");
    expect(iframe.getAttribute("sandbox")).not.toContain("allow-same-origin");
  });

  it("updates srcDoc when content changes", () => {
    const { rerender } = render(<HtmlViewer content="<p>one</p>" />);
    expect(screen.getByTitle("HTML preview").getAttribute("srcDoc")).toContain("<p>one</p>");
    rerender(<HtmlViewer content="<p>two</p>" />);
    const srcDoc = screen.getByTitle("HTML preview").getAttribute("srcDoc") ?? "";
    expect(srcDoc).toContain("<p>two</p>");
    expect(srcDoc).not.toContain("<p>one</p>");
  });
});
