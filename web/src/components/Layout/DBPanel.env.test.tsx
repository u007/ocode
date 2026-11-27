import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import DBPanel from "./DBPanel";
import { api } from "../../api/client";

vi.mock("../../api/client", () => {
  class ApiError extends Error {
    readonly status: number;
    constructor(message: string, status: number) {
      super(message);
      this.name = "ApiError";
      this.status = status;
    }
  }
  return {
    ApiError,
    api: {
      dbConnectList: vi.fn(),
      dbConnectAdd: vi.fn(),
      dbConnectRemove: vi.fn(),
      dbConnectUnlock: vi.fn(),
      dbConnectLock: vi.fn(),
      dbConnectTables: vi.fn(),
      dbConnectQuery: vi.fn(),
      dbConnectRows: vi.fn(),
      dbConnectRow: vi.fn(),
    },
  };
});

const mockList = vi.mocked(api.dbConnectList);
const mockAdd = vi.mocked(api.dbConnectAdd);
const mockUnlock = vi.mocked(api.dbConnectUnlock);
const mockLock = vi.mocked(api.dbConnectLock);
const mockTables = vi.mocked(api.dbConnectTables);

const SURFACE_RE = /^db-panel::\/proj:[a-z0-9]+-\d+$/;

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  mockLock.mockResolvedValue(undefined);
  mockTables.mockResolvedValue({ tables: [], hasMore: false });
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});

afterEach(() => {
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

describe("DBPanel environment-backed connections", () => {
  it("marks an environment-backed connection with an env badge", async () => {
    mockList.mockResolvedValue({
      connections: [{ name: "prod", driver: "postgres", unlocked: true, fromEnv: true }],
    });
    render(<DBPanel projectPath="/proj" />);

    const badge = await screen.findByTitle(/read from this project's \.env/);
    expect(badge.textContent).toBe("env");
  });

  it("does not mark a literal connection as env-backed", async () => {
    mockList.mockResolvedValue({
      connections: [{ name: "prod", driver: "postgres", unlocked: true, fromEnv: false }],
    });
    render(<DBPanel projectPath="/proj" />);

    await screen.findByText("prod", { selector: "span" });
    expect(screen.queryByTitle(/read from this project's \.env/)).toBeNull();
  });

  it("loads an environment connection from the project .env with no password", async () => {
    mockList.mockResolvedValue({
      connections: [{ name: "env", driver: "postgres", unlocked: false, fromEnv: true }],
    });
    mockUnlock.mockResolvedValue({ unlocked: ["env"], failed: [] });
    render(<DBPanel projectPath="/proj" />);

    const load = await screen.findByRole("button", { name: /Load from \.env/ });
    expect(screen.queryByLabelText("Master password")).toBeNull();
    fireEvent.click(load);

    await waitFor(() => {
      expect(mockUnlock).toHaveBeenCalledWith(expect.stringMatching(SURFACE_RE), "", undefined, "/proj");
    });
  });

  it("saves an environment connection without a project target, then loads it with the project path", async () => {
    // A local project's add carries no project target (the saved connection is
    // global). The project path reaches the API on the "Load from .env" unlock,
    // which is where the ${DATABASE_URL} template is resolved.
    let saved = false;
    mockList.mockImplementation(async () => ({
      connections: saved ? [{ name: "env", driver: "postgres", unlocked: false, fromEnv: true }] : [],
    }));
    mockAdd.mockImplementation(async () => {
      saved = true;
      return { name: "env" };
    });
    mockUnlock.mockResolvedValue({ unlocked: ["env"], failed: [] });
    render(<DBPanel projectPath="/proj" />);

    await screen.findByText("Add a PostgreSQL connection");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "env" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), {
      target: { value: "${DATABASE_URL}" },
    });
    // An environment URL is not a secret: the password is not required and the
    // field is disabled rather than demanding an encryption password.
    expect((screen.getByLabelText(/Master password/) as HTMLInputElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }));

    await waitFor(() => {
      expect(mockAdd).toHaveBeenCalledWith("env", "${DATABASE_URL}", "", undefined, undefined, undefined);
    });

    const load = await screen.findByRole("button", { name: /Load from \.env/ });
    fireEvent.click(load);
    await waitFor(() => {
      expect(mockUnlock).toHaveBeenCalledWith(expect.stringMatching(SURFACE_RE), "", undefined, "/proj");
    });
  });

  it("sends a selected env file and templated tunnel ports", async () => {
    mockList.mockResolvedValue({ connections: [] });
    mockAdd.mockResolvedValue({ name: "staging" });
    render(<DBPanel projectPath="/proj" />);

    await screen.findByText("Add a PostgreSQL connection");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "staging" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), {
      target: { value: "${DATABASE_URL}" },
    });
    fireEvent.change(screen.getByLabelText("Env file (optional)"), {
      target: { value: ".staging" },
    });
    fireEvent.click(screen.getByLabelText("Use SSH tunnel"));
    fireEvent.change(screen.getByLabelText("SSH host"), { target: { value: "${SSH_HOST}" } });
    fireEvent.change(screen.getByLabelText("SSH port (optional)"), {
      target: { value: "${SSH_PORT}" },
    });
    fireEvent.change(screen.getByLabelText("Remote DB host"), {
      target: { value: "db.internal" },
    });
    fireEvent.change(screen.getByLabelText("Remote DB port"), {
      target: { value: "${DB_PORT}" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }));

    await waitFor(() => {
      expect(mockAdd).toHaveBeenCalledWith(
        "staging",
        "${DATABASE_URL}",
        "",
        undefined,
        expect.objectContaining({
          host: "${SSH_HOST}",
          port_env: "${SSH_PORT}",
          remote_port_env: "${DB_PORT}",
          port: undefined,
          remote_port: undefined,
        }),
        ".staging",
      );
    });
  });

  it("shows the selected env file in the unlock hint", async () => {
    mockList.mockResolvedValue({
      connections: [
        { name: "staging", driver: "postgres", unlocked: false, fromEnv: true, envFile: ".env.staging" },
      ],
    });
    render(<DBPanel projectPath="/proj" />);

    expect(await screen.findByText(/reads its URL from the/)).toHaveTextContent(".env.staging");
  });

  it("explains that the URL may come from the environment", async () => {
    mockList.mockResolvedValue({ connections: [] });
    render(<DBPanel projectPath="/proj" />);

    await screen.findByText("Add a PostgreSQL connection");
    const hint = screen.getByText(/read the URL from the/);
    expect(hint.textContent).toContain(".env");
    expect(hint.textContent).toContain("A value exported in the server environment wins unless an env file is selected.");
  });

  it("does not send an env file typed under a template URL once the URL is literal", async () => {
    mockList.mockResolvedValue({ connections: [] });
    mockAdd.mockResolvedValue({ name: "stage" });
    mockUnlock.mockResolvedValue({ unlocked: [], failed: [] });
    render(<DBPanel projectPath="/proj" />);

    await screen.findByText("Add a PostgreSQL connection");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "stage" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), { target: { value: "${DATABASE_URL}" } });
    fireEvent.change(screen.getByLabelText("Env file (optional)"), { target: { value: ".staging" } });
    // Switching to a literal URL hides the env file field; its typed value must not ride along.
    fireEvent.change(screen.getByLabelText("Connection URL"), { target: { value: "postgres://app:pw@db/app" } });
    expect(screen.queryByLabelText("Env file (optional)")).toBeNull();
    fireEvent.change(screen.getByLabelText("Create master password"), { target: { value: "master" } });
    fireEvent.change(screen.getByLabelText("Confirm master password"), { target: { value: "master" } });
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }));

    await waitFor(() => {
      expect(mockAdd).toHaveBeenCalledTimes(1);
    });
    expect(mockAdd.mock.calls[0][0]).toBe("stage");
    expect(mockAdd.mock.calls[0][1]).toBe("postgres://app:pw@db/app");
    expect(mockAdd.mock.calls[0][5]).toBeUndefined();
  });

  it("blocks Save and explains the error inline when a literal URL has a ${VAR} tunnel field", async () => {
    const conflict =
      "SSH tunnel fields that use ${VAR} need a template URL (with an env file); use literal tunnel values with a literal URL";
    mockList.mockResolvedValue({ connections: [] });
    mockAdd.mockResolvedValue({ name: "stage" });
    mockUnlock.mockResolvedValue({ unlocked: [], failed: [] });
    render(<DBPanel projectPath="/proj" />);

    await screen.findByText("Add a PostgreSQL connection");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "stage" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), { target: { value: "postgres://app:pw@db/app" } });
    fireEvent.change(screen.getByLabelText("Create master password"), { target: { value: "master" } });
    fireEvent.change(screen.getByLabelText("Confirm master password"), { target: { value: "master" } });
    fireEvent.click(screen.getByLabelText("Use SSH tunnel"));
    fireEvent.change(screen.getByLabelText("SSH host"), { target: { value: "${SSH_HOST}" } });

    expect(screen.getByText(conflict)).toBeTruthy();
    const save = screen.getByRole("button", { name: "Save connection" }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);

    // A literal host clears the conflict and Save comes back.
    fireEvent.change(screen.getByLabelText("SSH host"), { target: { value: "bastion.example" } });
    expect(screen.queryByText(conflict)).toBeNull();
    expect((screen.getByRole("button", { name: "Save connection" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("blocks Save with an inline message when a template URL's ${VAR} tunnel has no env file, and typing one clears both", async () => {
    const needsFile = "SSH tunnel fields that use ${VAR} need an env file";
    mockList.mockResolvedValue({ connections: [] });
    mockAdd.mockResolvedValue({ name: "staging" });
    render(<DBPanel projectPath="/proj" />);

    await screen.findByText("Add a PostgreSQL connection");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "staging" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), { target: { value: "${DATABASE_URL}" } });
    fireEvent.click(screen.getByLabelText("Use SSH tunnel"));
    fireEvent.change(screen.getByLabelText("SSH host"), { target: { value: "${SSH_HOST}" } });

    expect(screen.getByText(needsFile)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Save connection" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("Env file (optional)"), { target: { value: ".staging" } });
    expect(screen.queryByText(needsFile)).toBeNull();
    expect((screen.getByRole("button", { name: "Save connection" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("does not require an env file for a template URL whose tunnel holds only literal values", async () => {
    mockList.mockResolvedValue({ connections: [] });
    render(<DBPanel projectPath="/proj" />);

    await screen.findByText("Add a PostgreSQL connection");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "staging" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), { target: { value: "${DATABASE_URL}" } });
    fireEvent.click(screen.getByLabelText("Use SSH tunnel"));
    fireEvent.change(screen.getByLabelText("SSH host"), { target: { value: "bastion.example" } });

    expect(screen.queryByText(/need an env file/)).toBeNull();
    expect((screen.getByRole("button", { name: "Save connection" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("also blocks a literal URL when only a tunnel port is a ${VAR} reference", async () => {
    mockList.mockResolvedValue({ connections: [] });
    render(<DBPanel projectPath="/proj" />);

    await screen.findByText("Add a PostgreSQL connection");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "stage" } });
    fireEvent.change(screen.getByLabelText("Connection URL"), { target: { value: "postgres://app:pw@db/app" } });
    fireEvent.change(screen.getByLabelText("Create master password"), { target: { value: "master" } });
    fireEvent.change(screen.getByLabelText("Confirm master password"), { target: { value: "master" } });
    fireEvent.click(screen.getByLabelText("Use SSH tunnel"));
    fireEvent.change(screen.getByLabelText("SSH host"), { target: { value: "bastion.example" } });
    fireEvent.change(screen.getByLabelText("Remote DB host"), { target: { value: "db.internal" } });
    fireEvent.change(screen.getByLabelText("SSH port (optional)"), { target: { value: "${SSH_PORT}" } });

    expect(screen.getByText(/SSH tunnel fields that use \$\{VAR\} need a template URL/)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Save connection" }) as HTMLButtonElement).disabled).toBe(true);
  });
});
