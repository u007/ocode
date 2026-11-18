import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import UmlPanel from "./UmlPanel";
import { OPEN_FILE_EVENT } from "../../lib/fileLinks";
import type { UmlIndex, UmlResponse } from "../../api/types";

const index: UmlIndex = {
  version: 1,
  root: "/proj",
  generated_at: "2026-11-18T00:00:00Z",
  monorepo: "none",
  packages: [
    { id: "internal/agent", label: "internal/agent", lang: "Go", file_count: 1, symbol_count: 2 },
  ],
  symbols: [
    {
      id: "internal/agent/agent.go:10:Agent", name: "Agent", kind: "struct", lang: "Go",
      pkg: "internal/agent", file: "internal/agent/agent.go", line: 10, end_line: 40,
      doc: "Agent is the core coding agent.",
      sig: "struct { provider LLMClient }",
      members: [{ kind: "field", name: "provider", sig: "LLMClient" }],
    },
    {
      id: "internal/agent/client.go:20:LLMClient", name: "LLMClient", kind: "interface", lang: "Go",
      pkg: "internal/agent", file: "internal/agent/client.go", line: 20, end_line: 30,
      doc: "LLMClient abstracts chat completions.",
      members: [{ kind: "method", name: "Chat", sig: "func(ctx, msgs) (*Response, error)" }],
    },
  ],
  edges: [],
};

function stubFetchSequence(...bodies: (() => UmlResponse | object)[]) {
  let call = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      const next = bodies[Math.min(call++, bodies.length - 1)];
      const body = next();
      const text = JSON.stringify(body);
      const status = (body as { state?: string }).state === "indexing" ? 202 : 200;
      return { ok: status < 300, status, statusText: "", headers: new Headers(), text: async () => text, json: async () => JSON.parse(text) } as Response;
    }),
  );
}

describe("UmlPanel", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.useRealTimers();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("polls through 202 and renders symbols when ready", async () => {
    stubFetchSequence(() => ({ state: "indexing" }), () => ({ state: "ready", index }));
    render(<UmlPanel projectRoot="/proj" sessionId="s1" />);
    expect(screen.getByText(/Indexing Go sources/i)).toBeTruthy();
    await waitFor(() => expect(screen.getByText("Agent")).toBeTruthy(), { timeout: 3000 });
    expect(screen.getByText("LLMClient")).toBeTruthy();
  });

  it("lists results and jumps to source via ocode:open-file", async () => {
    stubFetchSequence(() => ({ state: "ready", index }));
    const seen: { path?: string; line?: number }[] = [];
    window.addEventListener(OPEN_FILE_EVENT, (e: Event) => {
      const d = (e as CustomEvent).detail as { path: string; line?: number };
      seen.push({ path: d.path, line: d.line });
    });
    render(<UmlPanel projectRoot="/proj" sessionId="s1" />);
    await waitFor(() => expect(screen.getByText("Agent")).toBeTruthy());
    fireEvent.click(screen.getByText("Agent"));
    await waitFor(() => expect(screen.getByText(/Open in editor at line 10/)).toBeTruthy());
    fireEvent.click(screen.getByText(/Open in editor at line 10/));
    expect(seen).toEqual([{ path: "/proj/internal/agent/agent.go", line: 10 }]);
  });

  it("filters the list by query", async () => {
    stubFetchSequence(() => ({ state: "ready", index }));
    render(<UmlPanel projectRoot="/proj" sessionId="s1" />);
    await waitFor(() => expect(screen.getByText("Agent")).toBeTruthy());
    // "LLMClient" also matches Agent's signature field (provider LLMClient),
    // so filter on a doc word only LLMClient carries.
    fireEvent.change(screen.getByLabelText("Search symbols"), { target: { value: "chat completions" } });
    expect(screen.queryByText("Agent")).toBeNull();
    expect(screen.getByText("LLMClient")).toBeTruthy();
  });
});
