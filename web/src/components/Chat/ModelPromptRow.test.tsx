import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import ModelPromptRow from "./ModelPromptRow";
import type { ModelPromptInfo } from "../../api/types";

const prompt: ModelPromptInfo = {
  kind: "file",
  path: "/Users/james/www/ocode/deepseek-v4-flash.OCODE.md",
  tokens: 4129,
  kaizen: [
    { name: "conduct-tuning-deepseek-v4-flash", tuned_for: "deepseek-v4-flash", stack: "conduct" },
  ],
};

describe("ModelPromptRow", () => {
  it("renders nothing when the model has no prompt and no kaizen directives", () => {
    const { container } = render(<ModelPromptRow />);
    expect(container.firstChild).toBeNull();
  });

  it("renders nothing for an empty prompt object", () => {
    const { container } = render(<ModelPromptRow prompt={{}} model="opencode-go/x" />);
    expect(container.firstChild).toBeNull();
  });

  it("mirrors the TUI banner: source basename, kind, token count, model id", () => {
    render(<ModelPromptRow prompt={prompt} model="opencode-go/deepseek-v4-flash" />);
    const row = screen.getByTestId("model-prompt-row");
    expect(row.textContent).toContain("◆ Model prompt");
    expect(row.textContent).toContain("deepseek-v4-flash.OCODE.md (file)");
    expect(row.textContent).toContain("~4.1k tok");
    expect(row.textContent).toContain("opencode-go/deepseek-v4-flash");
  });

  it("lists the force-injected Kaizen directives like the TUI notice", () => {
    render(<ModelPromptRow prompt={prompt} model="opencode-go/deepseek-v4-flash" />);
    expect(screen.getByTestId("model-prompt-row").textContent).toContain(
      "Kaizen directives active (force-injected): conduct-tuning-deepseek-v4-flash → deepseek-v4-flash (conduct)",
    );
  });

  it("shows kaizen-only rows without a prompt source", () => {
    render(
      <ModelPromptRow
        prompt={{ kaizen: [{ name: "conduct-tuning-x", tuned_for: "x" }] }}
        model="opencode-go/x"
      />,
    );
    const row = screen.getByTestId("model-prompt-row");
    expect(row.textContent).toContain("Kaizen directives active (force-injected): conduct-tuning-x → x");
    expect(row.textContent).not.toContain("Model prompt");
  });

  it("formats small token counts without a suffix", () => {
    render(<ModelPromptRow prompt={{ kind: "embedded", path: "deepseek-v4-flash.OCODE.md", tokens: 512 }} />);
    expect(screen.getByTestId("model-prompt-row").textContent).toContain("~512 tok");
    expect(screen.getByTestId("model-prompt-row").textContent).toContain("(embedded)");
  });
});