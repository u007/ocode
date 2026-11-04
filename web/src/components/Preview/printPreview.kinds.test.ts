import { afterEach, describe, expect, it, vi } from "vitest";
import * as XLSX from "xlsx";
import JSZip from "jszip";
import { api } from "../../api/client";
import { IMAGE_MIME_BY_EXT, imageMimeForPath, previewKindForPath } from "../../lib/previewKind";
import { buildPrintDocument } from "./printPreview";

const mermaid = vi.hoisted(() => ({
  initialize: vi.fn(),
  render: vi.fn(),
  mermaidAPI: { getConfig: vi.fn(() => ({ theme: "dark", securityLevel: "loose" })) },
}));
vi.mock("mermaid", () => ({ default: mermaid }));

const docx = vi.hoisted(() => ({ renderAsync: vi.fn() }));
vi.mock("docx-preview", () => ({ renderAsync: docx.renderAsync }));

vi.mock("../../api/client", () => ({ api: { fetchFileRaw: vi.fn(), getFileContent: vi.fn() } }));

afterEach(() => {
  vi.clearAllMocks();
});

const PNG_HEADER = new Uint8Array([0x89, 0x50, 0x4e, 0x47]);

function arrayBufferOf(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
}

describe("buildPrintDocument: image", () => {
  it("embeds the saved file as a data URL with its MIME type, fitted to the page", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(arrayBufferOf(PNG_HEADER));

    const html = await buildPrintDocument("image", { origin: "disk", path: "pics/a.png" }, "a.png");

    expect(html).toContain('src="data:image/png;base64,iVBORw=="');
    expect(html).toContain("max-height: 100vh");
    expect(html).toContain("object-fit: contain");
    expect(api.fetchFileRaw).toHaveBeenCalledWith("pics/a.png", undefined, undefined);
  });

  it("reads remote projects through the same host as the viewer", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(arrayBufferOf(PNG_HEADER));

    await buildPrintDocument("image", { origin: "disk", path: "pics/a.png", projectRoot: "/srv/p", projectHost: "ssh-1" }, "a.png");

    expect(api.fetchFileRaw).toHaveBeenCalledWith("pics/a.png", "/srv/p", "ssh-1");
  });

  it("escapes the title used as alt text", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(arrayBufferOf(PNG_HEADER));

    const html = await buildPrintDocument("image", { origin: "disk", path: "a.png" }, 'a"<b>.png');

    expect(html).toContain('alt="a&quot;&lt;b&gt;.png"');
    expect(html).not.toContain("<b>.png");
  });

  it("prints live SVG text from the split view, encoded so it cannot inject markup", async () => {
    const svg = '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>';

    const html = await buildPrintDocument("image", { origin: "editor", path: "logo.svg", content: svg }, "logo.svg");

    expect(html).toContain("src=\"data:image/svg+xml;charset=utf-8,");
    expect(html).not.toContain("<script>alert");
    expect(api.fetchFileRaw).not.toHaveBeenCalled();
  });

  it("rejects live text for a raster image, which has none", async () => {
    await expect(buildPrintDocument("image", { origin: "editor", path: "a.png", content: "x" }, "a.png")).rejects.toThrow(
      "print: a.png has no editable text",
    );
  });
});

describe("buildPrintDocument: mermaid", () => {
  it("renders the source on the light theme and places the SVG in the document", async () => {
    mermaid.render.mockResolvedValue({ svg: "<svg><text>A &amp; B</text></svg>" });

    const html = await buildPrintDocument("mermaid", { origin: "editor", path: "f.mmd", content: "graph TD; A-->B" }, "f.mmd");

    expect(mermaid.initialize).toHaveBeenCalledWith(expect.objectContaining({ theme: "default", securityLevel: "strict" }));
    expect(mermaid.render).toHaveBeenCalledWith(expect.stringMatching(/^mmd-print-/), "graph TD; A-->B");
    expect(html).toContain("<text>A &amp; B</text>");
    expect(html).toContain("<title>f.mmd</title>");
  });

  it("restores the viewer's theme and security level after rendering", async () => {
    mermaid.render.mockResolvedValue({ svg: "<svg></svg>" });

    await buildPrintDocument("mermaid", { origin: "editor", path: "f.mmd", content: "graph TD; A-->B" }, "f.mmd");

    expect(mermaid.initialize).toHaveBeenLastCalledWith({ startOnLoad: false, securityLevel: "loose", theme: "dark" });
  });

  it("propagates a diagram that fails to render, so the button shows it", async () => {
    mermaid.render.mockRejectedValue(new Error("Parse error on line 1"));

    await expect(
      buildPrintDocument("mermaid", { origin: "editor", path: "f.mmd", content: "graph" }, "f.mmd"),
    ).rejects.toThrow("Parse error on line 1");
    expect(mermaid.initialize).toHaveBeenLastCalledWith({ startOnLoad: false, securityLevel: "loose", theme: "dark" });
  });

  it("keeps flowchart labels in foreignObject, the inline style and marker refs, and removes script and handlers", async () => {
    mermaid.render.mockResolvedValue({
      svg:
        '<svg id="m" xmlns="http://www.w3.org/2000/svg"><style>#m .node rect{fill:#fff}</style><defs><marker id="arrow" viewBox="0 0 10 10"><path d="M 0 0 L 10 5 L 0 10 z"/></marker></defs>' +
        '<g class="node" id="flowchart-A-0"><rect width="40" height="20"/><foreignObject width="40" height="20"><div xmlns="http://www.w3.org/1999/xhtml" class="label"><span class="nodeLabel"><p>Hello</p></span></div></foreignObject></g>' +
        '<path d="M 0 0 L 10 10" marker-end="url(#arrow)" onclick="alert(1)"/><script>alert(2)</script></svg>',
    });

    const html = await buildPrintDocument("mermaid", { origin: "editor", path: "f.mmd", content: "graph TD; A-->B" }, "f.mmd");

    expect(html).toContain('<span class="nodeLabel"><p>Hello</p></span>');
    expect(html).toContain("#m .node rect{fill:#fff}");
    expect(html).toContain('<marker id="arrow"');
    expect(html).toContain('marker-end="url(#arrow)"');
    expect(html).not.toContain("alert(");
    expect(html).not.toContain("onclick");
  });
});

describe("buildPrintDocument: excel", () => {
  it("prints live CSV text as a table with every cell escaped", async () => {
    const csv = 'name,note\n"<img src=x onerror=alert(1)>",ok\n';

    const html = await buildPrintDocument("excel", { origin: "editor", path: "data.csv", content: csv }, "data.csv");

    expect(html).toContain("<table>");
    expect(html).toContain("&lt;img src=x onerror=alert(1)&gt;");
    expect(html).not.toContain("<img src=x");
    expect(html).toContain("<td>ok</td>");
  });

  it("keeps a blank row between two data rows as an empty table row", async () => {
    const html = await buildPrintDocument("excel", { origin: "editor", path: "gaps.csv", content: "a,b\n\nc,d\n" }, "gaps.csv");

    expect(html.match(/<tr>/g)).toHaveLength(3);
    expect(html).toContain("<tr><td>&nbsp;</td><td>&nbsp;</td></tr>");
  });

  it("prints every sheet of a saved workbook under its escaped name, one section per sheet", async () => {
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["Total", 42], ["<b>x</b>", "a & b"]]), "Q&A");
    XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([["second"]]), "Two");
    const bytes = XLSX.write(wb, { type: "array", bookType: "xlsx" }) as ArrayBuffer;
    vi.mocked(api.fetchFileRaw).mockResolvedValue(bytes);

    const html = await buildPrintDocument("excel", { origin: "disk", path: "book.xlsx" }, "book.xlsx");

    expect(html).toContain("<h2>Q&amp;A</h2>");
    expect(html).toContain("<h2>Two</h2>");
    expect(html.match(/<section>/g)).toHaveLength(2);
    expect(html).toContain("<td>42</td>");
    expect(html).toContain("<td>a &amp; b</td>");
    expect(html).toContain("&lt;b&gt;x&lt;/b&gt;");
    expect(html).not.toContain("<b>x</b>");
  });
});

describe("buildPrintDocument: docx", () => {
  it("serializes docx-preview's body and styles, with images inlined", async () => {
    docx.renderAsync.mockImplementation(async (_data: unknown, body: HTMLElement, styles: HTMLElement) => {
      styles.innerHTML = "<style>.docx{color:#000}</style>";
      body.innerHTML = '<div class="docx-wrapper"><section class="docx"><p>Tom &amp; Jerry &lt;b&gt;</p></section></div>';
    });
    vi.mocked(api.fetchFileRaw).mockResolvedValue(arrayBufferOf(PNG_HEADER));

    const html = await buildPrintDocument("docx", { origin: "disk", path: "a.docx" }, "a.docx");

    expect(html).toContain(".docx{color:#000}");
    expect(html).toContain("<p>Tom &amp; Jerry &lt;b&gt;</p>");
    expect(html).toContain("section.docx");
    const options = docx.renderAsync.mock.calls[0][3];
    expect(options).toEqual({ inWrapper: true, useBase64URL: true });
  });

  it("drops javascript: hyperlinks and keeps web links", async () => {
    docx.renderAsync.mockImplementation(async (_data: unknown, body: HTMLElement) => {
      body.innerHTML =
        '<a href="javascript:alert(1)">bad</a><a href="https://example.com/x">good</a><a href="#top">anchor</a>';
    });
    vi.mocked(api.fetchFileRaw).mockResolvedValue(arrayBufferOf(PNG_HEADER));

    const html = await buildPrintDocument("docx", { origin: "disk", path: "a.docx" }, "a.docx");

    expect(html).not.toContain("javascript:");
    expect(html).toContain('<a>bad</a>');
    expect(html).toContain('<a href="https://example.com/x">good</a>');
    expect(html).toContain('<a href="#top">anchor</a>');
  });

  it("removes script, event handlers and javascript: URLs from the body, and keeps data: images and style attributes", async () => {
    docx.renderAsync.mockImplementation(async (_data: unknown, body: HTMLElement) => {
      body.innerHTML =
        '<p onclick="x()" style="color:red">hi <img src="data:image/png;base64,iVBORw==" onerror="alert(1)"><script>alert(2)</script></p>';
    });
    vi.mocked(api.fetchFileRaw).mockResolvedValue(arrayBufferOf(PNG_HEADER));

    const html = await buildPrintDocument("docx", { origin: "disk", path: "a.docx" }, "a.docx");

    expect(html).not.toContain("alert(");
    expect(html).not.toContain("onclick");
    expect(html).not.toContain("onerror");
    expect(html).toContain('<p style="color:red">hi <img src="data:image/png;base64,iVBORw=="></p>');
  });

  it("does not let a font name in the styles break out of its <style> element", async () => {
    docx.renderAsync.mockImplementation(async (_data: unknown, body: HTMLElement, styles: HTMLElement) => {
      const style = document.createElement("style");
      style.textContent = "@font-face{font-family:X</style><script>window.pwned=1</script><style>}";
      styles.appendChild(style);
      body.innerHTML = "<p>x</p>";
    });
    vi.mocked(api.fetchFileRaw).mockResolvedValue(arrayBufferOf(PNG_HEADER));

    const html = await buildPrintDocument("docx", { origin: "disk", path: "a.docx" }, "a.docx");

    expect(html).not.toContain("window.pwned");
    expect(html).toContain("<p>x</p>");
  });

  it("refuses live editor text, since a Word file has no text to edit", async () => {
    await expect(buildPrintDocument("docx", { origin: "editor", path: "a.docx", content: "" }, "a.docx")).rejects.toThrow(
      "print: a.docx can only be printed from the saved file",
    );
  });
});

describe("buildPrintDocument: pptx", () => {
  async function deckBytes(): Promise<ArrayBuffer> {
    const zip = new JSZip();
    zip.file(
      "ppt/presentation.xml",
      '<?xml version="1.0"?><p:presentation xmlns:p="urn:p" xmlns:r="urn:r"><p:sldSz cx="9144000" cy="5143500"/><p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst></p:presentation>',
    );
    zip.file(
      "ppt/_rels/presentation.xml.rels",
      '<?xml version="1.0"?><Relationships xmlns="urn:rels"><Relationship Id="rId1" Type="urn:slide" Target="slides/slide1.xml"/></Relationships>',
    );
    zip.file(
      "ppt/slides/slide1.xml",
      '<?xml version="1.0"?><p:sld xmlns:p="urn:p" xmlns:a="urn:a"><a:p><a:r><a:t>Q3 &amp; &lt;b&gt;growth&lt;/b&gt;</a:t></a:r></a:p></p:sld>',
    );
    zip.file(
      "ppt/slides/_rels/slide1.xml.rels",
      '<?xml version="1.0"?><Relationships xmlns="urn:rels"><Relationship Id="rId2" Type="http://x/image" Target="../media/image1.png"/></Relationships>',
    );
    zip.file("ppt/media/image1.png", PNG_HEADER);
    return zip.generateAsync({ type: "arraybuffer" });
  }

  it("prints each slide as the viewer's card, with its text escaped and its image inlined", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(await deckBytes());

    const html = await buildPrintDocument("pptx", { origin: "disk", path: "deck.pptx" }, "deck.pptx");

    expect(html).toContain("Q3 &amp; &lt;b&gt;growth&lt;/b&gt;");
    expect(html).not.toContain("<b>growth</b>");
    expect(html).toContain('src="data:image/png;base64,iVBORw=="');
    expect(html.match(/class="slide"/g)).toHaveLength(1);
    expect(html).toContain("aspect-ratio:1.7777777777777777");
  });

  it("refuses live editor text, since a deck has no text to edit", async () => {
    await expect(buildPrintDocument("pptx", { origin: "editor", path: "d.pptx", content: "" }, "d.pptx")).rejects.toThrow(
      "print: d.pptx can only be printed from the saved file",
    );
  });

  it("refuses a deck above the slide cap instead of printing part of it", async () => {
    const slideCount = 201;
    const zip = new JSZip();
    const sldIds = Array.from({ length: slideCount }, (_, i) => `<p:sldId id="${256 + i}" r:id="rId${i + 1}"/>`).join("");
    zip.file(
      "ppt/presentation.xml",
      `<?xml version="1.0"?><p:presentation xmlns:p="urn:p" xmlns:r="urn:r"><p:sldSz cx="9144000" cy="5143500"/><p:sldIdLst>${sldIds}</p:sldIdLst></p:presentation>`,
    );
    const rels = Array.from({ length: slideCount }, (_, i) => `<Relationship Id="rId${i + 1}" Type="urn:slide" Target="slides/slide${i + 1}.xml"/>`).join("");
    zip.file("ppt/_rels/presentation.xml.rels", `<?xml version="1.0"?><Relationships xmlns="urn:rels">${rels}</Relationships>`);
    for (let i = 1; i <= slideCount; i++) {
      zip.file(`ppt/slides/slide${i}.xml`, '<?xml version="1.0"?><p:sld xmlns:p="urn:p" xmlns:a="urn:a"><a:p><a:r><a:t>x</a:t></a:r></a:p></p:sld>');
    }
    vi.mocked(api.fetchFileRaw).mockResolvedValue(await zip.generateAsync({ type: "arraybuffer" }));

    await expect(buildPrintDocument("pptx", { origin: "disk", path: "big.pptx" }, "big.pptx")).rejects.toThrow(
      "print: big.pptx has 201 slides; printing is limited to 200",
    );
  });
});

describe("image kinds", () => {
  // IMAGE_MIME_BY_EXT is the single source for image extensions: every entry is
  // an image kind that prints with its MIME type, and an extension outside it is
  // neither, so the kind classifier and the print MIME table cannot drift apart.
  it.each([...IMAGE_MIME_BY_EXT])("%s is an image kind with its print MIME type", (ext, mime) => {
    const path = `pic${ext}`;
    expect(previewKindForPath(path)).toBe("image");
    expect(imageMimeForPath(path)).toBe(mime);
  });

  it.each([".bmp", ".ico", ".avif", ".tif", ".tiff", ".heic", ".jxl"])("%s is not an image kind and has no print MIME type", (ext) => {
    const path = `pic${ext}`;
    expect(previewKindForPath(path)).not.toBe("image");
    expect(imageMimeForPath(path)).toBeNull();
  });
});
