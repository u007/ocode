import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import OpencodeMcpForm from "./OpencodeMcpForm";

const getMCP = vi.fn();
const setMCPEnabled = vi.fn();
const reloadMCPAndWait = vi.fn();
const connectElevenLabs = vi.fn();
const getMCPAuthStatus = vi.fn();

vi.mock("../../api/client", () => ({
  api: {
    getMCP: (...a: unknown[]) => getMCP(...a),
    setMCPEnabled: (...a: unknown[]) => setMCPEnabled(...a),
    reloadMCPAndWait: (...a: unknown[]) => reloadMCPAndWait(...a),
    connectElevenLabs: (...a: unknown[]) => connectElevenLabs(...a),
    getMCPAuthStatus: (...a: unknown[]) => getMCPAuthStatus(...a),
  },
}));

describe("OpencodeMcpForm ElevenLabs connect", () => {
  beforeEach(() => {
    getMCP.mockReset();
    setMCPEnabled.mockReset();
    reloadMCPAndWait.mockReset();
    connectElevenLabs.mockReset();
    getMCPAuthStatus.mockReset();
    getMCP.mockResolvedValue([]);
    connectElevenLabs.mockResolvedValue({
      job_id: "job-1",
      server: "elevenlabs",
      status: "running",
      browser_note: "Sign in with ElevenLabs",
    });
    getMCPAuthStatus.mockResolvedValue({ job_id: "job-1", server: "elevenlabs", status: "done" });
    reloadMCPAndWait.mockResolvedValue({ status: "reloaded", tools: 4, errors: [] });
  });

  it("shows the ElevenLabs card as not connected when no server is configured", async () => {
    render(<OpencodeMcpForm />);
    await waitFor(() => expect(screen.getByText("not connected")).toBeTruthy());
    expect(screen.getByText("Connect ElevenLabs")).toBeTruthy();
  });

  it("connects, polls the auth job, then reloads the MCP tool set", async () => {
    render(<OpencodeMcpForm />);
    await waitFor(() => expect(screen.getByText("Connect ElevenLabs")).toBeTruthy());

    fireEvent.click(screen.getByText("Connect ElevenLabs"));

    await waitFor(() => expect(reloadMCPAndWait).toHaveBeenCalledTimes(1));
    expect(connectElevenLabs).toHaveBeenCalledTimes(1);
    expect(getMCPAuthStatus).toHaveBeenCalledWith("job-1");
    await waitFor(() =>
      expect(screen.getByText("Connected. ElevenLabs tools are attached to new turns.")).toBeTruthy(),
    );
  });

  it("surfaces an authorization failure and does not reload", async () => {
    getMCPAuthStatus.mockResolvedValue({
      job_id: "job-1",
      server: "elevenlabs",
      status: "error",
      error: "oauth state mismatch",
    });
    render(<OpencodeMcpForm />);
    await waitFor(() => expect(screen.getByText("Connect ElevenLabs")).toBeTruthy());

    fireEvent.click(screen.getByText("Connect ElevenLabs"));

    await waitFor(() => expect(screen.getByText("oauth state mismatch")).toBeTruthy());
    expect(reloadMCPAndWait).not.toHaveBeenCalled();
  });

  // The preset is written before the flow starts, so a failed sign-in must
  // still refresh the list: the entry exists and Reconnect is the retry.
  it("refreshes the list after a failed authorization", async () => {
    getMCP
      .mockResolvedValueOnce([])
      .mockResolvedValue([{ name: "elevenlabs", type: "http", enabled: true }]);
    getMCPAuthStatus.mockResolvedValue({
      job_id: "job-1",
      server: "elevenlabs",
      status: "error",
      error: "mcp auth timed out after 2 minutes",
    });
    render(<OpencodeMcpForm />);
    await waitFor(() => expect(screen.getByText("Connect ElevenLabs")).toBeTruthy());

    fireEvent.click(screen.getByText("Connect ElevenLabs"));

    await waitFor(() => expect(screen.getByText("mcp auth timed out after 2 minutes")).toBeTruthy());
    await waitFor(() => expect(screen.getByText("Reconnect ElevenLabs")).toBeTruthy());
    expect(screen.getByText("configured")).toBeTruthy();
    expect(getMCP).toHaveBeenCalledTimes(2);
  });
});
