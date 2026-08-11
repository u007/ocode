import { describe, it, expect } from "vitest";
import { render, waitFor } from "@testing-library/react";
import MessageBubble, { AssistantText } from "./MessageBubble";
import { ToolBlock } from "./TurnParts";

describe("syntax highlighting", () => {
  it("colors a fenced ts block in assistant markdown", async () => {
    const { container } = render(
      <AssistantText content={"```ts\nconst a = 1;\n```"} />,
    );
    await waitFor(() => {
      const colored = container.querySelectorAll('pre code span[style*="color"]');
      expect(colored.length).toBeGreaterThan(0);
    });
    expect(container.querySelector("pre pre")).toBeNull();
    expect(container.textContent).toContain("const a = 1;");
  });

  // rehypeFileLinks skips `pre` subtrees, so a fenced block's children stay raw
  // text nodes and String(children) is safe. If that skip is ever removed this
  // fails loudly instead of silently rendering "[object Object]".
  it("keeps file paths inside a fence intact", async () => {
    const { container } = render(
      <AssistantText
        content={'```ts\nimport { x } from "./web/src/App.tsx";\n```'}
      />,
    );
    await waitFor(() => {
      expect(container.querySelector('pre code span[style*="color"]')).not.toBeNull();
    });
    expect(container.textContent).toContain(
      'import { x } from "./web/src/App.tsx";',
    );
    expect(container.textContent).not.toContain("[object Object]");
  });

  it("leaves inline code uncolored", async () => {
    const { container } = render(<AssistantText content={"use `foo()` here"} />);
    expect(container.querySelector('span[style*="color"]')).toBeNull();
  });

  it("colors tool command json and bash output", async () => {
    const { container } = render(
      <ToolBlock
        tool="bash"
        command={'{"command":"ls -la"}'}
        output={"$ ls -la\ntotal 0"}
      />,
    );
    await waitFor(() => {
      expect(
        container.querySelectorAll('span[style*="color"]').length,
      ).toBeGreaterThan(0);
    });
  });

  it("colors write-tool diff output", async () => {
    const { container } = render(
      <ToolBlock
        tool="write"
        command={'{"path":"a.ts"}'}
        output={"DIFF:a.ts\n@@ new file @@\n+const a = 1;"}
      />,
    );
    await waitFor(() => {
      const spans = Array.from(
        container.querySelectorAll('span[style*="color"]'),
      ).map((s) => s.textContent);
      expect(spans.some((t) => t?.includes("+const a = 1;"))).toBe(true);
    });
  });

  it("falls back to plaintext with mark when the find bar is active", async () => {
    render(
      <ToolBlock tool="bash" command={'{"command":"ls"}'} output="hello" highlight="ls" />,
    );
    await waitFor(() => {
      expect(document.querySelectorAll("mark").length).toBeGreaterThan(0);
    });
    expect(document.querySelector('span[style*="color"]')).toBeNull();
  });

  // Role "tool" messages carry only tool_call_id, not the tool's name — with no
  // toolName supplied (e.g. the id couldn't be resolved), output stays plain.
  it("leaves a replayed tool-result message unhighlighted when toolName is unresolved", async () => {
    const { container } = render(
      <MessageBubble
        message={{ role: "tool", content: "DIFF:a.ts\n@@ new file @@\n+const a = 1;" } as never}
      />,
    );
    await new Promise((r) => setTimeout(r, 200));
    expect(container.querySelector('span[style*="color"]')).toBeNull();
  });

  // ChatPanel resolves tool_call_id -> tool name from the preceding assistant
  // message's tool_calls and passes it down as toolName; this pins that once
  // resolved, replayed history highlights the same as the live stream.
  it("highlights a replayed tool-result message when toolName is resolved", async () => {
    const { container } = render(
      <MessageBubble
        message={{ role: "tool", content: "DIFF:a.ts\n@@ new file @@\n+const a = 1;" } as never}
        toolName="write"
      />,
    );
    await waitFor(() => {
      expect(container.querySelector('span[style*="color"]')).not.toBeNull();
    });
  });

  it("leaves read output unhighlighted", async () => {
    const { container } = render(
      <ToolBlock tool="read" output={"1\tconst a = 1;"} />,
    );
    await new Promise((r) => setTimeout(r, 200));
    const outputPre = container.querySelectorAll("pre")[0];
    expect(outputPre.querySelector('span[style*="color"]')).toBeNull();
    expect(outputPre.textContent).toContain("1\tconst a = 1;");
  });
});
