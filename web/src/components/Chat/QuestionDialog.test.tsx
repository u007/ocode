import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import QuestionDialog from "./QuestionDialog";
import type {
  QuestionAnswerPayload,
  QuestionPrompt,
} from "@/api/types";

function renderDialog(
  overrides: Partial<{
    questions: QuestionPrompt[];
    onSubmit: (
      requestId: string,
      answers: QuestionAnswerPayload[],
    ) => Promise<boolean>;
  }> = {},
) {
  const questions: QuestionPrompt[] = [
    {
      header: "Scope of data",
      question: "Pick one?",
      options: [
        { label: "Visual parity only", description: "Fastest, no backend risk" },
        { label: "Full DevTools parity", description: "Largest scope" },
      ],
    },
  ];
  const onSubmit = vi.fn(async () => true);
  const requestId = "req-1";
  const view = render(
    <QuestionDialog
      open={true}
      requestId={requestId}
      questions={overrides.questions ?? questions}
      onSubmit={overrides.onSubmit ?? onSubmit}
    />,
  );
  return { onSubmit, requestId, unmount: () => view.unmount() };
}

describe("QuestionDialog", () => {
  it("renders the header, question, options and a free-text 'Something else' row", () => {
    renderDialog();
    expect(screen.getByText("Scope of data")).toBeTruthy();
    expect(screen.getByText("Pick one?")).toBeTruthy();
    expect(screen.getByText("Visual parity only")).toBeTruthy();
    expect(screen.getByText("Full DevTools parity")).toBeTruthy();
    // The extra custom row is always appended (TUI parity).
    expect(screen.getByText("Something else")).toBeTruthy();
  });

  it("submits the request id and the selected option label", async () => {
    const { onSubmit, requestId } = renderDialog();
    const submit = screen.getByRole("button", { name: /submit/i });
    // Nothing selected yet — the submit button is disabled.
    expect((submit as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByText("Visual parity only"));
    expect((submit as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(submit);
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(requestId, [
        {
          header: "Scope of data",
          question: "Pick one?",
          answers: [{ label: "Visual parity only" }],
        },
      ]),
    );
  });

  it("selecting another option replaces the radio selection", async () => {
    const { onSubmit } = renderDialog();
    fireEvent.click(screen.getByText("Visual parity only"));
    fireEvent.click(screen.getByText("Full DevTools parity"));
    fireEvent.click(screen.getByRole("button", { name: /submit/i }));
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith("req-1", [
        {
          header: "Scope of data",
          question: "Pick one?",
          answers: [{ label: "Full DevTools parity" }],
        },
      ]),
    );
  });

  it("requires free text when the custom row is chosen and sends custom:true", async () => {
    const { onSubmit } = renderDialog();
    const submit = screen.getByRole("button", { name: /submit/i });
    fireEvent.click(screen.getByText("Something else"));
    // Custom row selected but no text — submit stays disabled.
    expect((submit as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(screen.getByPlaceholderText("Type your answer…"), {
      target: { value: "my own answer" },
    });
    expect((submit as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(submit);
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith("req-1", [
        {
          header: "Scope of data",
          question: "Pick one?",
          answers: [
            { label: "Something else", text: "my own answer", custom: true },
          ],
        },
      ]),
    );
  });

  it("keeps multiple selections for multiple:true questions", async () => {
    const { onSubmit } = renderDialog({
      questions: [
        {
          header: "Multi",
          question: "Pick any?",
          multiple: true,
          options: [{ label: "A" }, { label: "B" }],
        },
      ],
    });
    fireEvent.click(screen.getByText("A"));
    fireEvent.click(screen.getByText("B"));
    fireEvent.click(screen.getByRole("button", { name: /submit/i }));
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith("req-1", [
        {
          header: "Multi",
          question: "Pick any?",
          answers: [{ label: "A" }, { label: "B" }],
        },
      ]),
    );
  });

  it("re-enables submit when the answer round fails (dialog stays open)", async () => {
    const onSubmit = vi.fn(async () => false);
    const { unmount } = renderDialog({ onSubmit });
    fireEvent.click(screen.getByText("Visual parity only"));
    const submit = screen.getByRole("button", { name: /submit/i });
    fireEvent.click(submit);
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    // Failure (e.g. network) keeps the dialog open and the button retryable.
    await waitFor(() => expect((submit as HTMLButtonElement).disabled).toBe(false));
    unmount();
  });
});