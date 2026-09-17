import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ThinkingBlock, ToolBlock, parseQuestionAnswers } from "./TurnParts";

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
