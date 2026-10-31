import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import PathsForm from "./PathsForm";

const getPathsConfig = vi.fn();
const setPathsConfig = vi.fn();
vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getPathsConfig: (...args: unknown[]) => getPathsConfig(...args),
      setPathsConfig: (...args: unknown[]) => setPathsConfig(...args),
    },
  };
});

const EXCEL = "/Users/test/Library/Containers/com.microsoft.Excel/Data/Documents/wef";
const WORD = "/Users/test/Library/Containers/com.microsoft.Word/Data/Documents/wef";
const OUTLOOK = "/Users/test/Library/Containers/com.microsoft.Outlook/Data/Documents/wef";
// The stored grant can differ in form (tilde + trailing slash) from the preset path.
const EXCEL_STORED = "~/Library/Containers/com.microsoft.Excel/Data/Documents/wef/";

function preset(over: Partial<Record<string, unknown>> = {}) {
  return {
    app: "excel",
    label: "Excel",
    path: EXCEL,
    exists: true,
    available: true,
    allowed: false,
    ...over,
  };
}

function mockConfig(over: Record<string, unknown> = {}) {
  getPathsConfig.mockResolvedValue({
    extra_allowed_paths: [EXCEL_STORED, "/tmp/scratch"],
    upload_dir: "/tmp/uploads",
    platform: "darwin",
    office_addin_paths: [
      preset({ allowed: true, allowed_path: EXCEL_STORED }),
      preset({ app: "word", label: "Word", path: WORD, exists: false, allowed: false }),
    ],
    ...over,
  });
  setPathsConfig.mockImplementation(async (paths: string[], uploadDir: string) => ({
    extra_allowed_paths: paths,
    upload_dir: uploadDir,
  }));
}

const checkbox = (label: RegExp) => screen.findByLabelText(label) as Promise<HTMLInputElement>;

describe("PathsForm Office add-in presets", () => {
  beforeEach(() => {
    getPathsConfig.mockReset();
    setPathsConfig.mockReset();
  });

  it("shows the platform presets and keeps Office paths out of the free-text field", async () => {
    mockConfig();
    render(<PathsForm />);

    expect((await checkbox(/Grant Excel add-in folder access/)).checked).toBe(true);
    expect((await checkbox(/Grant Word add-in folder access/)).checked).toBe(false);
    // The already-granted Office path is rendered as a checkbox, not as text —
    // even when the stored entry is the tilde+trailing-slash form.
    expect(screen.getByDisplayValue("/tmp/scratch")).toBeTruthy();
    expect(screen.queryByDisplayValue(/~\//)).toBeNull();
    expect(screen.getByText(/Full Disk Access/)).toBeTruthy();
  });

  it("grants a ticked preset and preserves the other extra paths", async () => {
    mockConfig();
    render(<PathsForm />);

    const word = await checkbox(/Grant Word add-in folder access/);
    fireEvent.click(word);
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(setPathsConfig).toHaveBeenCalledWith(["/tmp/scratch", EXCEL, WORD], "/tmp/uploads");
    });
    expect(word.checked).toBe(true);
  });

  it("revokes a previously granted preset when unticked", async () => {
    mockConfig();
    render(<PathsForm />);

    const excel = await checkbox(/Grant Excel add-in folder access/);
    fireEvent.click(excel);
    fireEvent.click(await checkbox(/Grant Word add-in folder access/));
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(setPathsConfig).toHaveBeenCalledWith(["/tmp/scratch", WORD], "/tmp/uploads");
    });
  });

  it("disables a preset whose app is not installed", async () => {
    mockConfig({
      extra_allowed_paths: ["/tmp/scratch"],
      office_addin_paths: [
        preset({ app: "outlook", label: "Outlook", path: OUTLOOK, exists: false, available: false }),
      ],
    });
    render(<PathsForm />);

    const outlook = await checkbox(/Grant Outlook add-in folder access/);
    expect(outlook.disabled).toBe(true);
    expect(outlook.checked).toBe(false);
    expect(screen.getByText("Not installed")).toBeTruthy();
    // Even if a click slips through, an unavailable preset is never sent.
    fireEvent.click(outlook);
    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    await waitFor(() => {
      expect(setPathsConfig).toHaveBeenCalledWith(["/tmp/scratch"], "/tmp/uploads");
    });
  });

  it("lets an already-granted preset be revoked after its folder disappears", async () => {
    // `available` means "the parent directory exists NOW". A grant made earlier
    // survives the folder being removed, and it must still be revocable here —
    // blocking the uncheck left it removable only by hand-editing the config.
    mockConfig({
      extra_allowed_paths: ["/tmp/scratch", OUTLOOK],
      office_addin_paths: [
        preset({ app: "outlook", label: "Outlook", path: OUTLOOK, exists: false, available: false, allowed: true }),
      ],
    });
    render(<PathsForm />);

    const outlook = await checkbox(/Grant Outlook add-in folder access/);
    expect(outlook.disabled).toBe(false);
    expect(outlook.checked).toBe(true);

    fireEvent.click(outlook);
    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    await waitFor(() => {
      expect(setPathsConfig).toHaveBeenCalledWith(["/tmp/scratch"], "/tmp/uploads");
    });
  });

  it("points at manual entry when the platform has no preset", async () => {
    mockConfig({ platform: "linux", extra_allowed_paths: [], office_addin_paths: [] });
    render(<PathsForm />);

    expect(
      await screen.findByText(/No known Office add-in folder on this platform/),
    ).toBeTruthy();
    expect(screen.queryByText(/Full Disk Access/)).toBeNull();
  });
});
