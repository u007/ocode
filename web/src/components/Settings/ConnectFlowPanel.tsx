import { useCallback, useEffect, useRef, useState } from "react";
import { api, type ConnectFlow, type ConnectMethod, type ConnectProvider } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";

/**
 * ConnectFlowPanel — the OAuth half of Settings → Connectors, the last mile of
 * the manual (paste-back) login. The TUI `/connect` dialog runs these flows in
 * process; here the server owns the flow and this panel only starts it, renders
 * whatever the server reports, and polls until it reaches a terminal state.
 *
 * Why the CLIENT picks `mode`:
 *  - "auto" makes the SERVER bind a loopback port and finish when the provider
 *    redirects the browser back to it. That only works when the browser runs on
 *    the same machine as ocode — so it is wrong for a `serve --remote` host,
 *    and for a second device pointed at a local server.
 *  - "manual" binds nothing; the user pastes the redirect back instead.
 * The server cannot make this call itself: `remoteMode` lives on `*Server` and
 * these routes are registered as `s.handler.*`, so the handler never sees it.
 * The default is therefore `host ? "manual" : "auto"`, with both offered
 * because the server cannot infer a *browser* on a different machine.
 *
 * `waiting_input` is the state that makes manual mode usable, so it is treated
 * as load-bearing: it is the ONLY thing that renders the paste box. A flow
 * never renders a field the server did not send (a `userCode` belongs to a
 * device-code flow; showing an empty one tells the user to type a code that
 * does not exist).
 *
 * Polling is the only completion signal — the server holds the loopback
 * listener, so nothing is pushed to this tab. It runs only while the flow is
 * `waiting_browser`/`running` and stops on any terminal state.
 */

const POLL_INTERVAL_MS = 1500;

type Flow = ConnectFlow & { note?: string };

/** States after which the server will not change the flow on its own. */
function isTerminal(state: string): boolean {
  return state === "complete" || state === "failed" || state === "cancelled";
}

export default function ConnectFlowPanel({
  provider,
  method,
  host,
  onDone,
}: {
  provider: ConnectProvider;
  method: ConnectMethod;
  host?: string;
  /** Called once per completed flow so the caller can refresh provider status. */
  onDone: () => void;
}) {
  const [flow, setFlow] = useState<Flow | null>(null);
  const [mode, setMode] = useState<"auto" | "manual">(host ? "manual" : "auto");
  // Whether this method has a completion mode to choose at all. The server owns
  // that answer (oauthFlowTakesMode): only the OpenAI loopback login has two
  // shapes. Deriving it here instead of assuming every OAuth flow is a loopback
  // is what keeps the chooser off an Anthropic paste-code flow, where the
  // selection would be ignored.
  const modes = method.modes ?? [];
  const choosesMode = modes.includes("auto") && modes.includes("manual");
  const [pasted, setPasted] = useState("");
  // Grok's x.com session cookies. Held only until a successful submit: a
  // rejected submit keeps them so the user can correct a typo without retyping.
  const [cookies, setCookies] = useState({ authToken: "", ct0: "" });
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Guards onDone against a second invocation: a poll that returns `complete`
  // while the submit response is still settling must not refresh twice.
  const reportedRef = useRef(false);

  const start = useCallback(async () => {
    setStarting(true);
    setError(null);
    setPasted("");
    setCookies({ authToken: "", ct0: "" });
    reportedRef.current = false;
    try {
      // "Start over" on a live flow: cancel it first. The auto flow holds the
      // loopback callback port until it ends, so a second start would fail to
      // bind, and an orphaned flow could still finish and save a credential.
      if (flow && !isTerminal(flow.state)) {
        await api.cancelConnectFlow(flow.flowId, host);
      }
      // A method with one shape sends no mode at all: an absent field is the
      // server's "pick the default" signal, and sending a value the handler
      // ignores would make a request look like it asked for something it did not.
      setFlow(await api.startConnectFlow(provider.id, method.id, host, choosesMode ? mode : undefined));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setStarting(false);
    }
  }, [provider.id, method.id, host, mode, choosesMode, flow]);

  const cancel = useCallback(async () => {
    if (!flow) return;
    setError(null);
    try {
      setFlow(await api.cancelConnectFlow(flow.flowId, host));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [flow, host]);

  const submit = useCallback(async () => {
    if (!flow) return;
    setError(null);
    // The server's flow kind decides the payload shape: a cookie flow takes
    // authToken/ct0, every other waiting_input flow takes the pasted redirect.
    const input =
      flow.kind === "cookies"
        ? { authToken: cookies.authToken.trim(), ct0: cookies.ct0.trim() }
        : { code: pasted };
    try {
      setFlow(await api.submitConnectFlowInput(flow.flowId, input, host));
      if (flow.kind === "cookies") setCookies({ authToken: "", ct0: "" });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [flow, pasted, cookies, host]);

  // Poll while the server is still able to finish on its own. The dependency is
  // the flow object, so any state change re-arms (or tears down) the interval.
  useEffect(() => {
    if (!flow || flow.state === "waiting_input" || isTerminal(flow.state)) return;
    let stopped = false;
    const timer = setInterval(() => {
      void (async () => {
        try {
          const next = await api.getConnectFlow(flow.flowId, host);
          if (!stopped) setFlow(next);
        } catch {
          // A transient poll failure must not kill the flow: the next tick
          // re-reads the authoritative state and a real failure surfaces as
          // state "failed" with the server's own message.
        }
      })();
    }, POLL_INTERVAL_MS);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, [flow, host]);

  // Report completion exactly once per successful flow.
  useEffect(() => {
    if (flow?.state !== "complete" || reportedRef.current) return;
    reportedRef.current = true;
    onDone();
  }, [flow?.state, onDone]);

  const startButton = (
    <div className="space-y-2">
      {/* Only a flow with two shapes offers a choice. The server says which
          via `modes`; every other method (Anthropic paste-code, a device code, a
          cookie or plugin flow) has exactly one, and rendering a chooser there
          would be a control that silently does nothing — the user picks a mode,
          gets the same flow, and concludes the setting is broken. */}
      {choosesMode && (
        <>
          <fieldset className="space-y-1" aria-label="Sign-in completion">
            <legend className="text-xs text-muted-foreground">How should the sign-in finish?</legend>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="radio"
                name={`connect-mode-${provider.id}-${method.id}`}
                checked={mode === "auto"}
                onChange={() => setMode("auto")}
              />
              Open the page on this machine
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="radio"
                name={`connect-mode-${provider.id}-${method.id}`}
                checked={mode === "manual"}
                onChange={() => setMode("manual")}
              />
              Paste the redirect back
            </label>
          </fieldset>
          <p className="text-xs text-muted-foreground">
            Choose &ldquo;paste the redirect back&rdquo; if the sign-in page opens on a different
            machine than the one running ocode &mdash; an SSH/WSL host, or a second device.
            Otherwise ocode finishes the sign-in by itself.
          </p>
        </>
      )}
      <Button size="sm" onClick={() => void start()} disabled={starting}>
        {starting ? "Starting…" : `Sign in with ${method.label}`}
      </Button>
    </div>
  );

  if (!flow || flow.state === "cancelled") {
    return (
      <div className="space-y-2 rounded-md border p-3">
        {startButton}
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-2 rounded-md border p-3" aria-label={`${provider.label} sign-in`}>
      {flow.instructions && <p className="text-sm">{flow.instructions}</p>}
      {flow.note && <p className="text-xs text-muted-foreground">{flow.note}</p>}

      {flow.state === "waiting_input" && flow.kind === "cookies" && (
        <div className="space-y-2">
          {/* Password-masked and autocomplete-off, like every key field in
              Settings: auth_token is a full x.com session secret. */}
          <Input
            aria-label="auth_token"
            type="password"
            autoComplete="off"
            spellCheck={false}
            placeholder="auth_token"
            value={cookies.authToken}
            onChange={(e) => setCookies((c) => ({ ...c, authToken: e.target.value }))}
          />
          <Input
            aria-label="ct0"
            type="password"
            autoComplete="off"
            spellCheck={false}
            placeholder="ct0"
            value={cookies.ct0}
            onChange={(e) => setCookies((c) => ({ ...c, ct0: e.target.value }))}
          />
          <Button
            size="sm"
            onClick={() => void submit()}
            disabled={!cookies.authToken.trim() || !cookies.ct0.trim()}
          >
            Finish sign-in
          </Button>
        </div>
      )}

      {flow.state === "waiting_input" && flow.kind !== "cookies" && (
        <div className="space-y-2">
          <label className="block text-sm" htmlFor={`connect-paste-${flow.flowId}`}>
            Paste the URL your browser was redirected to
          </label>
          <Input
            id={`connect-paste-${flow.flowId}`}
            value={pasted}
            placeholder="http://localhost:1455/auth/callback?code=…&state=…"
            onChange={(e) => setPasted(e.target.value)}
          />
          <p className="text-xs text-muted-foreground">
            Paste the whole address bar, not just the code &mdash; the code is checked against the
            sign-in attempt that started here.
          </p>
          <Button size="sm" onClick={() => void submit()} disabled={!pasted.trim()}>
            Finish sign-in
          </Button>
        </div>
      )}

      {flow.state === "waiting_browser" && flow.url && (
        <a
          href={flow.url}
          target="_blank"
          rel="noreferrer"
          className="text-sm underline"
          // The provider page is a normal outbound navigation, not an in-app
          // URL click: the confirm-before-open dialog is for transcript links.
        >
          Open the sign-in page
        </a>
      )}

      {flow.kind === "device-code" && flow.userCode && (
        <p className="text-sm">
          Enter code <code className="font-mono font-semibold">{flow.userCode}</code>
          {flow.verificationUri && (
            <>
              {" at "}
              <a href={flow.verificationUri} target="_blank" rel="noreferrer" className="underline">
                {flow.verificationUri}
              </a>
            </>
          )}
        </p>
      )}

      {flow.state === "running" && <p className="text-sm text-muted-foreground">Finishing sign-in…</p>}

      {flow.state === "complete" && (
        <p className="text-sm">
          {provider.label} connected{flow.account ? ` as ${flow.account}` : ""}.
        </p>
      )}

      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {flow.error && (
        <p role="alert" className="text-sm text-destructive">
          {flow.error}
        </p>
      )}

      {!isTerminal(flow.state) && (
        <div className="flex gap-2">
          {!isTerminal(flow.state) && (
            <Button size="sm" variant="ghost" onClick={() => void cancel()}>
              Cancel
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={() => void start()}>
            Start over
          </Button>
        </div>
      )}
      {flow.state === "cancelled" && null}
    </div>
  );
}
