import { useState } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PermissionDialog from "./PermissionDialog";
import { api } from "@/api/client";
import type { PermissionDecision } from "@/api/types";
import type { PermissionDecideResult } from "./PermissionDialog";

vi.mock("@/api/client", () => ({
  api: { explainToolCall: vi.fn() },
}));

const explainToolCall = vi.mocked(api.explainToolCall);

/** Mirrors production: the dialog portals into the session's chat surface. */
function Scoped(props: Omit<Parameters<typeof PermissionDialog>[0], "container">) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  return (
    <div ref={setContainer} data-testid="chat-surface">
      <PermissionDialog {...props} container={container} />
    </div>
  );
}

function renderDialog(overrides: Partial<Parameters<typeof PermissionDialog>[0]> = {}) {
  const onDecide = vi.fn(
    async (_id: string, _d: PermissionDecision): Promise<PermissionDecideResult> => ({ ok: true }),
  );
  const props: Omit<Parameters<typeof PermissionDialog>[0], "container"> = {
    open: true,
    tool: "bash",
    command: "rm -rf build",
    args: { command: "rm -rf build" },
    rule: "bash.prefix.rm",
    scope: "bash_prefix",
    sessionId: "ses_1",
    host: "james@host",
    requestId: "call-1",
    onDecide,
    ...overrides,
  };
  const view = render(<Scoped {...props} />);
  return { onDecide, rerender: view.rerender };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (err: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("PermissionDialog tool-call digest", () => {
  beforeEach(() => {
    explainToolCall.mockReset();
  });

  it("asks the session's server for an explanation of the pending call", async () => {
    explainToolCall.mockResolvedValue({ summary: "Deletes the build directory.", model: "openai/gpt-4o-mini" });
    renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Explain this tool call" }));

    await waitFor(() => expect(explainToolCall).toHaveBeenCalledTimes(1));
    expect(explainToolCall).toHaveBeenCalledWith(
      "ses_1",
      {
        request_id: "call-1",
        tool: "bash",
        command: "rm -rf build",
        args: { command: "rm -rf build" },
        rule: "bash.prefix.rm",
        scope: "bash_prefix",
      },
      "james@host",
      expect.any(AbortSignal),
    );
    expect(await screen.findByText("Deletes the build directory.")).toBeTruthy();
  });

  it("labels the result as an AI summary, not a safety verdict", async () => {
    explainToolCall.mockResolvedValue({ summary: "Reads package.json.", model: "openai/gpt-4o-mini" });
    renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Explain this tool call" }));

    const status = await screen.findByRole("status");
    expect(status.textContent).toContain("AI summary");
    expect(status.textContent).toContain("openai/gpt-4o-mini");
    expect(status.textContent).toContain("not a safety verdict");
  });

  it("shows a loading state while the model answers and offers a re-run", async () => {
    const pending = deferred<{ summary: string; model: string }>();
    explainToolCall.mockReturnValue(pending.promise);
    renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Explain this tool call" }));
    expect(await screen.findByText("Asking the small model…")).toBeTruthy();

    await act(async () => {
      pending.resolve({ summary: "Deletes the build directory.", model: "m" });
    });
    // The button becomes a re-run affordance, so an unsatisfying or stale
    // answer can be refreshed without closing the dialog.
    expect(await screen.findByRole("button", { name: "Explain again" })).toBeTruthy();
  });

  it("surfaces a failed explanation inline instead of silently doing nothing", async () => {
    explainToolCall.mockRejectedValue(new Error("explain failed: upstream exploded"));
    renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Explain this tool call" }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Could not explain this call");
    expect(alert.textContent).toContain("upstream exploded");
    // The ask is still answerable — the explanation is an aid, never a gate.
    expect(screen.getByRole("button", { name: /^Deny$/ })).toBeTruthy();
  });

  it("does not offer the action without a session id", async () => {
    renderDialog({ sessionId: undefined });
    await screen.findByRole("dialog");
    expect(screen.queryByRole("button", { name: "Explain this tool call" })).toBeNull();
  });

  it("does not offer the action for a content-guardrail ask", async () => {
    // A content ask is about a result that already ran; there is no pending
    // tool call to explain.
    renderDialog({ scope: "content", untrustedContent: "some result" });
    await screen.findByRole("dialog");
    expect(screen.queryByRole("button", { name: "Explain this tool call" })).toBeNull();
  });

  it("clears a previous explanation when a new ask arrives", async () => {
    explainToolCall.mockResolvedValue({ summary: "Deletes the build directory.", model: "m" });
    const { rerender } = renderDialog();
    fireEvent.click(await screen.findByRole("button", { name: "Explain this tool call" }));
    expect(await screen.findByText("Deletes the build directory.")).toBeTruthy();

    const onDecide = vi.fn(async (): Promise<PermissionDecideResult> => ({ ok: true }));
    rerender(
      <Scoped
        open
        tool="read"
        command="cat README.md"
        sessionId="ses_1"
        requestId="call-2"
        onDecide={onDecide}
      />,
    );

    await waitFor(() => expect(screen.queryByText("Deletes the build directory.")).toBeNull());
  });
});
