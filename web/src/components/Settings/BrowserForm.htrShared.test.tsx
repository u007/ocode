import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import BrowserForm from "./BrowserForm";
import { api } from "../../api/client";

// The component calls api.getBrowserConfig(...)/api.getHtrStatus(...), so the
// EXPORTED api object is what has to be mocked — patching a named import the
// component never destructures would leave the real fetchJSON running.
vi.mock("../../api/client", () => ({
  api: {
    getBrowserConfig: vi.fn(),
    setBrowserConfig: vi.fn(),
    getHtrStatus: vi.fn(),
    startHtr: vi.fn(),
    stopHtr: vi.fn(),
    listHtrTabs: vi.fn(),
  },
}));

const mockBrowser = vi.mocked(api.getBrowserConfig);
const mockStatus = vi.mocked(api.getHtrStatus);
const mockSave = vi.mocked(api.setBrowserConfig);
const mockStop = vi.mocked(api.stopHtr);

// htr_port 3846 is the legacy configured value; shared mode answers on 3845 and
// the two deliberately differ so echoing the configured number back is visible.
const sharedBrowserConfig = {
  chrome_path: "",
  idle_timeout_minutes: 10,
  screencast_quality: 95,
  htr_enabled: true,
  htr_shared: true,
  htr_token_set: false,
  htr_port: 3846,
  effective_port: 3845,
  effective_socket: "/Users/me/.htrcli/daemon.sock",
  token_source: "htrcli-config",
  config_path: "/Users/me/.htrcli/config.json",
  htr_socket_path: "",
  htr_native_host_name: "com.ocode.htrcontrol",
  htr_extension_path: "",
  htrcli_path: "",
};

const sharedStatus = {
  enabled: true,
  running: true,
  managed: false,
  addr: "127.0.0.1:3845",
  port: 3845,
  socket: "/Users/me/.htrcli/daemon.sock",
  binary: "",
  mode: "shared",
  adopt_only: false,
  notice: "",
  token_source: "htrcli-config",
  config_path: "/Users/me/.htrcli/config.json",
  daemon_pid: 4242,
  started_by_ocode: true,
};

beforeEach(() => {
  vi.clearAllMocks();
  mockBrowser.mockResolvedValue(sharedBrowserConfig as never);
  mockStatus.mockResolvedValue(sharedStatus as never);
  mockSave.mockResolvedValue(sharedBrowserConfig as never);
  mockStop.mockResolvedValue({ ...sharedStatus, running: false, stopped: true } as never);
  vi.mocked(api.startHtr).mockResolvedValue(sharedStatus as never);
  vi.mocked(api.listHtrTabs).mockResolvedValue({ tabs: [] } as never);
});

describe("BrowserForm shared HTR mode", () => {
  it("shows the effective port with its provenance and greys the legacy fields", async () => {
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-effective")).toBeTruthy());

    const eff = screen.getByTestId("htr-effective").textContent ?? "";
    // 3845, not the configured 3846: in shared mode the port comes from
    // htrcli's config, and echoing the legacy value is what makes the panel
    // look broken.
    expect(eff).toContain("3845");
    expect(eff).not.toContain("3846");
    expect(eff).toContain("/Users/me/.htrcli/daemon.sock");
    expect(screen.getByTestId("htr-token-source").textContent).toBe("htrcli-config");
    // Asserted on its own node, not as a substring of the whole row: the config
    // path also contains "htrcli", so a row-wide check would still pass if the
    // token source were dropped entirely.
    expect(eff).toContain("/Users/me/.htrcli/config.json");

    expect(screen.getByTestId("htr-port").hasAttribute("disabled")).toBe(true);
    expect(screen.getByTestId("htr-socket").hasAttribute("disabled")).toBe(true);
  });

  it("leaves the legacy fields editable when the daemon is private-mode", async () => {
    mockBrowser.mockResolvedValue({
      ...sharedBrowserConfig,
      htr_shared: false,
      htr_token_set: true,
      effective_port: 3846,
      effective_socket: "",
      token_source: "generated",
    } as never);
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-port")).toBeTruthy());

    // No shared row at all: with a private daemon the configured port IS the
    // effective one, so a provenance box would only add noise.
    expect(screen.queryByTestId("htr-effective")).toBeNull();
    expect(screen.getByTestId("htr-port").hasAttribute("disabled")).toBe(false);
    expect(screen.getByTestId("htr-socket").hasAttribute("disabled")).toBe(false);
  });

  it("saves the legacy port the user edited in private mode", async () => {
    mockBrowser.mockResolvedValue({ ...sharedBrowserConfig, htr_shared: false } as never);
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-port")).toBeTruthy());

    fireEvent.change(screen.getByTestId("htr-port"), { target: { value: "3999" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    // Guards against a dead input: the field renders and is enabled, so a test
    // that only checked `disabled` would pass while nothing ever persisted.
    await waitFor(() => expect(mockSave).toHaveBeenCalledTimes(1));
    expect(vi.mocked(api.setBrowserConfig).mock.calls[0][0]).toMatchObject({ htr_port: 3999 });
  });

  it("does not send the legacy port in shared mode, where it is meaningless", async () => {
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-effective")).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mockSave).toHaveBeenCalledTimes(1));
    const sent = vi.mocked(api.setBrowserConfig).mock.calls[0][0] as Record<string, unknown>;
    expect("htr_port" in sent).toBe(false);
    expect("htr_socket_path" in sent).toBe(false);
  });

  it("disables the stop button when ocode did not start the daemon", async () => {
    mockStatus.mockResolvedValue({ ...sharedStatus, started_by_ocode: false } as never);
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-stop")).toBeTruthy());

    // A refusal is silent — the API answers 200 with stopped:false — so an
    // enabled button here would look like it worked and change nothing.
    expect(screen.getByTestId("htr-stop").hasAttribute("disabled")).toBe(true);

    // A disabled button fires no mouse events, so its own tooltip would never
    // show; without a visible line the control is just dead.
    await waitFor(() => expect(screen.getByTestId("htr-stop-unavailable")).toBeTruthy());
    expect(screen.getByTestId("htr-stop-unavailable").textContent).toContain("htrcli serve");
  });

  it("shows no stop-refusal line when ocode owns the daemon", async () => {
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-stop")).toBeTruthy());

    // The counterpart, so a hint that renders unconditionally cannot pass.
    expect(screen.queryByTestId("htr-stop-unavailable")).toBeNull();
  });

  it("shows no stop-refusal line when nothing is running", async () => {
    // started_by_ocode is dropped, not merely left true: the server only reads
    // provenance when its own probe says the daemon is running, so a stopped
    // status always reports the field false. A fixture that kept it true was
    // unreachable, and it made this test pass against a hint that is not gated
    // on `running` (mutation-verified).
    const { started_by_ocode: _omitted, ...stopped } = sharedStatus;
    mockStatus.mockResolvedValue({ ...stopped, running: false } as never);
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-stop")).toBeTruthy());

    // "ocode did not start it" is meaningless for a daemon that is not running;
    // Stop is already disabled by `running` and needs no provenance excuse.
    expect(screen.queryByTestId("htr-stop-unavailable")).toBeNull();
  });

  it("enables the stop button when ocode did start the daemon", async () => {
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-stop")).toBeTruthy());

    // The counterpart, so a button that is simply always disabled passes
    // neither direction by accident.
    expect(screen.getByTestId("htr-stop").hasAttribute("disabled")).toBe(false);

    fireEvent.click(screen.getByTestId("htr-stop"));
    await waitFor(() => expect(mockStop).toHaveBeenCalledTimes(1));
  });

  it("disables stop when the server omits started_by_ocode entirely", async () => {
    // An older server, or a payload that lost the field, must not be read as
    // permission: absent is not "yes, you may kill it".
    const { started_by_ocode: _omitted, ...withoutFlag } = sharedStatus;
    mockStatus.mockResolvedValue(withoutFlag as never);
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-stop")).toBeTruthy());

    expect(screen.getByTestId("htr-stop").hasAttribute("disabled")).toBe(true);
  });

  it("surfaces the adopt-only notice verbatim", async () => {
    const notice = "No htrcli config at /Users/me/.htrcli/config.json. Start `htrcli serve` yourself.";
    mockStatus.mockResolvedValue({
      ...sharedStatus,
      running: false,
      adopt_only: true,
      notice,
    } as never);
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-notice")).toBeTruthy());

    // Verbatim: a paraphrased notice loses the file path or the command, which
    // are the two things the user has to act on.
    expect(screen.getByTestId("htr-notice").textContent).toBe(notice);
  });

  it("shows no adopt-only notice when the daemon is not adopt-only", async () => {
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-stop")).toBeTruthy());

    expect(screen.queryByTestId("htr-notice")).toBeNull();
  });

  it("explains a refused stop instead of swallowing it", async () => {
    mockStop.mockResolvedValue({
      ...sharedStatus,
      running: true,
      stopped: false,
      reason: "left running: ocode only stops a daemon it started itself",
    } as never);
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-stop")).toBeTruthy());

    fireEvent.click(screen.getByTestId("htr-stop"));

    await waitFor(() => expect(screen.getByTestId("htr-stop-refused")).toBeTruthy());
    expect(screen.getByTestId("htr-stop-refused").textContent).toContain("only stops a daemon it started");
  });
});
