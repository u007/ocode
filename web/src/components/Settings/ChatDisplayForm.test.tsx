import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import ChatDisplayForm from "./ChatDisplayForm";
import { api } from "../../api/client";
import type { ChatVerbosityConfig } from "../../api/types";
import { __resetChatVerbosityForTests } from "../../lib/chatVerbosity";

vi.mock("../../api/client", () => ({
  api: {
    getChatVerbosityConfig: vi.fn(),
    setChatVerbosityConfig: vi.fn(),
  },
}));

vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: () => () => {},
    onReconnect: () => () => {},
  },
}));

const mockGet = vi.mocked(api.getChatVerbosityConfig);
const mockSet = vi.mocked(api.setChatVerbosityConfig);

const balancedConfig: ChatVerbosityConfig = {
  preset: "balanced" as const,
  overrides: {
    older_thinking: "collapsed" as const,
    tool_calls: "expanded" as const,
    tool_output: "preset" as const,
    activity_notices: "expanded" as const,
  },
};

beforeEach(() => {
  vi.clearAllMocks();
  __resetChatVerbosityForTests();
  mockGet.mockResolvedValue(balancedConfig);
  mockSet.mockResolvedValue(balancedConfig);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ChatDisplayForm", () => {
  it("loads and renders the persisted preset and overrides", async () => {
    render(<ChatDisplayForm />);
    await waitFor(() => expect((screen.getByRole("radio", { name: /Balanced/ }) as HTMLInputElement).checked).toBe(true));
    expect((screen.getByLabelText("Older thinking") as HTMLSelectElement).value).toBe("collapsed");
    expect((screen.getByLabelText("Tool call details") as HTMLSelectElement).value).toBe("expanded");
    expect((screen.getByLabelText("Tool output") as HTMLSelectElement).value).toBe("preset");
    expect((screen.getByLabelText("Activity notices") as HTMLSelectElement).value).toBe("expanded");
  });

  it("resets only the category overrides, leaving the preset selected", async () => {
    render(<ChatDisplayForm />);
    await waitFor(() => expect((screen.getByLabelText("Older thinking") as HTMLSelectElement).value).toBe("collapsed"));

    fireEvent.click(screen.getByRole("button", { name: "Reset overrides" }));

    expect((screen.getByLabelText("Older thinking") as HTMLSelectElement).value).toBe("preset");
    expect((screen.getByLabelText("Activity notices") as HTMLSelectElement).value).toBe("preset");
    expect((screen.getByRole("radio", { name: /Balanced/ }) as HTMLInputElement).checked).toBe(true);
  });

  it("saves the exact full config and disables the button while saving", async () => {
    let release: (value: ChatVerbosityConfig) => void = () => {};
    mockSet.mockImplementation(
      () =>
        new Promise<ChatVerbosityConfig>((resolve) => {
          release = resolve;
        }),
    );

    render(<ChatDisplayForm />);
    await waitFor(() =>
      expect((screen.getByRole("radio", { name: /Balanced/ }) as HTMLInputElement).checked).toBe(true),
    );

    const quiet = screen.getByRole("radio", { name: /Quiet/ }) as HTMLInputElement;
    fireEvent.click(quiet);
    await waitFor(() => expect(quiet.checked).toBe(true));
    fireEvent.change(screen.getByLabelText("Tool output"), { target: { value: "collapsed" } });

    const save = screen.getByRole("button", { name: "Save changes" });
    fireEvent.click(save);
    expect((save as HTMLButtonElement).disabled).toBe(true);

    release({
      preset: "quiet",
      overrides: {
        older_thinking: "collapsed",
        tool_calls: "collapsed",
        tool_output: "collapsed",
        activity_notices: "collapsed",
      },
    });

    await waitFor(() =>
      expect(mockSet).toHaveBeenCalledWith({
        preset: "quiet",
        overrides: {
          older_thinking: "collapsed",
          tool_calls: "expanded",
          tool_output: "collapsed",
          activity_notices: "expanded",
        },
      }),
    );
  });

  it("shows an inline alert on save failure", async () => {
    mockSet.mockRejectedValue(new Error("config is read-only"));
    render(<ChatDisplayForm />);
    await waitFor(() => expect(mockGet).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("config is read-only");
  });

  it("shows an inline alert when the initial load fails and stays usable", async () => {
    mockGet.mockRejectedValue(new Error("chat_verbosity.overrides.older_thinking must be one of preset, expanded, collapsed"));
    render(<ChatDisplayForm />);

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("older_thinking");
    expect(screen.getByRole("radio", { name: /Full/ })).toBeInTheDocument();
  });

  it("renders no alert on a successful save", async () => {
    render(<ChatDisplayForm />);
    await waitFor(() => expect(mockGet).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(mockSet).toHaveBeenCalled());
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
