import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import VaultForm from "./VaultForm";
import { api } from "../../api/client";

vi.mock("../../api/client", () => ({
  api: {
    vaultStatus: vi.fn(),
    vaultInit: vi.fn(),
    vaultUnlock: vi.fn(),
    vaultLock: vi.fn(),
    vaultList: vi.fn(),
    vaultReveal: vi.fn(),
    vaultCreate: vi.fn(),
    vaultUpdate: vi.fn(),
    vaultDelete: vi.fn(),
    vaultMatch: vi.fn(),
    vaultChangeMaster: vi.fn(),
    vaultGenerate: vi.fn(),
  },
}));

const mockStatus = vi.mocked(api.vaultStatus);
const mockInit = vi.mocked(api.vaultInit);
const mockUnlock = vi.mocked(api.vaultUnlock);
const mockLock = vi.mocked(api.vaultLock);
const mockList = vi.mocked(api.vaultList);
const mockReveal = vi.mocked(api.vaultReveal);
const mockCreate = vi.mocked(api.vaultCreate);
const mockDelete = vi.mocked(api.vaultDelete);
const mockGenerate = vi.mocked(api.vaultGenerate);

const sampleMeta = { id: "1", site: "Example", url: "https://example.com", title: "", username: "alice" };

beforeEach(() => {
  vi.clearAllMocks();
  mockStatus.mockResolvedValue({ exists: false, unlocked: false });
  mockList.mockResolvedValue({ items: [], total: 0 });
  mockInit.mockResolvedValue({ unlocked: true });
  mockUnlock.mockResolvedValue({ unlocked: true });
  mockLock.mockResolvedValue(undefined as never);
  mockDelete.mockResolvedValue(undefined as never);
  mockReveal.mockResolvedValue({
    ...sampleMeta,
    password: "s3cret",
    notes: "",
    created: "",
    updated: "",
  });
  mockCreate.mockResolvedValue({ ...sampleMeta, password: "pw", notes: "", created: "", updated: "" });
  mockGenerate.mockResolvedValue({ password: "generated-pw" });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("VaultForm", () => {
  it("creates a vault when none exists", async () => {
    render(<VaultForm />);

    const master = await screen.findByLabelText("Master password");
    fireEvent.change(master, { target: { value: "m" } });
    fireEvent.change(screen.getByLabelText("Confirm master password"), { target: { value: "m" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(mockInit).toHaveBeenCalledWith("m", "settings"));
  });

  it("shows an unlock prompt when locked", async () => {
    mockStatus.mockResolvedValue({ exists: true, unlocked: false });
    render(<VaultForm />);

    const master = await screen.findByLabelText("Master password");
    expect(screen.getByRole("button", { name: "Unlock" })).toBeTruthy();
    fireEvent.change(master, { target: { value: "m" } });
    fireEvent.click(screen.getByRole("button", { name: "Unlock" }));

    await waitFor(() => expect(mockUnlock).toHaveBeenCalledWith("m", "settings"));
  });

  it("lists items when unlocked", async () => {
    mockStatus.mockResolvedValue({ exists: true, unlocked: true });
    mockList.mockResolvedValue({ items: [sampleMeta], total: 1 });
    render(<VaultForm />);

    expect(await screen.findByText("Example")).toBeTruthy();
    expect(screen.getByText("alice")).toBeTruthy();
    expect(mockList).toHaveBeenCalledWith("settings", { sort: "site" });
  });

  it("add opens a dialog and saves a credential", async () => {
    mockStatus.mockResolvedValue({ exists: true, unlocked: true });
    render(<VaultForm />);
    await waitFor(() => expect(mockList).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    await screen.findByRole("dialog");

    fireEvent.change(screen.getByLabelText("Site"), { target: { value: "Example" } });
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "alice" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "pw" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mockCreate).toHaveBeenCalled());
    const [item, surface] = mockCreate.mock.calls[0];
    expect(surface).toBe("settings");
    expect(item).toMatchObject({ site: "Example", username: "alice", password: "pw" });
  });

  it("reveal shows the password", async () => {
    mockStatus.mockResolvedValue({ exists: true, unlocked: true });
    mockList.mockResolvedValue({ items: [sampleMeta], total: 1 });
    render(<VaultForm />);

    fireEvent.click(await screen.findByRole("button", { name: "Reveal" }));

    await waitFor(() => expect(mockReveal).toHaveBeenCalledWith("1", "settings"));
    expect(await screen.findByText("s3cret")).toBeTruthy();
  });

  it("delete removes the item", async () => {
    mockStatus.mockResolvedValue({ exists: true, unlocked: true });
    mockList.mockResolvedValue({ items: [sampleMeta], total: 1 });
    render(<VaultForm />);

    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));

    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith("1", "settings"));
  });

  it("generate fills the password field", async () => {
    mockStatus.mockResolvedValue({ exists: true, unlocked: true });
    render(<VaultForm />);
    await waitFor(() => expect(mockList).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    await screen.findByRole("dialog");

    fireEvent.click(screen.getByRole("button", { name: "Generate" }));

    await waitFor(() => expect(mockGenerate).toHaveBeenCalled());
    const password = screen.getByLabelText("Password") as HTMLInputElement;
    await waitFor(() => expect(password.value).toBe("generated-pw"));
  });

  it("lock relocks the surface", async () => {
    mockStatus.mockResolvedValue({ exists: true, unlocked: true });
    render(<VaultForm />);
    await waitFor(() => expect(mockList).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: "Lock" }));

    await waitFor(() => expect(mockLock).toHaveBeenCalledWith("settings"));
    expect(await screen.findByRole("button", { name: "Unlock" })).toBeTruthy();
  });
});
