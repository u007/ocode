import { describe, it, expect, afterEach } from "vitest";
import { focusTerminalInput } from "./terminalFocus";

afterEach(() => {
  document.body.innerHTML = "";
});

describe("focusTerminalInput", () => {
  it("focuses the terminal's hidden input textarea", () => {
    const host = document.createElement("div");
    const textarea = document.createElement("textarea");
    host.append(textarea);
    document.body.append(host);

    focusTerminalInput({ textarea });

    expect(document.activeElement).toBe(textarea);
  });

  it("is a no-op when the terminal has no textarea (not opened, or disposed)", () => {
    const host = document.createElement("div");
    const other = document.createElement("input");
    host.append(other);
    document.body.append(host);
    other.focus();

    expect(() => focusTerminalInput({ textarea: undefined })).not.toThrow();
    expect(document.activeElement).toBe(other);
  });

  it("throws when the terminal input is not a textarea", () => {
    const div = document.createElement("div");
    document.body.append(div);

    expect(() => focusTerminalInput({ textarea: div as unknown as HTMLTextAreaElement })).toThrow(
      "focusTerminalInput: terminal input is not an HTMLTextAreaElement",
    );
  });
});
