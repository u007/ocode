import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { api } from "../../api/client";
import PreviewHost from "./PreviewHost";

vi.mock("../../api/client", () => ({
  api: { openFileWithOS: vi.fn(), fetchFileRaw: vi.fn(), getFileContent: vi.fn(), saveFileContent: vi.fn() },
}));

vi.mock("../Browser/BrowserPanel", () => ({
  BrowserPanel: () => <div data-testid="browser-panel" />,
}));

vi.mock("../Files/FileEditor", () => ({ default: () => <div data-testid="file-editor" /> }));

// Stub the heavy viewer surface so the tests only exercise PreviewHost's shell
// state (surface/doc/page) and can observe the page it was handed.
vi.mock("./PreviewSurface", () => ({
  default: ({ path, page }: { path: string; page: number }) => (
    <div data-testid="preview-surface" data-path={path} data-page={String(page)} />
  ),
}));
// LegacyOfficePane stays REAL: its own test relies on the "Open in app" button.

beforeEach(() => {
  localStorage.clear();
});

describe("PreviewHost legacy fallback", () => {
  it("opens legacy .doc with the request's own project root, not the pane default", async () => {
    vi.mocked(api.openFileWithOS).mockResolvedValue({ path: "/proj/old.doc", status: "opened" });
    render(
      <PreviewHost
        stateKey="side:chat:test"
        projectRoot="/default"
        request={{ path: "old.doc", kind: "text", page: 1, projectRoot: "/proj" }}
        nonce={1}
      />,
    );

    expect(await screen.findByText("old.doc")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Open in app" }));
    expect(api.openFileWithOS).toHaveBeenCalledWith("old.doc", "/proj");
  });
});

describe("PreviewHost regression (after PreviewSurface extraction)", () => {
  it("renders sidebar shell with browser/preview tabs", () => {
    const { container } = render(<PreviewHost stateKey="tab:test" request={null} nonce={0} />);
    expect(container.textContent).toContain("Browser");
    expect(container.textContent).toContain("Preview");
  });
});

describe("PreviewHost per-project persistence", () => {
  // The panel is keyed by the active session tab in App, so a project switch
  // UNMOUNTS it. Without persistence the file/page/active-tab all reset.
  it("restores the last previewed file and page for the same project on remount", () => {
    const view = render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj-a"
        request={{ path: "docs/report.pdf", kind: "pdf", page: 7 }}
        nonce={1}
      />,
    );
    expect(screen.getByTestId("preview-surface").getAttribute("data-page")).toBe("7");
    view.unmount();

    // Remount as App does after the project switch comes back.
    render(<PreviewHost stateKey="side:chat:t1" projectRoot="/proj-a" request={null} nonce={0} />);
    expect(screen.getByTestId("preview-surface").getAttribute("data-path")).toBe("docs/report.pdf");
    expect(screen.getByTestId("preview-surface").getAttribute("data-page")).toBe("7");
  });

  it("does not leak one project's preview into another project's panel", () => {
    const a = render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj-a"
        request={{ path: "docs/a.pdf", kind: "pdf", page: 3 }}
        nonce={1}
      />,
    );
    expect(screen.getByTestId("preview-surface").getAttribute("data-path")).toBe("docs/a.pdf");
    a.unmount();

    // Switching to /proj-b mounts a panel for that project: it must start on
    // the Browser tab, not inherit /proj-a's file.
    render(<PreviewHost stateKey="side:chat:t2" projectRoot="/proj-b" request={null} nonce={0} />);
    expect(screen.queryByTestId("preview-surface")).toBeNull();
    expect(screen.getByTestId("browser-panel")).toBeDefined();
  });


  it("keeps project A's preview while project B's panel is open, then restores A", () => {
    const a = render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj-a"
        request={{ path: "docs/a.pdf", kind: "pdf", page: 5 }}
        nonce={1}
      />,
    );
    a.unmount();
    const b = render(<PreviewHost stateKey="side:chat:t2" projectRoot="/proj-b" request={null} nonce={0} />);
    expect(screen.queryByTestId("preview-surface")).toBeNull();
    b.unmount();

    render(<PreviewHost stateKey="side:chat:t1" projectRoot="/proj-a" request={null} nonce={0} />);
    expect(screen.getByTestId("preview-surface").getAttribute("data-path")).toBe("docs/a.pdf");
    expect(screen.getByTestId("preview-surface").getAttribute("data-page")).toBe("5");
  });

  it("does not erase a project's saved preview when a foreign-anchored doc renders", () => {
    // Establish /proj-a's slot.
    const a = render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj-a"
        request={{ path: "docs/a.pdf", kind: "pdf", page: 3 }}
        nonce={1}
      />,
    );
    expect(screen.getByTestId("preview-surface").getAttribute("data-path")).toBe("docs/a.pdf");
    a.unmount();

    // A request carrying another project's anchor renders in this pane but must
    // NOT be persisted under /proj-a: writing the path-less snapshot would
    // overwrite the valid slot, so switching away and back would show nothing.
    const foreign = render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj-a"
        request={{ path: "other/b.pdf", kind: "pdf", page: 9, projectRoot: "/proj-b" }}
        nonce={1}
      />,
    );
    expect(screen.getByTestId("preview-surface").getAttribute("data-path")).toBe("other/b.pdf");
    foreign.unmount();

    // /proj-a's original slot survived intact.
    render(<PreviewHost stateKey="side:chat:t1" projectRoot="/proj-a" request={null} nonce={0} />);
    expect(screen.getByTestId("preview-surface").getAttribute("data-path")).toBe("docs/a.pdf");
    expect(screen.getByTestId("preview-surface").getAttribute("data-page")).toBe("3");
  });

  it("acknowledges an applied activation exactly once per nonce", () => {
    const onConsumeActivation = vi.fn();
    const view = render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj-a"
        request={{ path: "docs/a.pdf", kind: "pdf", page: 2 }}
        nonce={4}
        onConsumeActivation={onConsumeActivation}
      />,
    );
    expect(onConsumeActivation).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("preview-surface").getAttribute("data-page")).toBe("2");

    // A re-render with the SAME nonce (App state churn) must not re-apply or
    // re-consume the already-applied activation.
    view.rerender(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj-a"
        request={{ path: "docs/a.pdf", kind: "pdf", page: 2 }}
        nonce={4}
        onConsumeActivation={onConsumeActivation}
      />,
    );
    expect(onConsumeActivation).toHaveBeenCalledTimes(1);
  });
});
