import { useState } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PermissionDialog from "./PermissionDialog";
import { api } from "@/api/client";
import type { PermissionDecision } from "@/api/types";
import type { PermissionDecideResult } from "./PermissionDialog";

vi.mock("@/api/client", () => ({
  api: { explainToolCall: vi.fn() },
}));

const explainToolCall = vi.mocked(api.explainToolCall);

type DialogProps = Omit<Parameters<typeof PermissionDialog>[0], "container">;

/** Mirrors production: the dialog portals into the session's chat surface. */
function Scoped(props: DialogProps) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  return (
    <div ref={setContainer} data-testid="chat-surface">
      <PermissionDialog {...props} container={container} />
    </div>
  );
}

function renderDialog(overrides: Partial<DialogProps> = {}) {
  const onDecide = vi.fn(
    async (
      _requestId: string,
      _decision: PermissionDecision,
      _prefix?: string,
      _password?: string,
    ): Promise<PermissionDecideResult> => ({ ok: true }),
  );
  const props: DialogProps = {
    open: true,
    tool: "bash",
    command: "sudo whoami",
    args: { command: "sudo whoami" },
    requestId: "call-1",
    onDecide,
    ...overrides,
  };
  const view = render(<Scoped {...props} />);
  return { onDecide, ...view };
}

const ELEVATION = {
  needsPassword: true,
  elevationCommand: "sudo whoami",
} as const;

describe("PermissionDialog elevation password field", () => {
  beforeEach(() => {
    explainToolCall.mockReset();
    explainToolCall.mockResolvedValue({ summary: "Runs whoami as root.", model: "small" });
  });

  it("renders a masked password input only for elevation asks", () => {
    const { unmount } = renderDialog(ELEVATION);
    const input = screen.getByTestId("permission-password") as HTMLInputElement;
    expect(input.type).toBe("password");
    expect(screen.getByTestId("elevation-command").textContent).toContain("sudo whoami");

    unmount();

    // An ordinary ask has no field even if a stale flag would suggest one.
    renderDialog({ needsPassword: false, elevationCommand: "sudo whoami" });
    expect(screen.queryByTestId("permission-password")).toBeNull();
  });

  it("sends the typed value on approval only, then clears the field", async () => {
    const { onDecide } = renderDialog(ELEVATION);
    const input = screen.getByTestId("permission-password") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "hunter2-elev-secret" } });
    expect(input.value).toBe("hunter2-elev-secret");

    fireEvent.click(screen.getByRole("button", { name: "Allow once" }));

    await vi.waitFor(() => expect(onDecide).toHaveBeenCalledTimes(1));
    expect(onDecide).toHaveBeenCalledWith(
      "call-1",
      "allow",
      undefined,
      "hunter2-elev-secret",
    );
    expect(input.value).toBe("");
  });

  it("never sends or renders the value on deny, for ordinary asks, or to the explain call", async () => {
    // Deny on an elevation ask: the typed value must not cross the wire.
    const denied = renderDialog(ELEVATION);
    fireEvent.change(screen.getByTestId("permission-password"), {
      target: { value: "deny-me-secret" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    await vi.waitFor(() => expect(denied.onDecide).toHaveBeenCalledTimes(1));
    expect(denied.onDecide).toHaveBeenCalledWith("call-1", "deny");
    expect((screen.getByTestId("permission-password") as HTMLInputElement).value).toBe("");
    denied.unmount();

    // Ordinary ask: no field, no password argument.
    const ordinary = renderDialog({ command: "rm -rf build" });
    fireEvent.click(screen.getByRole("button", { name: "Allow once" }));
    await vi.waitFor(() => expect(ordinary.onDecide).toHaveBeenCalledTimes(1));
    expect(ordinary.onDecide).toHaveBeenCalledWith("call-1", "allow");
    ordinary.unmount();

    // The explain payload never carries the typed secret, and neither the
    // rendered dialog text nor console output ever contains it.
    const logSpy = vi.spyOn(console, "log").mockImplementation(() => {});
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const explained = renderDialog({ ...ELEVATION, sessionId: "ses_1" });
    fireEvent.change(screen.getByTestId("permission-password"), {
      target: { value: "never-in-digest-secret" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Explain this tool call" }));
    await vi.waitFor(() => expect(explainToolCall).toHaveBeenCalledTimes(1));
    expect(explainToolCall.mock.calls[0][1]).not.toHaveProperty("password");
    expect(JSON.stringify(explainToolCall.mock.calls[0][1])).not.toContain("never-in-digest-secret");
    expect(document.body.textContent).not.toContain("never-in-digest-secret");
    expect(logSpy.mock.calls.flat().join(" ")).not.toContain("never-in-digest-secret");
    expect(errSpy.mock.calls.flat().join(" ")).not.toContain("never-in-digest-secret");
    logSpy.mockRestore();
    errSpy.mockRestore();
    explained.unmount();
  });

  it("clears the password when a new ask arrives on the same dialog", async () => {
    const view = renderDialog(ELEVATION);
    fireEvent.change(screen.getByTestId("permission-password"), {
      target: { value: "old-request-secret" },
    });
    await act(async () => {
      view.rerender(<Scoped {...({
        open: true,
        tool: "bash",
        command: "sudo whoami",
        requestId: "call-2",
        onDecide: view.onDecide,
        ...ELEVATION,
      } as DialogProps)} />);
    });
    expect((screen.getByTestId("permission-password") as HTMLInputElement).value).toBe("");
  });
});
