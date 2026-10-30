import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import FeaturesForm from "./FeaturesForm";

// Only the two feature-config calls are stubbed; the rest of the api module
// stays real so the component's own import graph is the production one.
const getFeaturesConfig = vi.fn();
const setFeaturesConfig = vi.fn();
vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getFeaturesConfig: (...args: unknown[]) => getFeaturesConfig(...args),
      setFeaturesConfig: (...args: unknown[]) => setFeaturesConfig(...args),
    },
  };
});

describe("FeaturesForm suggested-responses toggle", () => {
  beforeEach(() => {
    getFeaturesConfig.mockReset();
    setFeaturesConfig.mockReset();
  });

  it("shows the toggle and saves the four feature flags together", async () => {
    getFeaturesConfig.mockResolvedValue({
      memory_enabled: true,
      doc_prompt_enabled: false,
      suggested_responses_enabled: true,
      communication_style_enabled: true,
    });
    setFeaturesConfig.mockResolvedValue({});

    render(<FeaturesForm />);

    const toggle = (await screen.findByLabelText(
      /Suggested responses enabled/i,
    )) as HTMLInputElement;
    expect(toggle.checked).toBe(true);

    fireEvent.click(toggle);
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(setFeaturesConfig).toHaveBeenCalledWith(true, false, false, true);
    });
  });

  it("reflects a disabled toggle from the server", async () => {
    getFeaturesConfig.mockResolvedValue({
      memory_enabled: true,
      doc_prompt_enabled: false,
      suggested_responses_enabled: false,
    });

    render(<FeaturesForm />);

    const toggle = (await screen.findByLabelText(
      /Suggested responses enabled/i,
    )) as HTMLInputElement;
    expect(toggle.checked).toBe(false);
  });

  it("defaults to enabled when an older server does not report the flag", async () => {
    // An older server omits the key entirely. Reading it as "off" would make
    // the toggle lie about the server's actual default.
    getFeaturesConfig.mockResolvedValue({
      memory_enabled: true,
      doc_prompt_enabled: false,
    });

    render(<FeaturesForm />);

    const toggle = (await screen.findByLabelText(
      /Suggested responses enabled/i,
    )) as HTMLInputElement;
    expect(toggle.checked).toBe(true);
  });

  it("ticks the communication-style toggle by default when the server omits it", async () => {
    getFeaturesConfig.mockResolvedValue({
      memory_enabled: true,
      doc_prompt_enabled: false,
    });

    render(<FeaturesForm />);

    const toggle = (await screen.findByLabelText(
      /Communication style standard enabled/i,
    )) as HTMLInputElement;
    expect(toggle.checked).toBe(true);
  });

  it("reflects a disabled communication-style toggle and saves the change", async () => {
    getFeaturesConfig.mockResolvedValue({
      memory_enabled: true,
      doc_prompt_enabled: false,
      suggested_responses_enabled: true,
      communication_style_enabled: false,
    });
    setFeaturesConfig.mockResolvedValue({});

    render(<FeaturesForm />);

    const toggle = (await screen.findByLabelText(
      /Communication style standard enabled/i,
    )) as HTMLInputElement;
    expect(toggle.checked).toBe(false);

    fireEvent.click(toggle);
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(setFeaturesConfig).toHaveBeenCalledWith(true, false, true, true);
    });
  });
});
