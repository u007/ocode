import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { render, act, waitFor } from "@testing-library/react";
import {
  useJumpToSession,
  useJumpToPendingAsk,
  PulseJumpProvider,
} from "./jumpToSession";
import { browserActions } from "./browserStore";
import { sideChatKey } from "./sidePaneState";
import type { Project } from "../api/types";

const mockSelectProject = vi.fn();
const mockOpenSessionTab = vi.fn();
const projects: Project[] = [
  { path: "/proj-a", name: "a", added_at: "", last_used_at: "", order: 1, group: "" },
  { path: "/proj-b", name: "b", added_at: "", last_used_at: "", order: 2, group: "" },
  {
    path: "/shared",
    name: "local",
    added_at: "",
    last_used_at: "",
    order: 3,
    group: "",
  },
  {
    path: "/shared",
    name: "remote",
    added_at: "",
    last_used_at: "",
    order: 4,
    group: "",
    host: "user@box",
  },
] as unknown as Project[];

vi.mock("../stores/projectStore", () => ({
  useProjectState: () => ({
    state: { projects, activeProject: projects[0] },
    selectProject: mockSelectProject,
    openSessionTab: mockOpenSessionTab,
  }),
}));

// Re-created per test: afterEach restores all mocks, so a module-level spy
// would silently stop recording from the second test onward.
let openSpy: ReturnType<typeof vi.spyOn>;

let exitPulseCalls = 0;
let jump: (t: { projectPath: string; host: string; sessionId: string; title: string }) => void;
let jumpAsk: (t: {
  projectPath: string;
  host: string;
  sessionId: string;
  title: string;
}) => void;

function Probe() {
  jump = useJumpToSession();
  jumpAsk = useJumpToPendingAsk();
  return null;
}

function mount() {
  return render(
    <PulseJumpProvider
      exitPulse={() => {
        exitPulseCalls += 1;
      }}
    >
      <Probe />
    </PulseJumpProvider>,
  );
}

beforeEach(() => {
  mockSelectProject.mockReset();
  mockOpenSessionTab.mockReset();
  openSpy = vi.spyOn(browserActions, "open");
  exitPulseCalls = 0;
});

afterEach(() => vi.restoreAllMocks());

describe("useJumpToSession", () => {
  it("selects the target project before opening the session tab", async () => {
    const order: string[] = [];
    mockSelectProject.mockImplementation(async () => {
      order.push("select");
    });
    mockOpenSessionTab.mockImplementation(() => {
      order.push("open");
    });
    mount();

    await act(async () => {
      jump({ projectPath: "/proj-b", host: "", sessionId: "ses_2", title: "two" });
    });

    // Order is load-bearing: opening the tab before the project is active
    // binds it to the wrong project's session list.
    expect(order).toEqual(["select", "open"]);
    expect(mockSelectProject).toHaveBeenCalledWith(projects[1]);
    expect(mockOpenSessionTab).toHaveBeenCalledWith("ses_2", "two", "/proj-b");
  });

  it("also selects when the session is already in the active project (idempotent)", async () => {
    mount();
    await act(async () => {
      jump({ projectPath: "/proj-a", host: "", sessionId: "ses_1", title: "one" });
    });
    expect(mockSelectProject).toHaveBeenCalledWith(projects[0]);
    expect(mockOpenSessionTab).toHaveBeenCalledWith("ses_1", "one", "/proj-a");
  });

  it("passes the host so a remote project is not confused with a local one", async () => {
    mount();
    await act(async () => {
      jump({ projectPath: "/shared", host: "user@box", sessionId: "ses_r", title: "remote" });
    });
    // Same path exists locally and remotely; identity is path+host.
    expect(mockSelectProject).toHaveBeenCalledWith(projects[3]);
    expect(mockSelectProject).not.toHaveBeenCalledWith(projects[2]);
  });

  it("leaves the dashboard after a successful jump", async () => {
    mount();
    await act(async () => {
      jump({ projectPath: "/proj-b", host: "", sessionId: "ses_2", title: "two" });
    });
    await waitFor(() => expect(exitPulseCalls).toBe(1));
  });

  it("refuses to jump to an unknown project and says why", async () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    mount();
    await act(async () => {
      jump({ projectPath: "/nope", host: "", sessionId: "ses_x", title: "x" });
    });
    // Opening a tab for a project the store does not know would create a
    // session bound to no project — silently wrong, so it is refused loudly.
    expect(mockOpenSessionTab).not.toHaveBeenCalled();
    expect(exitPulseCalls).toBe(0);
    expect(spy).toHaveBeenCalled();
    expect(spy.mock.calls.map((c) => c.join(" ")).join("\n")).toContain("/nope");
  });

  it("does not open the side pane", async () => {
    mount();
    await act(async () => {
      jump({ projectPath: "/proj-b", host: "", sessionId: "ses_2", title: "two" });
    });
    expect(openSpy).not.toHaveBeenCalled();
  });
});

describe("useJumpToPendingAsk", () => {
  it("jumps, then opens the side pane for that session", async () => {
    const order: string[] = [];
    mockSelectProject.mockImplementation(async () => {
      order.push("select");
    });
    mockOpenSessionTab.mockImplementation(() => {
      order.push("open");
    });
    openSpy.mockImplementation(() => {
      order.push("pane");
    });
    mount();

    await act(async () => {
      jumpAsk({ projectPath: "/proj-b", host: "", sessionId: "ses_2", title: "two" });
    });

    // The pane key is per-session, so it must be opened AFTER the tab exists.
    expect(order).toEqual(["select", "open", "pane"]);
    expect(openSpy).toHaveBeenCalledWith(sideChatKey("ses_2"), "");
  });

  it("does not open a pane when the project is unknown", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    mount();
    await act(async () => {
      jumpAsk({ projectPath: "/nope", host: "", sessionId: "ses_x", title: "x" });
    });
    expect(openSpy).not.toHaveBeenCalled();
  });
});

describe("PulseJumpProvider", () => {
  it("throws a directed error when a jump hook is used without the provider", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    function Bare() {
      useJumpToSession();
      return null;
    }
    expect(() => render(<Bare />)).toThrow(/PulseJumpProvider/);
    spy.mockRestore();
  });
});
