import { afterEach, describe, expect, it, vi } from "vitest";
import { buildPrintDocument, collectAppStyles, printPreview } from "./printPreview";
import { printDocument } from "./printDocument";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
  vi.restoreAllMocks();
  document.head.querySelectorAll("link[rel='stylesheet'], style").forEach((el) => el.remove());
  document.body.textContent = "";
  delete (window as unknown as { _wails?: unknown })._wails;
  window.history.replaceState({}, "", "/");
});

describe("buildPrintDocument", () => {
  it("renders markdown on white without the dark class and appends the bootstrap", async () => {
    const html = await buildPrintDocument("markdown", { origin: "editor", path: "a.md", content: "# Title\n\ntext" }, "a.md");

    expect(html).toContain("<h1>Title</h1>");
    expect(html).toContain("<title>a.md</title>");
    expect(html).toContain('class="prose');
    expect(html).toContain("ocode:print-window-ready");
    expect(html).not.toContain('class="dark"');
  });

  it("inlines linked and head stylesheets for markdown", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, text: async () => "body{}" })));
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = "/x.css";
    document.head.appendChild(link);
    const style = document.createElement("style");
    style.setAttribute("data-vite-dev-id", "src/app.css");
    style.textContent = ".inline{color:red}";
    document.head.appendChild(style);

    const html = await buildPrintDocument("markdown", { origin: "editor", path: "a.md", content: "text" }, "a.md");

    expect(html).toContain("<style>body{}</style>");
    expect(html).toContain("<style>.inline{color:red}</style>");
  });

  it("skips cross-origin stylesheets without fetching or logging them", async () => {
    const fetchSpy = vi.fn(async () => ({ ok: true, text: async () => "body{}" }));
    vi.stubGlobal("fetch", fetchSpy);
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = "https://cdn.example.com/cross-origin-sheet.css";
    document.head.appendChild(link);

    const styles = await collectAppStyles();

    expect(fetchSpy).not.toHaveBeenCalled();
    expect(error).not.toHaveBeenCalled();
    expect(styles).toBe("");
  });

  it("resolves relative url() refs against the stylesheet, leaving data, fragment and absolute refs as written", async () => {
    const sheet = `.a{background:url(img/bg.png)} .b{mask:url("#m")} .c{background:url(data:image/png;base64,AAA)} .d{background:url('/abs.png')}`;
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, text: async () => sheet })));
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = "/assets/resolve-test.css";
    document.head.appendChild(link);

    const styles = await collectAppStyles();

    const origin = window.location.origin;
    expect(styles).toContain(`url("${origin}/assets/img/bg.png")`);
    expect(styles).toContain('url("#m")');
    expect(styles).toContain("url(data:image/png;base64,AAA)");
    expect(styles).toContain(`url("${origin}/abs.png")`);
  });

  it("logs a same-origin stylesheet that fails to load and skips it", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: false, status: 500, text: async () => "" })));
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = "/assets/broken-test.css";
    document.head.appendChild(link);

    const styles = await collectAppStyles();

    expect(error).toHaveBeenCalledWith(
      "print preview: could not read stylesheet, skipping it",
      { href: `${window.location.origin}/assets/broken-test.css` },
      expect.any(Error),
    );
    expect(styles).toBe("");
  });

  it("inlines only the dev-injected app styles, with their url() refs resolved", async () => {
    const dev = document.createElement("style");
    dev.setAttribute("data-vite-dev-id", "src/index.css");
    dev.textContent = ".dev{background:url(./dev.png)}";
    document.head.appendChild(dev);
    const runtime = document.createElement("style");
    runtime.textContent = ".runtime-library-toast{color:red}";
    document.head.appendChild(runtime);

    const styles = await collectAppStyles();

    expect(styles).toContain(`url("${new URL("./dev.png", document.baseURI).href}")`);
    expect(styles).not.toContain("runtime-library-toast");
  });

  it("escapes text content", async () => {
    const html = await buildPrintDocument("text", { origin: "editor", path: "t.txt", content: "<b>x</b>" }, "t.txt");

    expect(html).toContain("&lt;b&gt;x&lt;/b&gt;");
    expect(html).not.toContain("<b>x</b>");
  });

  it("prints html as a sanitized document and appends only the bootstrap", async () => {
    const source = '<p class="x">hi</p>';

    const html = await buildPrintDocument("html", { origin: "editor", path: "p.html", content: source }, "p.html");

    expect(html).toContain('<body><p class="x">hi</p></body>');
    expect(html).toContain("ocode:print-window-ready");
    expect(html.endsWith("</script>")).toBe(true);
  });

  it("removes script, event handlers and javascript: URLs from html", async () => {
    const source = '<body onload="steal()"><p onclick="x()">hi</p><a href="javascript:alert(1)">l</a><script>alert(2)</script></body>';

    const html = await buildPrintDocument("html", { origin: "editor", path: "p.html", content: source }, "p.html");

    expect(html).not.toContain("alert(");
    expect(html).not.toContain("steal()");
    expect(html).not.toContain("onclick");
    expect(html).not.toContain("javascript:");
    expect(html).toContain("<p>hi</p>");
    expect(html).toContain("ocode:print-window-ready");
  });

  it("keeps the author's doctype, head styles, stylesheet links and style attributes", async () => {
    const source =
      '<!DOCTYPE html><html><head><title>T</title><link rel="stylesheet" href="https://cdn.example.com/a.css"><style>p{color:red}</style></head><body><p style="color:blue">hi</p></body></html>';

    const html = await buildPrintDocument("html", { origin: "editor", path: "p.html", content: source }, "p.html");

    expect(html.startsWith("<!doctype html>")).toBe(true);
    expect(html).toContain('<link rel="stylesheet" href="https://cdn.example.com/a.css">');
    expect(html).toContain("<style>p{color:red}</style>");
    expect(html).toContain('<p style="color:blue">hi</p>');
  });

  it("keeps a <style> that leads a fragment", async () => {
    const html = await buildPrintDocument("html", { origin: "editor", path: "p.html", content: "<style>p{color:red}</style><p>x</p>" }, "p.html");

    expect(html).toContain("<style>p{color:red}</style>");
    expect(html).toContain("<p>x</p>");
  });

  it("rejects kinds it cannot print", async () => {
    await expect(buildPrintDocument("pdf", { origin: "editor", path: "a.pdf", content: "" }, "a.pdf")).rejects.toThrow(
      'print: unsupported preview kind "pdf"',
    );
  });
});

describe("printDocument in a browser", () => {
  it("hosts the document in a sandboxed iframe and posts the print message on load", () => {
    printDocument("<p>doc</p>");

    const frame = document.querySelector("iframe");
    expect(frame?.getAttribute("sandbox")).toBe("allow-scripts allow-modals");
    expect(frame?.getAttribute("srcdoc")).toBe("<p>doc</p>");
    const post = vi.spyOn(frame?.contentWindow as Window, "postMessage");

    frame?.dispatchEvent(new Event("load"));

    expect(post).toHaveBeenCalledWith("ocode:print", "*");
  });

  it("removes the frame after the print dialog has had time to open", () => {
    vi.useFakeTimers();
    printDocument("<p>doc</p>");
    const frame = document.querySelector("iframe");

    frame?.dispatchEvent(new Event("load"));
    vi.advanceTimersByTime(1000);
    expect(frame?.isConnected).toBe(true);

    vi.advanceTimersByTime(60_000);
    expect(frame?.isConnected).toBe(false);
  });

  it("waits for a slow load before posting the print message", async () => {
    vi.useFakeTimers();
    const printed = printDocument("<p>doc</p>");
    const frame = document.querySelector("iframe");
    const post = vi.spyOn(frame?.contentWindow as Window, "postMessage");

    vi.advanceTimersByTime(5000);
    expect(post).not.toHaveBeenCalled();
    frame?.dispatchEvent(new Event("load"));

    await printed;
    expect(post).toHaveBeenCalledWith("ocode:print", "*");
    expect(frame?.isConnected).toBe(true);
  });

  it("removes a frame that never loads and rejects", async () => {
    vi.useFakeTimers();
    const printed = printDocument("<p>doc</p>");
    const rejected = expect(printed).rejects.toThrow("print: preview frame did not load within 15000 ms");

    vi.advanceTimersByTime(15_000);

    await rejected;
    expect(document.querySelector("iframe")).toBeNull();
  });
});

describe("printDocument on the desktop shell", () => {
  // wails.ts reads ?clip= once at module load, so each case loads fresh modules
  // after setting the location it should see.
  async function loadWithLocation(path: string) {
    window.history.replaceState({}, "", path);
    vi.resetModules();
    return import("./printDocument");
  }

  it("hands the document to the shell with the clipboard nonce", async () => {
    const { printDocument: printDesktop } = await loadWithLocation("/?clip=abc123");
    const invoke = vi.fn();
    (window as unknown as { _wails: unknown })._wails = { invoke };

    printDesktop("<p>doc</p>");

    expect(invoke).toHaveBeenCalledWith("ocode:print-document:abc123:<p>doc</p>");
    expect(document.querySelector("iframe")).toBeNull();
  });

  it("rejects when the shell bridge refuses the document", async () => {
    const { printDocument: printDesktop } = await loadWithLocation("/session/abc");
    (window as unknown as { _wails: unknown })._wails = { invoke: vi.fn() };

    await expect(printDesktop("<p>doc</p>")).rejects.toThrow("print: desktop bridge unavailable");
  });
});

describe("printPreview", () => {
  it("builds the document and prints it once the frame loads", async () => {
    const printed = printPreview({ kind: "text", title: "a.txt", source: { origin: "editor", path: "a.txt", content: "plain" } });

    await vi.waitFor(() => expect(document.querySelector("iframe")).not.toBeNull());
    const frame = document.querySelector("iframe");
    expect(frame?.getAttribute("srcdoc")).toContain("<pre");
    expect(frame?.getAttribute("srcdoc")).toContain("plain");
    frame?.dispatchEvent(new Event("load"));

    await printed;
  });
});

describe("collectAppStyles", () => {
  it("re-reads a stylesheet on every print instead of reusing an earlier read", async () => {
    const fetchSpy = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, text: async () => ".v1{color:red}" })
      .mockResolvedValueOnce({ ok: true, text: async () => ".v2{color:blue}" });
    vi.stubGlobal("fetch", fetchSpy);
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = "/assets/recache-test.css";
    document.head.appendChild(link);

    expect(await collectAppStyles()).toContain(".v1{color:red}");
    expect(await collectAppStyles()).toContain(".v2{color:blue}");
    expect(fetchSpy).toHaveBeenCalledTimes(2);
  });
});
