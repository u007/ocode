import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import SystemPermissionsForm from "./SystemPermissionsForm";
import { api } from "../../api/client";

vi.mock("../../api/client", () => ({
  api: {
    getSystemPermissions: vi.fn(),
    setSystemPermission: vi.fn(),
    deleteSystemPermission: vi.fn(),
    requestSystemPermissions: vi.fn(),
  },
}));

const mockGet = vi.mocked(api.getSystemPermissions);
const mockSet = vi.mocked(api.setSystemPermission);
const mockDelete = vi.mocked(api.deleteSystemPermission);
const mockRequestAll = vi.mocked(api.requestSystemPermissions);

const entries = [
  {
    id: "macos.full-disk-access",
    label: "Full Disk Access",
    detail: "Read protected locations.",
    kind: "category",
    platform: "darwin",
    supported: true,
    status: "granted",
    enabled: true,
    requested: true,
    source: "builtin",
  },
  {
    id: "macos.files.documents",
    label: "Files & Folders — Documents",
    detail: "/Users/x/Documents",
    kind: "path",
    platform: "darwin",
    supported: true,
    status: "not_determined",
    enabled: false,
    requested: false,
    path: "/Users/x/Documents",
    source: "builtin",
  },
  {
    id: "path:/tmp/custom",
    label: "custom",
    detail: "/tmp/custom",
    kind: "path",
    platform: "darwin",
    supported: true,
    status: "denied",
    enabled: true,
    requested: true,
    path: "/tmp/custom",
    source: "custom",
  },
] as const;

function response(overrides: Record<string, unknown> = {}) {
  return {
    platform: "darwin",
    supported: true,
    entries: entries as unknown as never[],
    ...overrides,
  } as never;
}

beforeEach(() => {
  vi.clearAllMocks();
  mockGet.mockResolvedValue(response());
  mockSet.mockResolvedValue(
    response({
      result: {
        id: "macos.files.documents",
        status: "granted",
        message: "Access granted.",
        opened_settings: false,
      },
    }),
  );
  mockDelete.mockResolvedValue(response());
  mockRequestAll.mockResolvedValue(
    response({
      results: [
        {
          id: "macos.files.documents",
          status: "granted",
          message: "Access granted.",
          opened_settings: false,
        },
      ],
    }),
  );
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("SystemPermissionsForm", () => {
  it("renders entries with status badges and their persisted toggle state", async () => {
    render(<SystemPermissionsForm />);
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(1));

    expect(screen.getByTestId("sysperm-status-macos.full-disk-access").textContent).toBe("Granted");
    expect(screen.getByTestId("sysperm-status-macos.files.documents").textContent).toBe(
      "Not requested",
    );
    const toggles = screen.getAllByRole("checkbox") as HTMLInputElement[];
    expect(toggles[0].checked).toBe(true);
    expect(toggles[1].checked).toBe(false);
  });

  it("enabling an entry persists it and shows the request result", async () => {
    render(<SystemPermissionsForm />);
    await waitFor(() => expect(mockGet).toHaveBeenCalled());

    const toggles = screen.getAllByRole("checkbox") as HTMLInputElement[];
    fireEvent.click(toggles[1]);

    await waitFor(() =>
      expect(mockSet).toHaveBeenCalledWith({ id: "macos.files.documents", enabled: true }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("system-permissions-message").textContent).toContain(
        "Access granted.",
      ),
    );
  });

  it("request-all reconciles with no id and surfaces the results", async () => {
    render(<SystemPermissionsForm />);
    await waitFor(() => expect(mockGet).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: /Request all enabled/i }));

    await waitFor(() => expect(mockRequestAll).toHaveBeenCalledWith());
    await waitFor(() =>
      expect(screen.getByTestId("system-permissions-message").textContent).toContain(
        "Access granted.",
      ),
    );
  });

  it("adds a custom path", async () => {
    render(<SystemPermissionsForm />);
    await waitFor(() => expect(mockGet).toHaveBeenCalled());

    fireEvent.change(screen.getByTestId("system-permissions-path-input"), {
      target: { value: "/tmp/proj" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Add path/i }));

    await waitFor(() =>
      expect(mockSet).toHaveBeenCalledWith({ path: "/tmp/proj", enabled: true }),
    );
  });

  it("removes a custom path", async () => {
    render(<SystemPermissionsForm />);
    await waitFor(() => expect(mockGet).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: /Remove/i }));

    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith("path:/tmp/custom"));
  });
});
