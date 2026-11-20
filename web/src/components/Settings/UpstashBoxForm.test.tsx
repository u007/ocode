import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { api } from "../../api/client";
import type { UpstashAccount } from "../../api/types";
import UpstashBoxForm from "./UpstashBoxForm";

// Settings → Upstash Box. What these tests protect, in order of blast radius:
//  1. The raw API key never reaches the DOM; only the server's mask does.
//  2. Editing without typing a key must not send `api_key`, or the stored key is
//     overwritten with an empty string.
//  3. Bounds are enforced before a request is sent (hard cap 1..4320, idle >= 1).
//  4. Delete is destructive, so it asks first.
//  5. Upstash cannot be chosen as the default backend while there are no accounts.

const ACCOUNTS: UpstashAccount[] = [
  {
    id: "acc-1",
    name: "Work",
    key_masked: "upst••••9f2c",
    size: "medium",
    idle_minutes: 15,
    hard_cap_minutes: 120,
    retain_days: 14,
    is_default: true,
  },
  {
    id: "acc-2",
    name: "Personal",
    key_masked: "upst••••7a10",
    size: "small",
    idle_minutes: 10,
    hard_cap_minutes: 60,
    retain_days: 7,
    is_default: false,
  },
];

let listAccounts: Mock<typeof api.upstashListAccounts>;
let createAccount: Mock<typeof api.upstashCreateAccount>;
let updateAccount: Mock<typeof api.upstashUpdateAccount>;
let deleteAccount: Mock<typeof api.upstashDeleteAccount>;
let updateSettings: Mock<typeof api.upstashUpdateSettings>;

function mockList(accounts: UpstashAccount[], defaultBackend: "local" | "upstash" = "local") {
  listAccounts.mockImplementation(async () => ({
    accounts,
    default_backend: defaultBackend,
    total: accounts.length,
  }));
}

beforeEach(() => {
  listAccounts = vi.fn(async () => ({ accounts: ACCOUNTS, default_backend: "local" as const, total: 2 }));
  createAccount = vi.fn(async () => ACCOUNTS[0]);
  updateAccount = vi.fn(async () => ACCOUNTS[0]);
  deleteAccount = vi.fn(async () => undefined);
  updateSettings = vi.fn(async () => ({ default_backend: "local" as const, default_account_id: "acc-1" }));
  vi.spyOn(api, "upstashListAccounts").mockImplementation(listAccounts);
  vi.spyOn(api, "upstashCreateAccount").mockImplementation(createAccount);
  vi.spyOn(api, "upstashUpdateAccount").mockImplementation(updateAccount);
  vi.spyOn(api, "upstashDeleteAccount").mockImplementation(deleteAccount);
  vi.spyOn(api, "upstashUpdateSettings").mockImplementation(updateSettings);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("UpstashBoxForm — listing", () => {
  it("renders the empty state with a sentence and an Add button", async () => {
    mockList([]);
    render(<UpstashBoxForm />);

    expect(await screen.findByText("No Upstash account is set up.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add" })).toBeTruthy();
  });

  it("lists accounts from GET and never shows a raw key", async () => {
    // The server contract never returns a raw key. Put a sentinel in the object
    // anyway, so a regression that renders extra fields would show up here.
    const leaky = [{ ...ACCOUNTS[0], api_key: "RAW-KEY-SENTINEL-123" }] as unknown as UpstashAccount[];
    mockList(leaky);
    render(<UpstashBoxForm />);

    const list = await screen.findByRole("list", { name: "Upstash accounts" });
    expect(within(list).getByText("Work")).toBeTruthy();
    expect(within(list).getByText("upst••••9f2c")).toBeTruthy();
    expect(within(list).getByText("Default")).toBeTruthy();
    expect(document.body.textContent).not.toContain("RAW-KEY-SENTINEL-123");
  });

  it("offers Make default only on the non-default account and sends the account id", async () => {
    mockList(ACCOUNTS);
    render(<UpstashBoxForm />);

    await screen.findByRole("list", { name: "Upstash accounts" });
    expect(screen.queryByRole("button", { name: "Make default for Work" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Make default for Personal" }));
    await waitFor(() =>
      expect(updateSettings).toHaveBeenCalledWith({ default_account_id: "acc-2" }),
    );
  });
});

describe("UpstashBoxForm — form", () => {
  it("rejects hard cap 4321 and idle 0 before sending anything", async () => {
    mockList([]);
    render(<UpstashBoxForm />);
    await screen.findByText("No Upstash account is set up.");

    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Team" } });
    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "upstash-key" } });
    fireEvent.change(screen.getByLabelText("Idle timeout (min)"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("Hard cap (min)"), { target: { value: "4321" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    // Both fields carry the same range message, so check each field's own error.
    await waitFor(() =>
      expect(document.getElementById("upstash-idle-error")?.textContent).toBe(
        "Enter whole minutes from 1 to 4320.",
      ),
    );
    expect(document.getElementById("upstash-hardCap-error")?.textContent).toBe(
      "Enter whole minutes from 1 to 4320.",
    );
    const hardCap = screen.getByLabelText("Hard cap (min)");
    expect(hardCap.getAttribute("aria-invalid")).toBe("true");
    expect(hardCap.getAttribute("aria-describedby")).toContain("upstash-hardCap-error");
    expect(createAccount).not.toHaveBeenCalled();
  });

  it("rejects idle longer than the hard cap, and retain 366 days", async () => {
    mockList([]);
    render(<UpstashBoxForm />);
    await screen.findByText("No Upstash account is set up.");

    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Team" } });
    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "upstash-key" } });
    fireEvent.change(screen.getByLabelText("Idle timeout (min)"), { target: { value: "200" } });
    fireEvent.change(screen.getByLabelText("Hard cap (min)"), { target: { value: "120" } });
    fireEvent.change(screen.getByLabelText("Delete retained boxes after (days)"), {
      target: { value: "366" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("The idle timeout cannot be longer than the hard cap.")).toBeTruthy();
    expect(screen.getByText("Enter whole days from 1 to 365.")).toBeTruthy();
    expect(createAccount).not.toHaveBeenCalled();
  });

  it("rejects an API key with spaces or longer than 512 characters", async () => {
    mockList([]);
    render(<UpstashBoxForm />);
    await screen.findByText("No Upstash account is set up.");

    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Team" } });
    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "has a space" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("An API key cannot contain spaces.")).toBeTruthy();

    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "k".repeat(513) } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(
      await screen.findByText("An API key must be 512 characters or fewer."),
    ).toBeTruthy();
    expect(createAccount).not.toHaveBeenCalled();
  });

  it("clears the typed key when the dialog is cancelled", async () => {
    mockList([]);
    render(<UpstashBoxForm />);
    await screen.findByText("No Upstash account is set up.");

    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "upstash-key-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByLabelText("API key")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    expect((screen.getByLabelText("API key") as HTMLInputElement).value).toBe("");
  });

  it("clears the typed key after a successful save, so Edit shows only the mask", async () => {
    mockList(ACCOUNTS);
    render(<UpstashBoxForm />);
    await screen.findByRole("list", { name: "Upstash accounts" });

    fireEvent.click(screen.getByRole("button", { name: "Edit Work" }));
    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "new-secret-key" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(updateAccount).toHaveBeenCalledTimes(1));
    expect(updateAccount.mock.calls[0][1]).toMatchObject({ api_key: "new-secret-key" });
    await waitFor(() => expect(screen.queryByLabelText("API key")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "Edit Work" }));
    const keyInput = screen.getByLabelText("API key") as HTMLInputElement;
    expect(keyInput.value).toBe("");
    expect(keyInput.placeholder).toBe("upst••••9f2c");
    expect(document.body.textContent).not.toContain("new-secret-key");
  });

  it("editing without typing a key sends no api_key field", async () => {
    mockList(ACCOUNTS);
    render(<UpstashBoxForm />);
    await screen.findByRole("list", { name: "Upstash accounts" });

    fireEvent.click(screen.getByRole("button", { name: "Edit Work" }));
    const keyInput = screen.getByLabelText("API key") as HTMLInputElement;
    expect(keyInput.value).toBe("");
    expect(keyInput.placeholder).toBe("upst••••9f2c");

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(updateAccount).toHaveBeenCalledTimes(1));
    const [id, body] = updateAccount.mock.calls[0];
    expect(id).toBe("acc-1");
    expect(body).not.toHaveProperty("api_key");
    expect(body).toMatchObject({ name: "Work", size: "medium", idle_minutes: 15, hard_cap_minutes: 120, retain_days: 14 });
  });
});

describe("UpstashBoxForm — delete", () => {
  it("asks for confirmation before calling DELETE", async () => {
    mockList(ACCOUNTS);
    render(<UpstashBoxForm />);
    await screen.findByRole("list", { name: "Upstash accounts" });

    fireEvent.click(screen.getByRole("button", { name: "Delete Personal" }));

    expect(await screen.findByText("Delete Personal?")).toBeTruthy();
    expect(deleteAccount).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(deleteAccount).toHaveBeenCalledWith("acc-2"));
  });
});

describe("UpstashBoxForm — default backend", () => {
  it("disables the Upstash choice while there are zero accounts", async () => {
    mockList([]);
    render(<UpstashBoxForm />);
    await screen.findByText("No Upstash account is set up.");

    const upstash = screen.getByRole("button", { name: "Upstash" }) as HTMLButtonElement;
    expect(upstash.disabled).toBe(true);
    expect(screen.getByText("Add an account before choosing Upstash.")).toBeTruthy();
  });

  it("shows Local as the effective choice when a stale upstash value has no accounts", async () => {
    // The server resets the backend to local when the last account is deleted,
    // but a stale "upstash" value must still not show Upstash as selected.
    mockList([], "upstash");
    render(<UpstashBoxForm />);
    await screen.findByText("No Upstash account is set up.");

    const local = screen.getByRole("button", { name: "Local" });
    const upstash = screen.getByRole("button", { name: "Upstash" }) as HTMLButtonElement;
    expect(local.getAttribute("aria-pressed")).toBe("true");
    expect(upstash.getAttribute("aria-pressed")).toBe("false");
    expect(upstash.disabled).toBe(true);
  });

  it("enables the Upstash choice once an account exists", async () => {
    mockList(ACCOUNTS);
    render(<UpstashBoxForm />);
    await screen.findByRole("list", { name: "Upstash accounts" });

    const upstash = screen.getByRole("button", { name: "Upstash" }) as HTMLButtonElement;
    expect(upstash.disabled).toBe(false);
    fireEvent.click(upstash);
    await waitFor(() =>
      expect(updateSettings).toHaveBeenCalledWith({ default_backend: "upstash" }),
    );
  });
});
