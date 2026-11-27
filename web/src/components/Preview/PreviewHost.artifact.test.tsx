import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import PreviewHost from "./PreviewHost";

vi.mock("../../api/client", () => ({
  api: {
    putSessionViewer: vi.fn(() => Promise.resolve()),
    deleteSessionViewer: vi.fn(() => Promise.resolve()),
    checkFileAccess: vi.fn(() => Promise.resolve()),
    fetchFileRaw: vi.fn(),
    getFileContent: vi.fn(),
    saveFileContent: vi.fn(),
    openFileWithOS: vi.fn(),
    grantFilePath: vi.fn(),
  },
}));
vi.mock("../Browser/BrowserPanel", () => ({
  BrowserPanel: () => <div data-testid="browser-panel" />,
}));
vi.mock("./PreviewSurface", () => ({
  default: () => <div data-testid="preview-surface" />,
}));
vi.mock("../../lib/eventBus", () => ({
  eventBus: { on: () => () => undefined },
}));
vi.mock("../Files/FileEditor", () => ({ default: () => null }));

describe("PreviewHost artifact surface", () => {
  beforeEach(() => {
    localStorage.clear();
    window.history.replaceState({}, "", "/");
  });

  it("shows a sandboxed artifact iframe for an http wrapper URL", () => {
    const wrapper = "http://localhost:3000/p/a1/tok/~bridge";
    window.history.replaceState(
      {},
      "",
      `/?artifact=${encodeURIComponent(wrapper)}`,
    );

    render(
      <PreviewHost
        stateKey="side:chat:artifact-test"
        projectRoot="/proj"
        request={null}
        nonce={0}
      />,
    );

    fireEvent.click(screen.getByRole("tab", { name: "Artifact" }));

    const frame = screen.getByTitle("Artifact preview");
    const src = new URL(frame.getAttribute("src") ?? "");
    expect(`${src.origin}${src.pathname}`).toBe(wrapper);
    expect(src.searchParams.get("embedOrigin")).toBe(window.location.origin);
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame.getAttribute("sandbox")).not.toContain("allow-same-origin");
  });

  it("refuses a non-http artifact URL", () => {
    window.history.replaceState({}, "", "/?artifact=javascript:alert(1)");

    render(
      <PreviewHost
        stateKey="side:chat:artifact-test-2"
        projectRoot="/proj"
        request={null}
        nonce={0}
      />,
    );

    fireEvent.click(screen.getByRole("tab", { name: "Artifact" }));
    expect(screen.queryByTitle("Artifact preview")).toBeNull();
    expect(screen.getByText(/No artifact URL/)).toBeDefined();
  });
});
