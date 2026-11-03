import { beforeEach, describe, expect, it } from "vitest";
import { buildHtmlFindSrcDoc, htmlFindAgent } from "./htmlFindAgent";

type Agent = ReturnType<typeof htmlFindAgent>;

// Rendered text: "Alpha beta gamma betamax." + "Alpha BETA delta."
const BODY = "<p>Alpha beta gamma betamax.</p><p>Alpha BETA delta.</p>";

/** jsdom has no Range rects; one rect per range is enough to prove painting. */
function stubRangeRects() {
  const proto = Range.prototype as unknown as { getClientRects?: () => DOMRectList };
  const native = proto.getClientRects;
  proto.getClientRects = function () {
    const rect = { left: 10, top: 20, width: 30, height: 12, x: 10, y: 20, right: 40, bottom: 32, toJSON: () => ({}) };
    return [rect] as unknown as DOMRectList;
  };
  return () => {
    if (native) proto.getClientRects = native;
    else delete proto.getClientRects;
  };
}

let api: Agent;

beforeEach(() => {
  document.body.innerHTML = BODY;
  document.getElementById("__ocode_find_layer")?.remove();
  const w = window as unknown as Record<string, unknown>;
  delete w.__ocodeFindAgent;
  api = htmlFindAgent();
});

describe("htmlFindAgent", () => {
  it("finds matches case-insensitively, then honours case and whole word", () => {
    api.search("beta");
    expect(api.matches()).toHaveLength(3);

    api.search("beta", { caseSensitive: true });
    expect(api.matches()).toHaveLength(2);

    api.search("beta", { caseSensitive: true, wholeWord: true });
    expect(api.matches()).toHaveLength(1);
  });

  it("walks matches and wraps at both ends", () => {
    api.search("beta");
    expect(api.current()).toBe(0);
    api.step(1);
    expect(api.current()).toBe(1);
    api.step(-1);
    api.step(-1);
    expect(api.current()).toBe(2);
    api.step(1);
    expect(api.current()).toBe(0);
  });

  it("paints an overlay layer and clears it on close", () => {
    const restore = stubRangeRects();
    try {
      api.search("beta");
      const layer = document.getElementById("__ocode_find_layer");
      expect(layer).not.toBeNull();
      expect(layer?.childElementCount).toBe(3);
      // The current match is painted orange, the others yellow.
      expect(layer?.children[0].getAttribute("style")).toContain("rgba(249, 115, 22");
      expect(layer?.children[1].getAttribute("style")).toContain("rgba(250, 204, 21");

      api.clear();
      expect(document.getElementById("__ocode_find_layer")).toBeNull();
    } finally {
      restore();
    }
  });

  it("reports matches over the same line-based text as the parent surfaces", () => {
    // Whole word must not bridge the two paragraphs.
    document.body.innerHTML = "<p>end</p><p>start</p>";
    api.search("endstart");
    expect(api.matches()).toHaveLength(0);
  });
});

describe("buildHtmlFindSrcDoc", () => {
  it("injects the agent before the closing body tag and keeps the page", () => {
    const srcDoc = buildHtmlFindSrcDoc("<html><body><h1>Hello</h1></body></html>");
    expect(srcDoc.startsWith("<html><body><h1>Hello</h1>")).toBe(true);
    expect(srcDoc).toContain("__ocodeFind");
    expect(srcDoc.indexOf("<script>")).toBeLessThan(srcDoc.indexOf("</body>"));
  });

  it("appends when there is no body/html closing tag", () => {
    const srcDoc = buildHtmlFindSrcDoc("<h1>Hello</h1>");
    expect(srcDoc.startsWith("<h1>Hello</h1>")).toBe(true);
    expect(srcDoc).toContain("__ocodeFind");
  });

  it("serializes a self-contained function that actually runs", () => {
    const srcDoc = buildHtmlFindSrcDoc("<p>plain</p>");
    const start = srcDoc.indexOf("<script>(") + "<script>(".length;
    const end = srcDoc.lastIndexOf(")();</script>");
    expect(start).toBeGreaterThan(0);
    expect(end).toBeGreaterThan(start);
    const source = srcDoc.slice(start, end);

    // Run the SERIALIZED source, not the imported function: a module-scope
    // reference inside the agent would still pass a syntax check but throw
    // ReferenceError in the frame when evaluated from global scope.
    delete (window as unknown as Record<string, unknown>).__ocodeFindAgent;
    document.getElementById("__ocode_find_layer")?.remove();
    const factory = new Function(`return (${source});`) as () => () => Agent;
    const evaluated = factory()();
    evaluated.search("beta");
    expect(evaluated.matches()).toHaveLength(3);
    evaluated.clear();
  });
});
