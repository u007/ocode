import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { api, ApiError } from "../../api/client";
import type { UpstashLoginState } from "../../api/types";
import UpstashLoginStates from "./UpstashLoginStates";

// Settings → Upstash Box → Login states. What these tests protect:
//  1. The table shows the saved states with counts, size and the holder.
//  2. Assigning a state sends the ACTIVE project and the chosen name.
//  3. A new name that breaks the rule is blocked here, with the rule shown.
//  4. Delete asks first, and a 409 from the server stays visible in the dialog.
//  5. With no active project (or a remote one) the assignment row is disabled.

const PROJECT = "/work/app";

const STATES: UpstashLoginState[] = [
  {
    name: "work",
    saved_at: "2026-11-20T09:30:00Z",
    cookies: 12,
    origins: 3,
    bytes: 2048,
    in_use_by: "",
    corrupt: false,
  },
  {
    name: "home",
    saved_at: "2026-11-18T18:00:00Z",
    cookies: 4,
    origins: 1,
    bytes: 512,
    in_use_by: "sess_live_123",
    corrupt: false,
  },
  {
    name: "broken",
    saved_at: "",
    cookies: 0,
    origins: 0,
    bytes: 90,
    in_use_by: "",
    corrupt: true,
  },
];

let listStates: Mock<typeof api.upstashListLoginStates>;
let getAssignment: Mock<typeof api.upstashGetLoginState>;
let setAssignment: Mock<typeof api.upstashSetLoginState>;
let deleteState: Mock<typeof api.upstashDeleteLoginState>;

beforeEach(() => {
  listStates = vi.fn(async () => ({ login_states: STATES }));
  getAssignment = vi.fn(async (project: string) => ({ project, name: "" }));
  setAssignment = vi.fn(async (project: string, name: string) => ({ project, name }));
  deleteState = vi.fn(async () => undefined);
  vi.spyOn(api, "upstashListLoginStates").mockImplementation(listStates);
  vi.spyOn(api, "upstashGetLoginState").mockImplementation(getAssignment);
  vi.spyOn(api, "upstashSetLoginState").mockImplementation(setAssignment);
  vi.spyOn(api, "upstashDeleteLoginState").mockImplementation(deleteState);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

/** Opens the assignment select. Radix opens on a click that is not a mouse pointer-up. */
function openSelect(): void {
  fireEvent.click(screen.getByRole("combobox"));
}

describe("UpstashLoginStates — table", () => {
  it("renders each saved state with counts, size and the holding session", async () => {
    render(<UpstashLoginStates project={PROJECT} />);

    const table = await screen.findByRole("table");
    const work = within(table).getByText("work").closest("tr") as HTMLElement;
    expect(within(work).getByText("12")).toBeTruthy();
    expect(within(work).getByText("3")).toBeTruthy();
    expect(within(work).getByText("2.0 KB")).toBeTruthy();
    expect(within(work).getByText("—")).toBeTruthy();

    const home = within(table).getByText("home").closest("tr") as HTMLElement;
    expect(within(home).getByText("sess_live_123")).toBeTruthy();
    expect(within(home).getByText("512 B")).toBeTruthy();
  });

  it("shows an unreadable state as unreadable, still offering Delete", async () => {
    render(<UpstashLoginStates project={PROJECT} />);

    const table = await screen.findByRole("table");
    const broken = within(table).getByText("broken").closest("tr") as HTMLElement;
    expect(within(broken).getByText("unreadable")).toBeTruthy();
    expect(within(broken).getByRole("button", { name: "Delete login state broken" })).toBeTruthy();
    expect(within(broken).queryByText("0")).toBeNull();
  });

  it("says so when no state is saved yet", async () => {
    listStates = vi.fn(async () => ({ login_states: [] }));
    vi.spyOn(api, "upstashListLoginStates").mockImplementation(listStates);
    render(<UpstashLoginStates project={PROJECT} />);
    expect(await screen.findByText("No login states saved yet.")).toBeTruthy();
  });
});

describe("UpstashLoginStates — assignment", () => {
  it("loads the project's assignment and shows it in the select", async () => {
    getAssignment = vi.fn(async (project: string) => ({ project, name: "work" }));
    vi.spyOn(api, "upstashGetLoginState").mockImplementation(getAssignment);
    render(<UpstashLoginStates project={PROJECT} />);

    await waitFor(() => expect(getAssignment).toHaveBeenCalledWith(PROJECT));
    await waitFor(() => expect(screen.getByRole("combobox").textContent).toBe("work"));
  });

  it("assigning a saved state calls PUT with the active project and the name", async () => {
    render(<UpstashLoginStates project={PROJECT} />);
    await screen.findByRole("table");

    openSelect();
    fireEvent.click(await screen.findByRole("option", { name: "work" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(setAssignment).toHaveBeenCalledWith(PROJECT, "work"));
  });
});

describe("UpstashLoginStates — new name", () => {
  it("blocks an invalid new name, shows the rule, and sends nothing", async () => {
    render(<UpstashLoginStates project={PROJECT} />);
    await screen.findByRole("table");

    openSelect();
    fireEvent.click(await screen.findByRole("option", { name: "New name…" }));
    fireEvent.change(screen.getByLabelText("New login state name"), { target: { value: "Bad Name" } });

    expect(screen.getByRole("alert").textContent).toContain("Use 1 to 40 characters");
    expect((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(setAssignment).not.toHaveBeenCalled();
  });

  it("refuses the reserved name none", async () => {
    render(<UpstashLoginStates project={PROJECT} />);
    await screen.findByRole("table");

    openSelect();
    fireEvent.click(await screen.findByRole("option", { name: "New name…" }));
    fireEvent.change(screen.getByLabelText("New login state name"), { target: { value: "none" } });

    expect(screen.getByRole("alert").textContent).toContain("reserved");
    expect((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("a valid new name is sent with the active project", async () => {
    render(<UpstashLoginStates project={PROJECT} />);
    await screen.findByRole("table");

    openSelect();
    fireEvent.click(await screen.findByRole("option", { name: "New name…" }));
    fireEvent.change(screen.getByLabelText("New login state name"), { target: { value: "  fresh-1 " } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(setAssignment).toHaveBeenCalledWith(PROJECT, "fresh-1"));
  });
});

describe("UpstashLoginStates — delete", () => {
  it("asks first, deletes after confirmation, and then reloads the list", async () => {
    render(<UpstashLoginStates project={PROJECT} />);
    await screen.findByRole("table");

    fireEvent.click(screen.getByRole("button", { name: "Delete login state home" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Delete login state home?")).toBeTruthy();
    expect(deleteState).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(deleteState).toHaveBeenCalledWith("home"));
    await waitFor(() => expect(listStates).toHaveBeenCalledTimes(2));
  });

  it("shows the server's 409 message in the dialog and keeps it open", async () => {
    deleteState = vi.fn(async () => {
      throw new ApiError(
        'login state "work" is in use by session sess_live_123',
        409,
        { error: "in use", in_use_by: "sess_live_123" },
      );
    });
    vi.spyOn(api, "upstashDeleteLoginState").mockImplementation(deleteState);
    render(<UpstashLoginStates project={PROJECT} />);
    await screen.findByRole("table");

    fireEvent.click(screen.getByRole("button", { name: "Delete login state work" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    expect(await screen.findByText(/is in use by session sess_live_123/)).toBeTruthy();
    expect(screen.getByRole("dialog")).toBeTruthy();
  });
});

describe("UpstashLoginStates — no usable project", () => {
  it("renders the assignment row disabled when no project is open", async () => {
    render(<UpstashLoginStates />);
    await screen.findByRole("table");

    expect(screen.getByText("Open a project to assign a login state")).toBeTruthy();
    expect((screen.getByRole("combobox") as HTMLButtonElement).disabled).toBe(true);
    expect(getAssignment).not.toHaveBeenCalled();
  });

  it("renders the row disabled for a remote project, which the server does not assign", async () => {
    render(<UpstashLoginStates project={PROJECT} host="remote-box" />);
    await screen.findByRole("table");

    expect(screen.getByText(/Open a local project to assign one/)).toBeTruthy();
    expect((screen.getByRole("combobox") as HTMLButtonElement).disabled).toBe(true);
    expect(getAssignment).not.toHaveBeenCalled();
  });
});
