import { describe, it, expect } from "vitest";
import {
  DOM_NODE_LIMIT,
  findNode,
  normalizeChromeTree,
  normalizeLocalTree,
  pathToNode,
  rowLabel,
  selectorFor,
  serializeNode,
  type DomNode,
} from "./domInspector";

function chromeNode(overrides: Record<string, unknown>): Record<string, unknown> {
  return { nodeType: 1, attributes: [], ...overrides };
}

describe("normalizeChromeTree", () => {
  it("renders the html element as the root and flattens Chrome's attribute pairs", () => {
    const tree = normalizeChromeTree({
      root: {
        nodeId: 1,
        nodeType: 9,
        children: [{
          nodeId: 2,
          nodeType: 1,
          localName: "html",
          nodeName: "HTML",
          attributes: [],
          children: [
            chromeNode({ nodeId: 3, localName: "body", nodeName: "BODY", attributes: [], children: [
              chromeNode({ nodeId: 4, localName: "button", nodeName: "BUTTON", attributes: ["id", "save", "class", "primary"] }),
              { nodeId: 5, nodeType: 3, nodeValue: "  Save  " },
              { nodeId: 6, nodeType: 3, nodeValue: "   " },
              { nodeId: 7, nodeType: 8, nodeValue: "comment" },
            ] }),
          ],
        }],
      },
    });
    expect(tree.root.id).toBe("2");
    expect(tree.root.tag).toBe("html");
    const body = tree.root.children?.[0];
    expect(body?.tag).toBe("body");
    const button = body?.children?.[0];
    expect(button).toMatchObject({ id: "4", tag: "button", attrs: { id: "save", class: "primary" } });
    // Whitespace-only text and comments are dropped; a text node keeps trimmed text.
    expect(body?.children).toHaveLength(2);
    expect(body?.children?.[1]).toMatchObject({ tag: "#text", text: "Save" });
    expect(tree.truncated).toBe(false);
  });

  it("rejects a reply with no document element", () => {
    expect(() => normalizeChromeTree({ root: { nodeId: 1, nodeType: 9 } })).toThrow(/no document element/);
    expect(() => normalizeChromeTree({})).toThrow(/no document/);
  });

  it("caps the tree and marks it truncated", () => {
    const kids = Array.from({ length: DOM_NODE_LIMIT + 50 }, (_, i) =>
      chromeNode({ nodeId: 100 + i, localName: "div", nodeName: "DIV" }),
    );
    const tree = normalizeChromeTree({
      root: { nodeId: 1, nodeType: 9, children: [{ nodeId: 2, nodeType: 1, localName: "html", attributes: [], children: kids }] },
    });
    expect(tree.truncated).toBe(true);
    expect(tree.nodes).toBeLessThanOrEqual(DOM_NODE_LIMIT);
  });
});

describe("normalizeLocalTree", () => {
  it("re-types a hostile payload instead of trusting it", () => {
    const tree = normalizeLocalTree({
      root: {
        id: "0",
        tag: "html",
        attrs: { id: 7 as unknown as string },
        children: [
          { id: "0/1", tag: "#text", text: 42 as unknown as string },
          { id: "", tag: "ignored" },
          { id: "0/3", tag: "div", children: "not-an-array" as unknown as unknown[] },
        ],
      },
      truncated: "yes" as unknown as boolean,
    });
    expect(tree.root.attrs.id).toBe("7");
    expect(tree.root.children?.map((c) => c.tag)).toEqual(["#text", "div"]);
    expect(tree.root.children?.[0].text).toBe("42");
    expect(tree.root.children?.[1].children).toBeUndefined();
    expect(tree.truncated).toBe(false);
  });

  it("throws without a root", () => {
    expect(() => normalizeLocalTree({})).toThrow(/no DOM tree/);
  });
});

const TREE: DomNode = {
  id: "0",
  tag: "html",
  attrs: {},
  children: [
    {
      id: "1",
      tag: "body",
      attrs: {},
      children: [
        {
          id: "2",
          tag: "div",
          attrs: { id: "app", class: "shell" },
          children: [
            { id: "3", tag: "button", attrs: { class: "primary" }, children: [{ id: "4", tag: "#text", attrs: {}, text: "Save" }] },
            { id: "5", tag: "button", attrs: { class: "secondary" } },
          ],
        },
      ],
    },
  ],
};

describe("tree helpers", () => {
  it("finds a node and its ancestor chain", () => {
    expect(pathToNode(TREE, "4")?.map((n) => n.id)).toEqual(["0", "1", "2", "3", "4"]);
    expect(findNode(TREE, "5")?.attrs.class).toBe("secondary");
    expect(findNode(TREE, "nope")).toBeNull();
  });

  it("anchors a selector at the nearest id and uses nth-of-type otherwise", () => {
    expect(selectorFor(TREE, "3")).toBe("#app > button:nth-of-type(1)");
    expect(selectorFor(TREE, "2")).toBe("#app");
    // A text node has no element selector.
    expect(selectorFor(TREE, "4")).toBeNull();
    // Without any id the chain starts at the root and numbers each level.
    const noIds: DomNode = {
      id: "0", tag: "html", attrs: {}, children: [
        { id: "1", tag: "body", attrs: {}, children: [{ id: "2", tag: "p", attrs: {} }, { id: "3", tag: "p", attrs: {} }] },
      ],
    };
    expect(selectorFor(noIds, "3")).toBe("html > body > p:nth-of-type(2)");
  });

  it("serializes with escaping and void elements", () => {
    const node: DomNode = {
      id: "1",
      tag: "div",
      attrs: { title: 'a "quoted" <tag>' },
      children: [{ id: "2", tag: "img", attrs: { src: "x.png" } }, { id: "3", tag: "#text", attrs: {}, text: "a & b" }],
    };
    expect(serializeNode(node)).toBe('<div title="a &quot;quoted&quot; &lt;tag&gt;"><img src="x.png">a &amp; b</div>');
  });

  it("labels a row with id, classes and a couple of attributes", () => {
    expect(rowLabel({ id: "1", tag: "div", attrs: { id: "app", class: "a b c", role: "main" } })).toBe('div#app.a.b role="main"');
    expect(rowLabel({ id: "2", tag: "#text", attrs: {}, text: "hi" })).toBe('"hi"');
  });
});
