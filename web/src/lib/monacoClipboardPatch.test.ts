import { afterEach, describe, expect, it } from "vitest";
import { BrowserClipboardService } from "monaco-editor/esm/vs/platform/clipboard/browser/clipboardService.js";
import { disableMonacoWebKitClipboardWorkaround } from "./monacoClipboardPatch";

const prototype = BrowserClipboardService.prototype as {
  installWebKitWriteTextWorkaround?: () => void;
};
const original = prototype.installWebKitWriteTextWorkaround;

describe("disableMonacoWebKitClipboardWorkaround", () => {
  afterEach(() => {
    prototype.installWebKitWriteTextWorkaround = original;
  });

  it("leaves Monaco with a real workaround to replace", () => {
    expect(typeof original).toBe("function");
  });

  it("replaces the WebKit clipboard workaround with a no-op", () => {
    disableMonacoWebKitClipboardWorkaround();

    expect(prototype.installWebKitWriteTextWorkaround).not.toBe(original);
    expect(prototype.installWebKitWriteTextWorkaround?.()).toBeUndefined();
  });
});
