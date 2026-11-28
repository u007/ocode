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
      dbConnectEnvFiles: vi.fn(),
    },
  };
});

const mockList = vi.mocked(api.dbConnectList);
const mockAdd = vi.mocked(api.dbConnectAdd);
const mockUnlock = vi.mocked(api.dbConnectUnlock);
const mockLock = vi.mocked(api.dbConnectLock);
const mockTables = vi.mocked(api.dbConnectTables);
const mockEnvFiles = vi.mocked(api.dbConnectEnvFiles);

const PROJECT_ENV_FILES = [".env", ".env.local", ".env.production", ".env.staging"];
const DEFAULT_OPTION = "Default (.env + .env.local)";

// Opens the env file picker and chooses one option. Radix opens on Enter; the
// pointer path needs hasPointerCapture, which jsdom does not provide.
async function pickEnvFile(option: string) {
  fireEvent.keyDown(screen.getByLabelText("Env file (optional)"), { key: "Enter" });
  fireEvent.click(await screen.findByRole("option", { name: option }));
}

// Fills the add form with a template URL and a name, ready to save.
async function fillTemplateForm(name: string) {
  await screen.findByText("Add a PostgreSQL connection");
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: name } });
  fireEvent.change(screen.getByLabelText("Connection URL"), { target: { value: "${DATABASE_URL}" } });
}

const SURFACE_RE = /^db-panel::\/proj:[a-z0-9]+-\d+$/;

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  mockLock.mockResolvedValue(undefined);
  mockTables.mockResolvedValue({ tables: [], hasMore: false });
  mockEnvFiles.mockResolvedValue(PROJECT_ENV_FILES);
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

    await fillTemplateForm("staging");
    await pickEnvFile(".env.staging");
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
        ".env.staging",
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
    expect(hint.textContent).toContain(
      "Connections saved to the project read only the project's env files, never the server environment. Global connections read the server environment first unless an env file is selected.",
    );
  });

  it("does not send an env file typed under a template URL once the URL is literal", async () => {
    mockList.mockResolvedValue({ connections: [] });
    mockAdd.mockResolvedValue({ name: "stage" });
    mockUnlock.mockResolvedValue({ unlocked: [], failed: [] });
    render(<DBPanel projectPath="/proj" />);

    await fillTemplateForm("stage");
    await pickEnvFile(".env.staging");
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

    await pickEnvFile(".env.staging");
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

describe("DBPanel env file picker", () => {
  it("lists the project's env files, with the default option first", async () => {
    mockList.mockResolvedValue({ connections: [] });
    render(<DBPanel projectPath="/proj" />);

    await fillTemplateForm("env");
    fireEvent.keyDown(screen.getByLabelText("Env file (optional)"), { key: "Enter" });

    const options = await screen.findAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual([DEFAULT_OPTION, ...PROJECT_ENV_FILES]);
    expect(mockEnvFiles).toHaveBeenCalledWith({ host: undefined, projectPath: "/proj" });
  });

  it("sends the chosen env file on add", async () => {
    mockList.mockResolvedValue({ connections: [] });
    mockAdd.mockResolvedValue({ name: "prod" });
    render(<DBPanel projectPath="/proj" />);

    await fillTemplateForm("prod");
    await pickEnvFile(".env.production");
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }));

    await waitFor(() => {
      expect(mockAdd).toHaveBeenCalledWith("prod", "${DATABASE_URL}", "", undefined, undefined, ".env.production");
    });
  });

  it("sends no env file when the default option is chosen", async () => {
    mockList.mockResolvedValue({ connections: [] });
    mockAdd.mockResolvedValue({ name: "env" });
    render(<DBPanel projectPath="/proj" />);

    await fillTemplateForm("env");
    await pickEnvFile(".env.local");
    await pickEnvFile(DEFAULT_OPTION);
    expect(screen.getByLabelText("Env file (optional)").textContent).toBe(DEFAULT_OPTION);
    fireEvent.click(screen.getByRole("button", { name: "Save connection" }));

    await waitFor(() => {
      expect(mockAdd).toHaveBeenCalledWith("env", "${DATABASE_URL}", "", undefined, undefined, undefined);
    });
  });

  it("shows a load failure inline and offers no free-text fallback", async () => {
    mockList.mockResolvedValue({ connections: [] });
    mockEnvFiles.mockRejectedValue(new Error("project has no readable env files"));
    render(<DBPanel projectPath="/proj" />);

    await fillTemplateForm("env");
    const error = await screen.findByText(/Could not load env files: project has no readable env files/);
    expect(error.className).toContain("text-destructive");
    expect(screen.getByLabelText("Env file (optional)").tagName).toBe("BUTTON");
    expect(screen.queryByPlaceholderText(/\.env\.production/)).toBeNull();
  });

  it("asks for a project and does not query the server when there is none", async () => {
    mockList.mockResolvedValue({ connections: [] });
    render(<DBPanel />);

    await fillTemplateForm("env");
    expect(await screen.findByText("Open a project to pick an env file")).toBeTruthy();
    expect((screen.getByLabelText("Env file (optional)") as HTMLButtonElement).disabled).toBe(true);
    expect(mockEnvFiles).not.toHaveBeenCalled();
  });
});
