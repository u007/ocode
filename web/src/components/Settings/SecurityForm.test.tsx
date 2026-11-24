import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import SecurityForm from "./SecurityForm";

const getMaskAdvanced = vi.fn();
const setMaskEnabled = vi.fn();
const setMaskMode = vi.fn();
const setMaskModel = vi.fn();
const setMaskAdvanced = vi.fn();

vi.mock("../Layout/ModelDialog", () => ({ default: () => null }));

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getMaskAdvanced: (...args: unknown[]) => getMaskAdvanced(...args),
      setMaskEnabled: (...args: unknown[]) => setMaskEnabled(...args),
      setMaskMode: (...args: unknown[]) => setMaskMode(...args),
      setMaskModel: (...args: unknown[]) => setMaskModel(...args),
      setMaskAdvanced: (...args: unknown[]) => setMaskAdvanced(...args),
    },
  };
});

const baseConfig = {
  enabled: false,
  mode: "lenient",
  model: "",
  base_url: "",
  fail_mode: "warn",
  allow_remote_tier2: false,
  custom_words: [],
  pii_categories: [],
};

describe("SecurityForm PII groups", () => {
  beforeEach(() => {
    getMaskAdvanced.mockReset();
    setMaskEnabled.mockReset().mockResolvedValue({});
    setMaskMode.mockReset().mockResolvedValue({});
    setMaskModel.mockReset().mockResolvedValue({});
    setMaskAdvanced.mockReset().mockResolvedValue({});
  });

  it("shows the six opt-in PII checkboxes unchecked, with high-noise warnings", async () => {
    getMaskAdvanced.mockResolvedValue({ ...baseConfig });

    render(<SecurityForm />);

    const boxes = await screen.findAllByLabelText(/^PII /);
    expect(boxes).toHaveLength(6);
    for (const box of boxes) {
      expect((box as HTMLInputElement).checked).toBe(false);
    }
    expect(screen.getAllByText("high-noise")).toHaveLength(2);
  });

  it("sends pii_categories when a group is toggled and saved", async () => {
    getMaskAdvanced.mockResolvedValue({ ...baseConfig });

    render(<SecurityForm />);

    fireEvent.click(await screen.findByLabelText("PII contact"));
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(setMaskAdvanced).toHaveBeenCalledWith(
        expect.objectContaining({ pii_categories: ["contact"] }),
      );
    });
  });

  it("round-trips an already-enabled group from the server", async () => {
    getMaskAdvanced.mockResolvedValue({
      ...baseConfig,
      pii_categories: ["contact", "legal"],
    });

    render(<SecurityForm />);

    await waitFor(() => {
      expect((screen.getByLabelText("PII contact") as HTMLInputElement).checked).toBe(true);
      expect((screen.getByLabelText("PII legal") as HTMLInputElement).checked).toBe(true);
    });
  });
});
