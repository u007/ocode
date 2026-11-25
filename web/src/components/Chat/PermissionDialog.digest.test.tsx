import { useState } from "react";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PermissionDialog from "./PermissionDialog";
import { api, ApiError } from "@/api/client";
import type { PermissionDecision } from "@/api/types";
import type { PermissionDecideResult } from "./PermissionDialog";

// Only the explain call is replaced; ApiError stays real so the component's
// status/code branching is exercised against the class fetchJSON throws.
vi.mock("@/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/client")>();
  return { ...actual, api: { ...actual.api, explainToolCall: vi.fn() } };
});

// Link clicks must reach the URL chooser, not navigate. Mock only the open
// request so the confirmation flow is observable without a mounted chooser.
const requestURL = vi.hoisted(() => vi.fn());
vi.mock("@/lib/externalLinks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/externalLinks")>();
  return { ...actual, requestURLOpen: requestURL };
});

const explainToolCall = vi.mocked(api.explainToolCall);

function apiError(status: number, code: string | undefined, message: string) {
  return new ApiError(message, status, code ? { error: message, code } : { error: message });
}

/** Mirrors production: the dialog portals into the session's chat surface. */
function Scoped(props: Omit<Parameters<typeof PermissionDialog>[0], "container">) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  return (
    <div ref={setContainer} data-testid="chat-surface">
      <PermissionDialog {...props} container={container} />
    </div>
  );
}

type DialogProps = Omit<Parameters<typeof PermissionDialog>[0], "container">;

function makeProps(overrides: Partial<Parameters<typeof PermissionDialog>[0]> = {}): DialogProps {
  const onDecide = vi.fn(
    async (_id: string, _d: PermissionDecision): Promise<PermissionDecideResult> => ({ ok: true }),
  );
  return {
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
}

function renderDialog(overrides: Partial<Parameters<typeof PermissionDialog>[0]> = {}) {
  const props = makeProps(overrides);
  const view = render(<Scoped {...props} />);
  return { onDecide: props.onDecide, props, rerender: view.rerender, unmount: view.unmount };
}

async function startExplain() {
  fireEvent.click(await screen.findByRole("button", { name: "Explain this tool call" }));
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
    requestURL.mockClear();
  });

  it("asks the session's server for an explanation of the pending call", async () => {
    explainToolCall.mockResolvedValue({ summary: "Deletes the build directory.", model: "openai/gpt-4o-mini" });
    renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Explain this tool call" }));

    await waitFor(() => expect(explainToolCall).toHaveBeenCalledTimes(1));
    // A live ask sends its id only; the server reads the parameters itself.
    expect(explainToolCall).toHaveBeenCalledWith(
      "ses_1",
      { request_id: "call-1", tool: "bash" },
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

  it("sends lightweight live request, retries once with full props", async () => {
    explainToolCall
      .mockRejectedValueOnce(apiError(404, "ask_unavailable", "no pending ask"))
      .mockResolvedValueOnce({ summary: "Deletes the build directory.", model: "m" });
    renderDialog();

    await startExplain();

    expect(await screen.findByText("Deletes the build directory.")).toBeTruthy();
    expect(explainToolCall).toHaveBeenCalledTimes(2);
    expect(explainToolCall.mock.calls[0]?.[1]).toEqual({ request_id: "call-1", tool: "bash" });
    expect(explainToolCall.mock.calls[1]?.[1]).toEqual({
      request_id: "call-1",
      tool: "bash",
      command: "rm -rf build",
      args: { command: "rm -rf build" },
      rule: "bash.prefix.rm",
      scope: "bash_prefix",
    });
  });

  it("renders markdown headings/lists/code and never raw HTML or images", async () => {
    explainToolCall.mockResolvedValue({
      summary: [
        "## Build cleanup",
        "",
        "- removes `build/`",
        "- keeps sources",
        "",
        "```",
        "rm -rf build",
        "```",
        "",
        '<img src="https://tracker.example/pixel.png" onerror="alert(1)">',
        "",
        "<script>window.__pwned = true</script>",
        "",
        "![chart](https://tracker.example/chart.png)",
      ].join("\n"),
      model: "m",
    });
    renderDialog();

    await startExplain();

    const status = await screen.findByRole("status");
    expect(within(status).getByRole("heading", { level: 2, name: "Build cleanup" })).toBeTruthy();
    expect(within(status).getAllByRole("listitem")).toHaveLength(2);
    expect(status.querySelector("pre")?.textContent).toContain("rm -rf build");
    expect(status.querySelector("img")).toBeNull();
    expect(status.querySelector("script")).toBeNull();
    expect(status.querySelector("iframe")).toBeNull();
  });

  it("routes links through the confirmation flow", async () => {
    explainToolCall.mockResolvedValue({ summary: "See [the docs](https://example.com/docs).", model: "m" });
    renderDialog();

    await startExplain();

    const link = await screen.findByRole("link", { name: "the docs" });
    expect(link.getAttribute("href")).toBe("https://example.com/docs");
    const proceeded = fireEvent.click(link);
    // MarkdownLink prevents the default navigation and hands the URL to the chooser.
    expect(proceeded).toBe(false);
    expect(requestURL).toHaveBeenCalledWith("https://example.com/docs", undefined);
  });

  it("scrolls long output", async () => {
    const paragraphs = Array.from({ length: 400 }, (_, i) => `Step ${i + 1} explains a detail.`);
    explainToolCall.mockResolvedValue({
      summary: [...paragraphs, "FINAL-LINE-MARKER"].join("\n\n"),
      model: "m",
    });
    renderDialog();

    await startExplain();

    const status = await screen.findByRole("status");
    const scroller = status.querySelector(".overflow-y-auto") as HTMLElement | null;
    expect(scroller).not.toBeNull();
    expect(scroller!.className).toContain("max-h-64");
    expect(scroller!.getAttribute("tabindex")).toBe("0");
    // jsdom cannot measure layout. The tail being in the DOM shows nothing was truncated.
    expect(scroller!.textContent).toContain("Step 1 explains");
    expect(scroller!.textContent).toContain("FINAL-LINE-MARKER");
  });

  it("aborts on close and ask change and ignores stale replies", async () => {
    // Ask change: the in-flight run is aborted and its late reply is dropped.
    const first = deferred<{ summary: string; model: string }>();
    explainToolCall.mockReturnValueOnce(first.promise);
    const { props, rerender, unmount } = renderDialog();
    await startExplain();
    await waitFor(() => expect(explainToolCall).toHaveBeenCalledTimes(1));
    const firstSignal = explainToolCall.mock.calls[0]?.[3] as AbortSignal;
    expect(firstSignal.aborted).toBe(false);

    rerender(
      <Scoped
        {...props}
        requestId="call-2"
        tool="read"
        command="cat README.md"
        args={undefined}
        rule={undefined}
        scope={undefined}
      />,
    );
    expect(firstSignal.aborted).toBe(true);
    await act(async () => {
      first.resolve({ summary: "STALE-ASK-ANSWER", model: "m" });
    });
    expect(screen.queryByText("STALE-ASK-ANSWER")).toBeNull();

    // Dialog close: the same abort applies to a run still in flight. Drop the
    // first dialog so the second one's controls are the only ones in the DOM.
    unmount();
    const second = deferred<{ summary: string; model: string }>();
    explainToolCall.mockReturnValueOnce(second.promise);
    const reopened = renderDialog();
    await startExplain();
    await waitFor(() => expect(explainToolCall).toHaveBeenCalledTimes(2));
    const secondSignal = explainToolCall.mock.calls[1]?.[3] as AbortSignal;
    reopened.rerender(<Scoped {...reopened.props} open={false} />);
    expect(secondSignal.aborted).toBe(true);
    await act(async () => {
      second.resolve({ summary: "STALE-CLOSE-ANSWER", model: "m" });
    });
    expect(screen.queryByText("STALE-CLOSE-ANSWER")).toBeNull();
  });

  it("cross-tab 409 clears spinner with no error banner but shows other errors", async () => {
    // Another tab replaced this run: benign, so the idle affordance returns with no banner.
    explainToolCall.mockRejectedValueOnce(
      apiError(409, "explanation_superseded", "tool digest superseded"),
    );
    renderDialog();

    await startExplain();

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Explain this tool call" })).toBeTruthy(),
    );
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("status")).toBeNull();

    // Any other failure still surfaces.
    explainToolCall.mockRejectedValueOnce(apiError(500, undefined, "upstream exploded"));
    await startExplain();
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("upstream exploded");
  });

  it("surfaces 413", async () => {
    explainToolCall.mockRejectedValueOnce(apiError(413, undefined, ""));
    renderDialog();

    await startExplain();

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("too large");
  });
});
