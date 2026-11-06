import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { api } from "../../api/client";
import PreviewHost from "./PreviewHost";
import { loadSidebarPreviewState } from "./sidebarPreviewState";
import { isOutsideProjectRoot } from "./outsideRootAccess";
import { readViewerState, resetViewerStates } from "../../lib/viewerContext";

// Passthrough by default; a single test overrides it to hold the probe open.
vi.mock("./outsideRootAccess", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./outsideRootAccess")>();
  return { ...actual, isOutsideProjectRoot: vi.fn(actual.isOutsideProjectRoot) };
});

vi.mock("../../api/client", () => ({
  api: {
    openFileWithOS: vi.fn(),
    fetchFileRaw: vi.fn(),
    getFileContent: vi.fn(),
    saveFileContent: vi.fn(),
    checkFileAccess: vi.fn(),
    grantFilePath: vi.fn(),
    putSessionViewer: vi.fn(() => Promise.resolve()),
    deleteSessionViewer: vi.fn(() => Promise.resolve()),
  },
}));

vi.mock("../Browser/BrowserPanel", () => ({
  BrowserPanel: () => <div data-testid="browser-panel" />,
}));

// Capture subscriptions so tests can emit tool/turn events at the component.
const busHandlers = new Map<string, Set<(env: unknown) => void>>();
const busEnv = (event: string, session_id: string | undefined, data: unknown) => ({
  event,
  session_id,
  seq: 1,
  data,
});
vi.mock("../../lib/eventBus", () => ({
  eventBus: {
    on: (event: string, handler: (env: unknown) => void) => {
      let set = busHandlers.get(event);
      if (!set) {
        set = new Set();
        busHandlers.set(event, set);
      }
      set.add(handler);
      return () => set!.delete(handler);
    },
  },
}));
function emit(event: string, session_id: string | undefined, data: unknown) {
  for (const h of busHandlers.get(event) ?? []) h(busEnv(event, session_id, data));
}

vi.mock("../Files/FileEditor", () => ({ default: () => <div data-testid="file-editor" /> }));

// Stub the heavy viewer surface so the tests only exercise PreviewHost's shell
// state (surface/doc/page/revision/followTail) and can observe what it handed.
vi.mock("./PreviewSurface", () => ({
  default: ({
    path,
    page,
    projectRoot,
    revision,
    followTail,
  }: { path: string; page: number; projectRoot?: string; revision?: number; followTail?: boolean }) => (
    <div
      data-testid="preview-surface"
      data-path={path}
      data-root={projectRoot ?? ""}
      data-page={String(page)}
      data-revision={revision === undefined ? "none" : String(revision)}
      data-follow-tail={followTail ? "true" : "false"}
    />
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

describe("PreviewHost per-session persistence", () => {
  // The panel is keyed by the active session tab in App (`key={sideStateKey}`),
  // so switching sessions/projects UNMOUNTS it. Without persistence the
  // file/page/active-tab all reset; without per-session keying one chat's
  // preview would leak into every other chat.
  it("restores the last previewed file and page for the same session on remount", async () => {
    const view = render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj-a"
        request={{ path: "docs/report.pdf", kind: "pdf", page: 7 }}
        nonce={1}
      />,
    );
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-page")).toBe("7");
    view.unmount();

    // Remount as App does after the project switch comes back.
    render(<PreviewHost stateKey="side:chat:t1" projectRoot="/proj-a" request={null} nonce={0} />);
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-path")).toBe("docs/report.pdf");
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-page")).toBe("7");
  });

  it("does not leak one project's preview into another project's panel", async () => {
    const a = render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj-a"
        request={{ path: "docs/a.pdf", kind: "pdf", page: 3 }}
        nonce={1}
      />,
    );
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-path")).toBe("docs/a.pdf");
    a.unmount();

    // Switching to /proj-b mounts a panel for that project: it must start on
    // the Browser tab, not inherit /proj-a's file.
    render(<PreviewHost stateKey="side:chat:t2" projectRoot="/proj-b" request={null} nonce={0} />);
    expect(screen.queryByTestId("preview-surface")).toBeNull();
    expect(screen.getByTestId("browser-panel")).toBeDefined();
  });

  it("keeps each chat session's preview separate within the SAME project", async () => {
    // The reported bug: opening the side preview in one chat showed it in every
    // other chat of the project. Two sessions in /proj must not share a slot.
    const s1 = render(
      <PreviewHost
        stateKey="side:chat:s1"
        projectRoot="/proj"
        request={{ path: "docs/one.pdf", kind: "pdf", page: 4 }}
        nonce={1}
      />,
    );
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-path")).toBe("docs/one.pdf");
    s1.unmount();

    // Session 2 (same project) starts fresh — no leak from s1.
    const s2 = render(<PreviewHost stateKey="side:chat:s2" projectRoot="/proj" request={null} nonce={0} />);
    expect(screen.queryByTestId("preview-surface")).toBeNull();
    expect(screen.getByTestId("browser-panel")).toBeDefined();
    s2.unmount();

    // Session 1 restores its own file/page.
    render(<PreviewHost stateKey="side:chat:s1" projectRoot="/proj" request={null} nonce={0} />);
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-path")).toBe("docs/one.pdf");
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-page")).toBe("4");
  });


  it("keeps project A's preview while project B's panel is open, then restores A", async () => {
    const a = render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj-a"
        request={{ path: "docs/a.pdf", kind: "pdf", page: 5 }}
        nonce={1}
      />,
    );
    // The request is saved once its access check has applied the doc, so wait
    // for it before switching away.
    await screen.findByTestId("preview-surface");
    a.unmount();
    const b = render(<PreviewHost stateKey="side:chat:t2" projectRoot="/proj-b" request={null} nonce={0} />);
    expect(screen.queryByTestId("preview-surface")).toBeNull();
    expect(screen.getByTestId("browser-panel")).toBeDefined();
    b.unmount();

    render(<PreviewHost stateKey="side:chat:t1" projectRoot="/proj-a" request={null} nonce={0} />);
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-path")).toBe("docs/a.pdf");
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-page")).toBe("5");
  });

  it("does not erase a project's saved preview when a foreign-anchored doc renders", async () => {
    // Establish /proj-a's slot.
    const a = render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj-a"
        request={{ path: "docs/a.pdf", kind: "pdf", page: 3 }}
        nonce={1}
      />,
    );
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-path")).toBe("docs/a.pdf");
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
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-path")).toBe("other/b.pdf");
    foreign.unmount();

    // /proj-a's original slot survived intact.
    render(<PreviewHost stateKey="side:chat:t1" projectRoot="/proj-a" request={null} nonce={0} />);
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-path")).toBe("docs/a.pdf");
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-page")).toBe("3");
  });

  it("acknowledges an applied activation exactly once per nonce", async () => {
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
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-page")).toBe("2");

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

  it("saves an in-root probe that resolves after unmount, so the next mount restores it", async () => {
    let resolveProbe: (outside: boolean) => void = () => {};
    vi.mocked(isOutsideProjectRoot).mockImplementationOnce(
      () =>
        new Promise<boolean>((resolve) => {
          resolveProbe = resolve;
        }),
    );
    const onConsumeActivation = vi.fn();
    const view = render(
      <PreviewHost
        stateKey="side:chat:u1"
        projectRoot="/proj-a"
        request={{ path: "img/pic.png", kind: "image", page: 1 }}
        nonce={1}
        onConsumeActivation={onConsumeActivation}
      />,
    );
    expect(onConsumeActivation).toHaveBeenCalledTimes(1);
    view.unmount();

    await act(async () => resolveProbe(false));
    const saved = loadSidebarPreviewState("side:chat:u1");
    expect(saved?.surface).toBe("preview");
    expect(saved?.path).toBe("img/pic.png");

    render(<PreviewHost stateKey="side:chat:u1" projectRoot="/proj-a" request={null} nonce={0} />);
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-path")).toBe("img/pic.png");
  });
});

describe("PreviewHost live refresh (tool-stream driven)", () => {
  beforeEach(() => {
    busHandlers.clear();
  });

  it("bumps the revision when a mutating tool touches the previewed file", async () => {
    const view = render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj"
        sessionId="ses-1"
        request={{ path: "docs/x.md", kind: "markdown", page: 1 }}
        nonce={1}
      />,
    );
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-revision")).toBe("0");

    act(() => {
      emit("tool_start", "ses-1", { tool: "edit", command: '{"path":"/proj/docs/x.md"}' });
    });
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-revision")).toBe("1");
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-follow-tail")).toBe("true");

    // A second edit of the same file bumps again.
    act(() => {
      emit("tool_start", "ses-1", { tool: "write", command: '{"path":"docs/x.md","content":"v2"}' });
    });
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-revision")).toBe("2");

    view.unmount();
  });

  it("does not bump for unrelated files, non-mutating tools, or other sessions", async () => {
    render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj"
        sessionId="ses-1"
        request={{ path: "docs/x.md", kind: "markdown", page: 1 }}
        nonce={1}
      />,
    );
    // Another session's tool activity must not flip followTail...
    act(() => {
      emit("tool_start", "ses-2", { tool: "write", command: '{"path":"docs/x.md"}' });
    });
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-revision")).toBe("0");
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-follow-tail")).toBe("false");
    // ...but same-session activity (even non-mutating) means the turn is
    // running; only the FILE-MATCHING bumps are gated to mutating tools.
    act(() => {
      emit("tool_start", "ses-1", { tool: "read", command: '{"path":"docs/x.md"}' });
      emit("tool_start", "ses-1", { tool: "edit", command: '{"path":"docs/other.md"}' });
    });
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-revision")).toBe("0");
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-follow-tail")).toBe("true");
  });

  it("followTail clears on turn_done/turn_error for the same session", async () => {
    render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj"
        sessionId="ses-1"
        request={{ path: "docs/x.md", kind: "markdown", page: 1 }}
        nonce={1}
      />,
    );
    act(() => {
      emit("tool_start", "ses-1", { tool: "write", command: '{"path":"docs/x.md"}' });
    });
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-follow-tail")).toBe("true");
    act(() => {
      emit("turn_done", "ses-1", {});
    });
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-follow-tail")).toBe("false");
  });

  it("clears followTail on turn_error and re-arms on the next tool_start", async () => {
    render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj"
        sessionId="ses-1"
        request={{ path: "docs/x.md", kind: "markdown", page: 1 }}
        nonce={1}
      />,
    );
    act(() => {
      emit("tool_start", "ses-1", { tool: "edit", command: '{"path":"docs/x.md"}' });
      emit("turn_error", "ses-1", { error: "boom" });
    });
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-follow-tail")).toBe("false");
    act(() => {
      emit("tool_start", "ses-1", { tool: "read", command: '{"path":"anything"}' });
    });
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-follow-tail")).toBe("true");
  });

  it("does not subscribe without a sessionId", async () => {
    render(
      <PreviewHost
        stateKey="side:chat:t1"
        projectRoot="/proj"
        request={{ path: "docs/x.md", kind: "markdown", page: 1 }}
        nonce={1}
      />,
    );
    act(() => {
      emit("tool_start", "ses-1", { tool: "write", command: '{"path":"docs/x.md"}' });
    });
    expect((await screen.findByTestId("preview-surface")).getAttribute("data-revision")).toBe("0");
    expect(busHandlers.get("tool_start")?.size ?? 0).toBe(0);
  });
});

describe("PreviewHost reading-column control", () => {
  // The column only applies to markdown prose. Showing the control for a PDF or
  // a code file would promise a behaviour those surfaces do not have.
  it("offers the width control for markdown and hides it for other kinds", async () => {
    const md = render(
      <PreviewHost
        stateKey="side:chat:md"
        projectRoot="/proj"
        request={{ path: "README.md", kind: "markdown", page: 1 }}
        nonce={1}
      />,
    );
    expect(await screen.findByTestId("preview-width-trigger")).toBeDefined();
    md.unmount();

    render(
      <PreviewHost
        stateKey="side:chat:txt"
        projectRoot="/proj"
        request={{ path: "src/main.go", kind: "text", page: 1 }}
        nonce={1}
      />,
    );
    expect(screen.queryByTestId("preview-width-trigger")).toBeNull();
  });
});

describe("PreviewHost outside-root grant", () => {
  const OUTSIDE = "/elsewhere/shots/a.png";
  const GRANT_BUTTON = { name: "Grant access to its folder" };
  const STATE_KEY = "ocode.ui.sidebarPreview.v2";

  beforeEach(() => {
    vi.mocked(api.checkFileAccess).mockReset();
    vi.mocked(api.grantFilePath).mockReset();
  });

  // Models the server: the absolute outside path is refused under the active
  // project, and the granted root serves its own files.
  function refuseOutsideProject() {
    vi.mocked(api.checkFileAccess).mockImplementation(async (path, root) => {
      if (path === OUTSIDE && root === "/proj") throw new Error("path is outside the project root");
    });
  }

  it("shows the grant prompt instead of a viewer for an outside-root request", async () => {
    refuseOutsideProject();
    render(
      <PreviewHost
        stateKey="side:chat:g1"
        projectRoot="/proj"
        request={{ path: OUTSIDE, kind: "image", page: 1 }}
        nonce={1}
      />,
    );

    expect(await screen.findByRole("button", GRANT_BUTTON)).toBeDefined();
    expect(screen.getByText(OUTSIDE)).toBeDefined();
    expect(screen.queryByTestId("preview-surface")).toBeNull();
    expect(api.checkFileAccess).toHaveBeenCalledWith(OUTSIDE, "/proj", undefined);
  });

  it("grants on click and re-anchors the viewer to the granted root", async () => {
    refuseOutsideProject();
    vi.mocked(api.grantFilePath).mockResolvedValue({
      root: "/elsewhere/shots",
      path: OUTSIDE,
      granted: true,
      roots: ["/elsewhere/shots"],
    });
    render(
      <PreviewHost
        stateKey="side:chat:g1"
        projectRoot="/proj"
        request={{ path: OUTSIDE, kind: "image", page: 1 }}
        nonce={1}
      />,
    );

    fireEvent.click(await screen.findByRole("button", GRANT_BUTTON));
    const surface = await screen.findByTestId("preview-surface");
    expect(api.grantFilePath).toHaveBeenCalledWith(OUTSIDE, undefined);
    expect(surface.getAttribute("data-path")).toBe("a.png");
    expect(surface.getAttribute("data-root")).toBe("/elsewhere/shots");
  });

  it("grants a relative outside request by its absolute path, resolved against the project root", async () => {
    vi.mocked(api.checkFileAccess).mockImplementation(async (path, root) => {
      if (path === "../Downloads/pic.png" && root === "/Users/x/proj") {
        throw new Error("path is outside the project root");
      }
    });
    vi.mocked(api.grantFilePath).mockResolvedValue({
      root: "/Users/x/Downloads",
      path: "/Users/x/Downloads/pic.png",
      granted: true,
      roots: ["/Users/x/Downloads"],
    });
    render(
      <PreviewHost
        stateKey="side:chat:g2"
        projectRoot="/Users/x/proj"
        request={{ path: "../Downloads/pic.png", kind: "image", page: 1 }}
        nonce={1}
      />,
    );

    expect(await screen.findByText("/Users/x/Downloads/pic.png")).toBeDefined();
    fireEvent.click(screen.getByRole("button", GRANT_BUTTON));
    const surface = await screen.findByTestId("preview-surface");
    expect(api.grantFilePath).toHaveBeenCalledWith("/Users/x/Downloads/pic.png", undefined);
    expect(surface.getAttribute("data-path")).toBe("pic.png");
    expect(surface.getAttribute("data-root")).toBe("/Users/x/Downloads");
  });

  it("restores a granted doc under its granted root after a remount", async () => {
    refuseOutsideProject();
    vi.mocked(api.grantFilePath).mockResolvedValue({
      root: "/elsewhere/shots",
      path: OUTSIDE,
      granted: true,
      roots: ["/elsewhere/shots"],
    });
    const view = render(
      <PreviewHost
        stateKey="side:chat:g1"
        projectRoot="/proj"
        request={{ path: OUTSIDE, kind: "image", page: 1 }}
        nonce={1}
      />,
    );
    fireEvent.click(await screen.findByRole("button", GRANT_BUTTON));
    await screen.findByTestId("preview-surface");
    view.unmount();

    render(<PreviewHost stateKey="side:chat:g1" projectRoot="/proj" request={null} nonce={0} />);
    const restored = await screen.findByTestId("preview-surface");
    expect(restored.getAttribute("data-path")).toBe("a.png");
    expect(restored.getAttribute("data-root")).toBe("/elsewhere/shots");
  });

  it("clears a restored grant the server no longer honours, without a prompt", async () => {
    // A grant lives in server memory: after a restart the saved root is refused.
    vi.mocked(api.checkFileAccess).mockRejectedValue(new Error("path is outside the project root"));
    localStorage.setItem(
      STATE_KEY,
      JSON.stringify({
        "side:chat:g2": {
          surface: "preview",
          path: "a.png",
          kind: "image",
          projectRoot: "/elsewhere/shots",
          grantedFrom: "/proj",
          page: 1,
        },
      }),
    );

    render(<PreviewHost stateKey="side:chat:g2" projectRoot="/proj" request={null} nonce={0} />);

    // The empty state only appears once the access check has cleared the doc.
    expect(await screen.findByText("No file previewed yet.")).toBeDefined();
    expect(api.checkFileAccess).toHaveBeenCalledWith("a.png", "/elsewhere/shots", undefined);
    expect(loadSidebarPreviewState("side:chat:g2")?.path).toBeUndefined();
    expect(screen.queryByRole("button", GRANT_BUTTON)).toBeNull();
    expect(screen.queryByTestId("preview-surface")).toBeNull();
  });
});

describe("PreviewHost viewer report", () => {
  beforeEach(() => {
    vi.mocked(api.checkFileAccess).mockReset();
  });

  it("reports the shown doc to the session's viewer context and clears it on unmount", async () => {
    resetViewerStates();
    const view = render(
      <PreviewHost
        stateKey="side:chat:v1"
        sessionId="v1"
        projectRoot="/proj"
        request={{ path: "docs/report.pdf", kind: "pdf", page: 2 }}
        nonce={1}
      />,
    );
    await screen.findByTestId("preview-surface");
    expect(readViewerState("v1")).toMatchObject({ path: "docs/report.pdf", kind: "pdf" });

    view.unmount();
    expect(readViewerState("v1")).toBeNull();
  });
});
