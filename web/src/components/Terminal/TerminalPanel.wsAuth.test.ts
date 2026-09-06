import { describe, it, expect, vi } from "vitest";

// TerminalPanel.tsx pulls in @xterm/* packages at module scope; jsdom lacks a
// real canvas, so mock the pieces that touch Color/canvas at import time to
// keep test output free of "Not implemented: HTMLCanvasElement..." noise.
// buildTerminalWsConnection itself has no xterm dependency.
vi.mock("@xterm/xterm", () => ({ Terminal: class {} }));
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class {} }));
vi.mock("@xterm/addon-search", () => ({ SearchAddon: class {} }));
vi.mock("@xterm/addon-serialize", () => ({ SerializeAddon: class {} }));
vi.mock("@xterm/addon-webgl", () => ({ WebglAddon: class {} }));
vi.mock("@xterm/addon-web-links", () => ({ WebLinksAddon: class {} }));
vi.mock("@xterm/xterm/css/xterm.css", () => ({}));

import { buildTerminalWsConnection } from "./TerminalPanel";

// buildTerminalWsConnection is a pure function of its inputs (it does not call
// authToken()/isRemoteSession() itself), so it's testable directly without
// mounting the xterm-backed component. See Task 5 (server-side checkAuth +
// HandleTerminalWS) for why remote mode can't use ?token=.
describe("buildTerminalWsConnection", () => {
  it("uses the Sec-WebSocket-Protocol bearer token and omits ?token= in remote mode", () => {
    const { url, protocols } = buildTerminalWsConnection({
      token: "tok123",
      projectPath: "/proj",
      terminalId: "t1",
      isRemote: true,
    });
    expect(url).not.toContain("token=");
    expect(protocols).toEqual(["ocode.bearer.tok123"]);
  });

  it("uses ?token= and no subprotocol in non-remote mode", () => {
    const { url, protocols } = buildTerminalWsConnection({
      token: "tok123",
      projectPath: "/proj",
      terminalId: "t1",
      isRemote: false,
    });
    expect(url).toContain("token=tok123");
    expect(protocols).toBeUndefined();
  });

  it("puts no token anywhere when there is none, in either mode", () => {
    for (const isRemote of [true, false]) {
      const { url, protocols } = buildTerminalWsConnection({
        token: "",
        projectPath: "/proj",
        terminalId: "t1",
        isRemote,
      });
      expect(url).not.toContain("token");
      expect(protocols).toBeUndefined();
    }
  });

  it("always includes project_path and terminal_id in the query string regardless of mode", () => {
    for (const isRemote of [true, false]) {
      const { url } = buildTerminalWsConnection({
        token: "tok123",
        projectPath: "/proj",
        terminalId: "t1",
        isRemote,
      });
      expect(url).toContain("project_path=%2Fproj");
      expect(url).toContain("terminal_id=t1");
    }
  });
});
