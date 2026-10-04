import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type ConnectMethod, type ConnectProvider } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import ConfirmDialog from "../common/ConfirmDialog";
import ConnectFlowPanel from "./ConnectFlowPanel";

/**
 * ConnectorsForm — Settings → Connectors, the web/desktop equivalent of the
 * TUI `/connect` dialog. Manages the BASE credential store (`auth.json`), not
 * profile overlays; per-profile keys stay in ProfilesManager.
 *
 * Two invariants this component owns:
 *  - `host` is threaded into every call. Credentials live in a per-machine
 *    store, so a call that dropped it would write the key to the wrong box
 *    when the user is looking at a remote (SSH/WSL) project.
 *  - Remove is destructive and irreversible, so it goes through
 *    `common/ConfirmDialog`. Native `confirm()` silently returns false in the
 *    Wails/WKWebView desktop webview, which makes a guarded action permanently
 *    unreachable there while looking fine in a browser.
 *
 * Scope: status, API keys, and OAuth. An OAuth method renders
 * `ConnectFlowPanel` in place of the key form — the two are alternatives, never
 * stacked, because a provider offers either way in and showing both at once
 * reads as "fill in both".
 */
/**
 * `host` is OMITTED by SettingsPanel on purpose: settings are a global surface
 * by convention, so this instance manages the credentials of the machine
 * serving it. The prop exists and is threaded through every call because a
 * project-scoped surface will need it, and because credentials on a remote
 * (SSH/WSL) host are a different store entirely.
 */
/**
 * The methods worth choosing between in an expanded row.
 *
 * `remove` is excluded on purpose: it is not a way to CONNECT, and the row
 * already carries a confirm-gated Remove button. Offering it again behind a
 * chooser labelled "sign-in method" would put a destructive action under a label
 * that promises the opposite, one click away from the API-key field.
 */
function connectMethodChoices(provider: ConnectProvider): ConnectMethod[] {
  return provider.methods.filter((m) => m.kind !== "remove");
}

/**
 * The method an expanded row shows before the user picks one.
 *
 * The API-key form wins when the provider offers one, so "Connect" keeps opening
 * the thing it opened before OAuth was reachable here. Only a provider with no
 * key method (or none at all beyond remove) starts on a flow.
 */
function defaultConnectMethod(choices: ConnectMethod[]): ConnectMethod | undefined {
  return choices.find((m) => m.kind === "apikey") ?? choices[0];
}

export default function ConnectorsForm({ host }: { host?: string }) {
  const [providers, setProviders] = useState<ConnectProvider[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [openId, setOpenId] = useState<string | null>(null);
  // Which connect method the expanded row is showing. Null means "the default
  // for this provider" — see connectMethodChoices — so the API-key form stays
  // what "Connect" opens with.
  const [openMethodId, setOpenMethodId] = useState<string | null>(null);
  const [apiKey, setApiKey] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [pendingRemove, setPendingRemove] = useState<ConnectProvider | null>(null);
  const [testingId, setTestingId] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<{ id: string; text: string; ok: boolean } | null>(null);

  const load = useCallback(async () => {
    setLoadError(null);
    try {
      const res = await api.listConnectProviders(host);
      setProviders(res.providers ?? []);
    } catch (err) {
      // Deliberately NOT swallowed: an empty provider list is indistinguishable
      // from "nothing is configured", which would read as a successful state.
      setProviders([]);
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [host]);

  useEffect(() => {
    void load();
  }, [load]);

  // The catalog is ~40 providers, so the filter is load-bearing rather than
  // cosmetic. Match on label AND id — users know providers by either.
  const visible = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) return providers;
    return providers.filter(
      (p) => p.label.toLowerCase().includes(needle) || p.id.toLowerCase().includes(needle),
    );
  }, [providers, filter]);

  const closeForm = () => {
    setOpenId(null);
    setApiKey("");
    setSaveError(null);
    setOpenMethodId(null);
  };

  const save = async (providerId: string) => {
    const key = apiKey.trim();
    if (!key) {
      setSaveError("Enter an API key first.");
      return;
    }
    setSaving(true);
    setSaveError(null);
    try {
      await api.setConnectCredential(providerId, { apiKey: key }, host);
      closeForm();
      // The mask is server-derived, so re-list rather than patching locally:
      // an optimistic mask would be one the server never produced.
      await load();
    } catch (err) {
      // Stay expanded — a collapsed form reads as "saved".
      setSaveError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const runTest = async (provider: ConnectProvider) => {
    setTestingId(provider.id);
    setTestResult(null);
    try {
      const res = await api.testConnectCredential(provider.id, host);
      setTestResult(
        res.ok
          ? { id: provider.id, text: "Connection succeeded.", ok: true }
          : { id: provider.id, text: res.error || "Connection failed.", ok: false },
      );
    } catch (err) {
      setTestResult({ id: provider.id, text: err instanceof Error ? err.message : String(err), ok: false });
    } finally {
      setTestingId(null);
    }
  };

  return (
    <div className="space-y-4">
      <div className="text-sm text-muted-foreground">
        These are the base credentials in <code>auth.json</code>, shared with the <code>/connect</code> dialog.
        Per-profile overrides live in Profiles.
      </div>

      {loadError && (
        <div role="alert" className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">
          Could not load providers: {loadError}
          <div className="mt-2">
            <Button size="sm" variant="outline" onClick={() => void load()}>
              Retry
            </Button>
          </div>
        </div>
      )}

      <Input
        placeholder="Filter providers…"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        aria-label="Filter providers"
      />

      {loading ? (
        <div className="text-sm text-muted-foreground">Loading providers…</div>
      ) : (
        <div className="space-y-2">
          {visible.map((p) => {
            const choices = connectMethodChoices(p);
            const selected = choices.find((m) => m.id === openMethodId) ?? defaultConnectMethod(choices);
            return (
              <div key={p.id} data-testid={`connector-${p.id}`} className="rounded border border-border p-3">
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-sm font-medium text-foreground">{p.label}</div>
                  <div data-testid={`connector-status-${p.id}`} className="text-xs text-muted-foreground">
                    {/* `status` is an opaque server symbol; render it verbatim
                        rather than re-deriving "connected" from it. */}
                    <span aria-hidden="true">{p.status}</span>{" "}
                    {p.statusDetail || (p.hasCredential ? "connected" : "not connected")}
                  </div>
                  {p.masked && (
                    <div className="mt-1 font-mono text-xs text-muted-foreground">{p.masked}</div>
                  )}
                </div>
                <div className="flex shrink-0 gap-2">
                  {p.hasCredential && (
                    <>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={testingId === p.id}
                        onClick={() => void runTest(p)}
                      >
                        {testingId === p.id ? "Testing…" : "Test"}
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => setPendingRemove(p)}>
                        Remove
                      </Button>
                    </>
                  )}
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      setOpenId(openId === p.id ? null : p.id);
                      setApiKey("");
                      setSaveError(null);
                    }}
                  >
                    Connect
                  </Button>
                </div>
              </div>

              {testResult?.id === p.id && (
                <div className={testResult.ok ? "mt-2 text-xs text-muted-foreground" : "mt-2 text-xs text-destructive"}>
                  {testResult.text}
                </div>
              )}

              {openId === p.id && (
                <div className="mt-3 space-y-3">
                  {/* Only when there is a real choice: a single-method provider
                      gets straight to the form, and a chooser with one option is
                      a control that cannot be wrong and teaches nothing. */}
                  {choices.length > 1 && (
                    <div
                      role="group"
                      aria-label={`${p.label} sign-in method`}
                      className="flex flex-wrap gap-2"
                    >
                      {choices.map((m) => (
                        <Button
                          key={m.id}
                          size="sm"
                          variant={selected?.id === m.id ? "default" : "outline"}
                          aria-pressed={selected?.id === m.id}
                          onClick={() => setOpenMethodId(m.id)}
                        >
                          {m.label}
                        </Button>
                      ))}
                    </div>
                  )}

                  {selected?.kind === "apikey" && (
                    <div className="space-y-2">
                      <Input
                        aria-label="API key"
                        placeholder="API key"
                        // Masked, like the TUI /connect dialog (textinput
                        // EchoPassword in internal/tui/connect.go) and like every
                        // other key field in Settings. A plain-text field leaves the
                        // secret on screen, in screenshots and in screen-shares;
                        // autoComplete is off so a password manager does not offer to
                        // fill or persist it either.
                        type="password"
                        autoComplete="off"
                        spellCheck={false}
                        value={apiKey}
                        onChange={(e) => setApiKey(e.target.value)}
                      />
                      {saveError && <div className="text-xs text-destructive">{saveError}</div>}
                      <div className="flex gap-2">
                        <Button size="sm" disabled={saving} onClick={() => void save(p.id)}>
                          {saving ? "Saving…" : "Save"}
                        </Button>
                        <Button size="sm" variant="ghost" onClick={closeForm}>
                          Cancel
                        </Button>
                      </div>
                    </div>
                  )}

                  {/* A flow method replaces the key form rather than sitting
                      under it. `key` remounts the panel when the selection
                      changes, so a finished flow's state cannot bleed into the
                      next method's panel. onDone reloads the list: the credential
                      now exists and only the server knows its new status and
                      mask. */}
                  {selected && selected.kind !== "apikey" && (
                    <ConnectFlowPanel
                      key={`${p.id}:${selected.id}`}
                      provider={p}
                      method={selected}
                      host={host}
                      onDone={() => void load()}
                    />
                  )}
                </div>
              )}
            </div>
            );
          })}
          {visible.length === 0 && !loadError && (
            <div className="text-sm text-muted-foreground">No provider matches “{filter}”.</div>
          )}
        </div>
      )}

      <ConfirmDialog
        open={pendingRemove !== null}
        title={`Remove the stored credential for ${pendingRemove?.label ?? ""}?`}
        description="The credential is deleted from auth.json — this cannot be undone. That provider then falls back to the environment or reports as disconnected."
        confirmLabel="Remove"
        pendingLabel="Removing…"
        onConfirm={async () => {
          if (!pendingRemove) return
          await api.removeConnectCredential(pendingRemove.id, host)
          setPendingRemove(null)
          await load()
        }}
        onCancel={() => setPendingRemove(null)}
      />
    </div>
  );
}