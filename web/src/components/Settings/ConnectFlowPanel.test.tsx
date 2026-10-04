import { describe, it, expect, beforeEach, vi } from "vitest";
import { fireEvent, render, screen, waitFor, act } from "@testing-library/react";
import { api } from "../../api/client";
import ConnectFlowPanel from "./ConnectFlowPanel";

/**
 * The flow panel is the last mile of manual OAuth: the server and api client
 * both support it, but without this component the user can never start a flow
 * from the web/desktop UI.
 *
 * Contract these tests pin:
 *  - the CLIENT chooses `mode`, because only the client knows whether the
 *    browser shares the server's host;
 *  - `waiting_input` must produce a paste box (that state is the whole reason
 *    manual mode exists). Query it by ROLE, not by label text: the "Paste the
 *    redirect back" mode radio also matches /redirect/i and comes first in DOM
 *    order, so a bare label query returns the radio and the test passes without
 *    ever touching the box;
 *  - `waiting_browser` must poll, because nothing pushes the completion;
 *  - a terminal flow stops polling and reports back so the status row refreshes.
 */

vi.mock("../../api/client", async (orig) => {
  const actual = await orig<typeof import("../../api/client")>();
  return { ...actual, api: { ...actual.api } };
});

// OpenAI's login is the one flow the server reports two completion modes for
// (see oauthFlowTakesMode), so the fixture carries `modes` exactly as the wire
// does. SINGLE_SHAPE_METHOD below is the other case: a flow with no choice.
const PROVIDER = {
  id: "openai",
  label: "OpenAI",
  status: "✗",
  statusDetail: "no credential",
  methods: [
    {
      id: "oauth",
      label: "ChatGPT subscription",
      kind: "oauth" as const,
      modes: ["auto", "manual"] as ("auto" | "manual")[],
    },
  ],
  hasCredential: false,
};

const METHOD = PROVIDER.methods[0];

/** A flow with exactly one shape: no `modes` on the wire, so no chooser. */
const SINGLE_SHAPE_METHOD = { id: "oauth_console", label: "Anthropic Console", kind: "oauth" as const };
const SINGLE_SHAPE_PROVIDER = {
  ...PROVIDER,
  id: "anthropic",
  label: "Anthropic",
  methods: [SINGLE_SHAPE_METHOD],
};

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

beforeEach(() => {
  vi.restoreAllMocks();
  api.startConnectFlow = vi.fn(async () => ({
    flowId: "f1",
    kind: "local-callback",
    state: "waiting_input",
  })) as never;
  api.getConnectFlow = vi.fn(async () => ({
    flowId: "f1",
    kind: "local-callback",
    state: "waiting_input",
  })) as never;
  api.submitConnectFlowInput = vi.fn(async () => ({
    flowId: "f1",
    kind: "local-callback",
    state: "complete",
  })) as never;
  api.cancelConnectFlow = vi.fn(async () => ({
    flowId: "f1",
    kind: "local-callback",
    state: "cancelled",
  })) as never;
});

describe("ConnectFlowPanel", () => {
  it("starts a flow and opens a paste box when the server asks for input", async () => {
    const onDone = vi.fn();
    render(<ConnectFlowPanel provider={PROVIDER} method={METHOD} onDone={onDone} />);

    fireEvent.click(screen.getByRole("button", { name: /sign in with chatgpt subscription/i }));

    await waitFor(() =>
      expect(screen.getByRole("textbox", { name: /redirect/i })).toBeInTheDocument(),
    );
    // The panel must NOT declare success just because the flow exists.
    expect(onDone).not.toHaveBeenCalled();
    expect(screen.queryByText(/connected|success/i)).toBeNull();
  });

  it("sends the pasted redirect and reports completion", async () => {
    const onDone = vi.fn();
    render(<ConnectFlowPanel provider={PROVIDER} method={METHOD} onDone={onDone} />);

    fireEvent.click(screen.getByRole("button", { name: /sign in with chatgpt subscription/i }));
    const box = await screen.findByRole("textbox", { name: /redirect/i });
    fireEvent.change(box, {
      target: { value: "http://localhost:1455/auth/callback?code=abc&state=st-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: /finish sign-in/i }));

    await waitFor(() =>
      expect(api.submitConnectFlowInput).toHaveBeenCalledWith(
        "f1",
        { code: "http://localhost:1455/auth/callback?code=abc&state=st-1" },
        undefined,
      ),
    );
    await waitFor(() => expect(onDone).toHaveBeenCalled());
  });

  it("requests manual mode when the user picks paste-back", async () => {
    render(<ConnectFlowPanel provider={PROVIDER} method={METHOD} onDone={vi.fn()} />);

    fireEvent.click(screen.getByRole("radio", { name: /paste the redirect back/i }));
    fireEvent.click(screen.getByRole("button", { name: /sign in with chatgpt subscription/i }));

    await waitFor(() =>
      expect(api.startConnectFlow).toHaveBeenCalledWith("openai", "oauth", undefined, "manual"),
    );
  });

  it("defaults to auto mode for a local server and manual for a remote host", async () => {
    const { unmount } = render(
      <ConnectFlowPanel provider={PROVIDER} method={METHOD} onDone={vi.fn()} />,
    );
    expect(screen.getByRole("radio", { name: /open the page on this machine/i })).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: /sign in with chatgpt subscription/i }));
    await waitFor(() =>
      expect(api.startConnectFlow).toHaveBeenCalledWith("openai", "oauth", undefined, "auto"),
    );
    unmount();

    render(<ConnectFlowPanel provider={PROVIDER} method={METHOD} host="james@box" onDone={vi.fn()} />);
    expect(screen.getByRole("radio", { name: /paste the redirect back/i })).toBeChecked();
  });

  it("offers no mode chooser for a flow the server gave one shape", async () => {
    // An Anthropic paste-code flow ALWAYS waits for a paste, so a chooser would
    // be a control the handler ignores: the user picks a mode, gets the same
    // flow, and concludes sign-in is broken. The server says which flows have a
    // choice by sending `modes`; absent means it does not.
    render(
      <ConnectFlowPanel
        provider={SINGLE_SHAPE_PROVIDER}
        method={SINGLE_SHAPE_METHOD}
        onDone={vi.fn()}
      />,
    );

    expect(screen.queryByRole("radio", { name: /open the page on this machine/i })).toBeNull();
    expect(screen.queryByRole("radio", { name: /paste the redirect back/i })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /sign in with anthropic console/i }));
    await waitFor(() => expect(api.startConnectFlow).toHaveBeenCalled());
    // No mode is sent at all, rather than a default the server would ignore.
    expect(api.startConnectFlow).toHaveBeenCalledWith("anthropic", "oauth_console", undefined, undefined);
  });

  it("polls a waiting_browser flow and stops once it completes", async () => {
    // No waitFor anywhere in this test: RTL's waitFor decides whether to drive
    // the clock by looking for a global `jest`, which vitest does not provide,
    // so it assumes real timers and its own polling loop then never runs. Flush
    // with act() and assert directly instead.
    vi.useFakeTimers();
    try {
      api.startConnectFlow = vi.fn(async () => ({
        flowId: "f1",
        kind: "local-callback",
        state: "waiting_browser",
        url: "https://auth.openai.com/oauth/authorize?x=1",
        instructions: "Sign in in the new tab.",
      })) as never;

      // The shared beforeEach poll mock answers "waiting_input", which would
      // make the panel (correctly) stop polling after the first tick. A real
      // loopback flow stays waiting_browser until the redirect lands, so model
      // that here and swap the mock only at the transition under test.
      const pending = vi.fn(async () => ({
        flowId: "f1",
        kind: "local-callback",
        state: "waiting_browser",
      }));
      api.getConnectFlow = pending as never;

      const onDone = vi.fn();
      render(<ConnectFlowPanel provider={PROVIDER} method={METHOD} onDone={onDone} />);

      await act(async () => {
        fireEvent.click(screen.getByRole("button", { name: /sign in with chatgpt subscription/i }));
      });

      const link = screen.getByRole("link", { name: /open the sign-in page/i });
      expect(link).toHaveAttribute("href", "https://auth.openai.com/oauth/authorize?x=1");

      // The gate: NO completion can arrive without polling, because the server
      // holds the loopback listener and nothing pushes to this tab.
      expect(api.getConnectFlow).not.toHaveBeenCalled();
      await act(async () => {
        vi.advanceTimersByTime(2000);
      });
      expect(api.getConnectFlow).toHaveBeenCalledWith("f1", undefined);

      const completing = vi.fn(async () => ({
        flowId: "f1",
        kind: "local-callback",
        state: "complete",
        account: "me@example.com",
      }));
      api.getConnectFlow = completing as never;
      await act(async () => {
        vi.advanceTimersByTime(2000);
      });
      expect(onDone).toHaveBeenCalledTimes(1);
      expect(screen.getByText(/connected as me@example\.com/i)).toBeInTheDocument();

      // The interval must be GONE, not merely harmless: a live interval on a
      // terminal flow re-arms on every state change and would poll a flow that
      // can never change again.
      const callsAtCompletion = completing.mock.calls.length;
      await act(async () => {
        vi.advanceTimersByTime(30000);
      });
      expect(completing).toHaveBeenCalledTimes(callsAtCompletion);
    } finally {
      vi.useRealTimers();
    }
  });

  it("shows the server's error and stops polling on failure", async () => {
    vi.useFakeTimers();
    try {
      api.startConnectFlow = vi.fn(async () => ({
        flowId: "f1",
        kind: "local-callback",
        state: "waiting_browser",
        url: "https://auth.openai.com/x",
      })) as never;
      const failing = vi.fn(async () => ({
        flowId: "f1",
        kind: "local-callback",
        state: "failed",
        error: "code already used",
      }));
      api.getConnectFlow = failing as never;

      render(<ConnectFlowPanel provider={PROVIDER} method={METHOD} onDone={vi.fn()} />);
      await act(async () => {
        fireEvent.click(screen.getByRole("button", { name: /sign in with chatgpt subscription/i }));
      });
      await act(async () => {
        vi.advanceTimersByTime(2000);
      });

      expect(screen.getByRole("alert")).toHaveTextContent(/already used/i);
      const callsAtFailure = failing.mock.calls.length;
      await act(async () => {
        vi.advanceTimersByTime(30000);
      });
      expect(failing).toHaveBeenCalledTimes(callsAtFailure);
    } finally {
      vi.useRealTimers();
    }
  });

  it("lets the user abandon a flow and starts over", async () => {
    render(<ConnectFlowPanel provider={PROVIDER} method={METHOD} onDone={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: /sign in with chatgpt subscription/i }));
    await screen.findByRole("textbox", { name: /redirect/i });
    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));

    await waitFor(() => expect(api.cancelConnectFlow).toHaveBeenCalledWith("f1", undefined));
    // Back to the start affordance, so a failure is retryable without a reload.
    expect(screen.getByRole("button", { name: /sign in with chatgpt subscription/i })).toBeEnabled();
  });

  it("cancels the live flow before Start over starts a new one", async () => {
    // The auto flow holds the loopback callback port until it ends: a second
    // start without a cancel fails to bind, and the orphan can still finish.
    const order: string[] = [];
    api.startConnectFlow = vi.fn(async () => {
      order.push("start");
      return { flowId: "f1", kind: "local-callback", state: "waiting_input" };
    }) as never;
    api.cancelConnectFlow = vi.fn(async () => {
      order.push("cancel");
      return { flowId: "f1", kind: "local-callback", state: "cancelled" };
    }) as never;
    render(<ConnectFlowPanel provider={PROVIDER} method={METHOD} onDone={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: /sign in with chatgpt subscription/i }));
    await screen.findByRole("textbox", { name: /redirect/i });
    fireEvent.click(screen.getByRole("button", { name: /start over/i }));

    await waitFor(() => expect(api.startConnectFlow).toHaveBeenCalledTimes(2));
    expect(api.cancelConnectFlow).toHaveBeenCalledWith("f1", undefined);
    expect(order).toEqual(["start", "cancel", "start"]);
  });

  it("never renders a flow field the server did not send", async () => {
    const gate = deferred<{ flowId: string; kind: string; state: string }>();
    api.startConnectFlow = vi.fn(() => gate.promise) as never;

    render(<ConnectFlowPanel provider={PROVIDER} method={METHOD} onDone={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: /sign in with chatgpt subscription/i }));

    // A userCode is NOT part of a local-callback flow; showing an empty one
    // would tell the user to type a code that does not exist.
    expect(screen.queryByText(/user code/i)).toBeNull();

    await act(async () => {
      gate.resolve({ flowId: "f1", kind: "local-callback", state: "waiting_input" });
    });
    expect(screen.queryByText(/user code/i)).toBeNull();
  });
});
