import { render as rtlRender, screen, fireEvent } from "@testing-library/react";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { ThinkingBlock, ToolBlock, parseQuestionAnswers } from "./TurnParts";
import { ChatDisplayTestProvider } from "./chatDisplayTestUtils";

// Every rendered block reads the controlled ChatDisplayContext; production
// throws without a provider, so direct component tests supply a Full one.
function render(ui: ReactElement) {
  return rtlRender(<ChatDisplayTestProvider>{ui}</ChatDisplayTestProvider>);
}

describe("ThinkingBlock", () => {
  it("renders the Speak button when onSpeak is provided", () => {
    render(<ThinkingBlock text="some reasoning" onSpeak={() => {}} />);
    expect(screen.getByRole("button", { name: /speak thinking/i })).toBeInTheDocument();
  });

  it("calls onSpeak when Speak is clicked", () => {
    const onSpeak = vi.fn();
    render(<ThinkingBlock text="some reasoning" onSpeak={onSpeak} />);
    fireEvent.click(screen.getByRole("button", { name: /speak thinking/i }));
    expect(onSpeak).toHaveBeenCalledTimes(1);
  });

  it("does not render the Speak button when onSpeak is omitted", () => {
    render(<ThinkingBlock text="some reasoning" />);
    expect(screen.queryByRole("button", { name: /speak thinking/i })).not.toBeInTheDocument();
  });
});

describe("parseQuestionAnswers", () => {
  it("parses the answered payload the server echoes to the model", () => {
    const payload = [
      { header: "Deploy target", question: "Where?", answers: [{ label: "Staging" }] },
    ];
    expect(parseQuestionAnswers(JSON.stringify(payload))).toEqual(payload);
  });

  it("returns null for the unanswered sentinel and non-answer content", () => {
    expect(
      parseQuestionAnswers("QUESTION_PROMPT:\n[]\n\nWAITING_FOR_USER_RESPONSE"),
    ).toBeNull();
    expect(parseQuestionAnswers("[1,2,3]")).toBeNull();
    expect(parseQuestionAnswers("not json")).toBeNull();
    expect(parseQuestionAnswers(undefined)).toBeNull();
  });
});

describe("ToolBlock question rendering", () => {
  it("renders the questions and selected answers instead of raw JSON", () => {
    const answers = [
      {
        header: "Deploy target",
        question: "Where?",
        answers: [{ label: "Something else", text: "prod-eu", custom: true }],
      },
    ];
    render(
      <ToolBlock
        tool="question"
        command='{"questions":[]}'
        output={JSON.stringify(answers)}
      />,
    );
    expect(screen.getByText("Answered question prompt")).toBeInTheDocument();
    expect(screen.getByText("Deploy target")).toBeInTheDocument();
    expect(screen.getByText("Where?")).toBeInTheDocument();
    expect(screen.getByText(/Something else: “prod-eu”/)).toBeInTheDocument();
    // The raw result JSON must never leak into the transcript.
    expect(screen.queryByText(JSON.stringify(answers))).not.toBeInTheDocument();
  });

  it("does not render the raw QUESTION_PROMPT sentinel for a pending call", () => {
    const sentinel =
      'QUESTION_PROMPT:\n[{"header":"h","question":"q?","options":[]}]\n\nWAITING_FOR_USER_RESPONSE';
    render(<ToolBlock tool="question" command="{}" output={sentinel} />);
    expect(screen.queryByText(/QUESTION_PROMPT:/)).not.toBeInTheDocument();
  });
});

describe("parseQuestionAnswers", () => {
  it("parses the answered payload the server echoes to the model", () => {
    const payload = [
      { header: "Deploy target", question: "Where?", answers: [{ label: "Staging" }] },
    ];
    expect(parseQuestionAnswers(JSON.stringify(payload))).toEqual(payload);
  });

  it("returns null for the unanswered sentinel and non-answer content", () => {
    expect(
      parseQuestionAnswers("QUESTION_PROMPT:\n[]\n\nWAITING_FOR_USER_RESPONSE"),
    ).toBeNull();
    expect(parseQuestionAnswers("[1,2,3]")).toBeNull();
    expect(parseQuestionAnswers("not json")).toBeNull();
    expect(parseQuestionAnswers(undefined)).toBeNull();
  });
});

describe("ToolBlock question rendering", () => {
  it("renders the questions and selected answers instead of raw JSON", () => {
    const answers = [
      {
        header: "Deploy target",
        question: "Where?",
        answers: [{ label: "Something else", text: "prod-eu", custom: true }],
      },
    ];
    render(
      <ToolBlock
        tool="question"
        command='{"questions":[]}'
        output={JSON.stringify(answers)}
      />,
    );
    expect(screen.getByText("Answered question prompt")).toBeInTheDocument();
    expect(screen.getByText("Deploy target")).toBeInTheDocument();
    expect(screen.getByText("Where?")).toBeInTheDocument();
    expect(screen.getByText(/Something else: “prod-eu”/)).toBeInTheDocument();
    // The raw result JSON must never leak into the transcript.
    expect(screen.queryByText(JSON.stringify(answers))).not.toBeInTheDocument();
  });

  it("does not render the raw QUESTION_PROMPT sentinel for a pending call", () => {
    const sentinel =
      'QUESTION_PROMPT:\n[{"header":"h","question":"q?","options":[]}]\n\nWAITING_FOR_USER_RESPONSE';
    render(<ToolBlock tool="question" command="{}" output={sentinel} />);
    expect(screen.queryByText(/QUESTION_PROMPT:/)).not.toBeInTheDocument();
  });
});

// read/write/bash arguments are summarized in the header (mirroring the TUI's
// formatToolCallHint); their raw JSON parameters are never dumped into the
// transcript. write is the worst offender because the JSON carries the whole
// file body.
describe("ToolBlock argument summary", () => {
  it("summarizes read in the header and hides the raw JSON", () => {
    const args = '{"path":"/src/app.ts","offset":10,"limit":20}';
    render(<ToolBlock tool="read" command={args} />);
    expect(screen.getByText(/read \/src\/app\.ts offset=10 limit=20/)).toBeInTheDocument();
    expect(screen.queryByText(args)).not.toBeInTheDocument();
    expect(screen.queryByText(/"offset"/)).not.toBeInTheDocument();
  });

  it("summarizes bash in the header and hides the raw JSON", () => {
    const args = '{"command":"ls -la /tmp"}';
    render(<ToolBlock tool="bash" command={args} />);
    expect(screen.getByText(/\$ ls -la \/tmp/)).toBeInTheDocument();
    expect(screen.queryByText(args)).not.toBeInTheDocument();
    expect(screen.queryByText(/"command"/)).not.toBeInTheDocument();
  });

  it("summarizes write as the path and never renders the file body", () => {
    const args = '{"path":"/src/new.ts","content":"SECRET_BODY"}';
    render(<ToolBlock tool="write" command={args} />);
    expect(screen.getByText(/write \/src\/new\.ts/)).toBeInTheDocument();
    expect(screen.queryByText(args)).not.toBeInTheDocument();
    expect(screen.queryByText(/SECRET_BODY/)).not.toBeInTheDocument();
  });

  it("still renders raw arguments for tools without a summary", () => {
    const args = '{"pattern":"**/*.ts"}';
    render(<ToolBlock tool="glob" command={args} />);
    expect(screen.getByText(/\{.*"pattern".*\}/)).toBeInTheDocument();
  });
});

// Bash tool-call blocks: command/result text is kept intact (no soft wrap) and
// scrolled horizontally so column-aligned output stays readable. A command that
// does not fit the header line is repeated in full in its own code block.
describe("ToolBlock bash code block", () => {
  it("repeats a bash command that doesn't fit one line in a non-wrapping scroll block", () => {
    const cmd = "echo " + "x".repeat(120);
    const { container } = render(
      <ToolBlock tool="bash" command={JSON.stringify({ command: cmd })} />,
    );
    const block = container.querySelector("pre.overflow-x-auto.whitespace-pre");
    expect(block).not.toBeNull();
    expect(block!.textContent).toContain(`$ ${cmd}`);
  });

  it("repeats a multi-line bash command even when it is short", () => {
    const cmd = "cd /tmp\nls";
    const { container } = render(
      <ToolBlock tool="bash" command={JSON.stringify({ command: cmd })} />,
    );
    const block = container.querySelector("pre.overflow-x-auto.whitespace-pre");
    expect(block).not.toBeNull();
    expect(block!.textContent).toContain("cd /tmp\nls");
  });

  it("keeps a short single-line bash command inline only", () => {
    const { container } = render(
      <ToolBlock tool="bash" command={'{"command":"ls -la /tmp"}'} />,
    );
    expect(container.querySelector("pre.overflow-x-auto.whitespace-pre")).toBeNull();
  });

  it("renders bash output without wrapping so wide lines scroll horizontally", () => {
    const { container } = render(
      <ToolBlock
        tool="bash"
        command={'{"command":"cat big.txt"}'}
        output={"a".repeat(200) + "\nsecond"}
      />,
    );
    expect(container.querySelector("span.whitespace-pre")).not.toBeNull();
    expect(container.querySelector("span.whitespace-pre-wrap")).toBeNull();
    expect(container.querySelector("div.overflow-x-auto")).not.toBeNull();
  });

  it("keeps soft wrapping for non-bash tool output", () => {
    const { container } = render(
      <ToolBlock tool="grep" command={'{"pattern":"x"}'} output={"a".repeat(200)} />,
    );
    expect(container.querySelector("span.whitespace-pre")).toBeNull();
    expect(container.querySelector("span.whitespace-pre-wrap")).not.toBeNull();
  });
});
