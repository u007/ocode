import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import QuestionDialog from "./QuestionDialog";
import type {
  QuestionAnswerPayload,
  QuestionPrompt,
} from "@/api/types";
import type { AskContext } from "@/stores/chatStore";

function renderDialog(
  overrides: Partial<{
    questions: QuestionPrompt[];
    onSubmit: (
      requestId: string,
      answers: QuestionAnswerPayload[],
    ) => Promise<boolean>;
    onCancel: (requestId: string) => Promise<boolean>;
    context: AskContext;
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
  const onCancel = vi.fn(async () => true);
  const requestId = "req-1";
  const view = render(
    <QuestionDialog
      open={true}
      requestId={requestId}
      questions={overrides.questions ?? questions}
      onSubmit={overrides.onSubmit ?? onSubmit}
      onCancel={overrides.onCancel ?? onCancel}
      context={overrides.context}
    />,
  );
  return {
    onSubmit,
    onCancel,
    requestId,
    unmount: () => view.unmount(),
  };
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

  // Regression: the prompt used to be non-dismissible — the only way out was
  // answering every question. Cancel mirrors the TUI's Esc-to-cancel.
  it("cancels without answering when the Cancel button is clicked", async () => {
    const { onCancel, onSubmit, requestId } = renderDialog();
    fireEvent.click(screen.getByRole("button", { name: /^cancel$/i }));
    await waitFor(() => expect(onCancel).toHaveBeenCalledWith(requestId));
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("cancels on Escape (Radix open-change) without a selection", async () => {
    const { onCancel, requestId } = renderDialog();
    fireEvent.keyDown(document.body, { key: "Escape", code: "Escape" });
    await waitFor(() => expect(onCancel).toHaveBeenCalledWith(requestId));
  });

  it("keeps the dialog retryable when cancel fails", async () => {
    const onCancel = vi.fn(async () => false);
    const { requestId } = renderDialog({ onCancel });
    const cancel = screen.getByRole("button", { name: /^cancel$/i });
    fireEvent.click(cancel);
    await waitFor(() => expect(onCancel).toHaveBeenCalledWith(requestId));
    // Failure keeps the dialog mounted and the button usable for a retry.
    await waitFor(() => expect((cancel as HTMLButtonElement).disabled).toBe(false));
  });
});

describe("QuestionDialog model context", () => {
  it("shows the last model message and its thinking", () => {
    renderDialog({
      context: {
        text: "I need a decision before writing the migration.",
        thinking: "The schema change is ambiguous.",
      },
    });
    const region = screen.getByRole("region", { name: "Last model message" });
    expect(region.textContent).toContain(
      "I need a decision before writing the migration.",
    );
    expect(region.textContent).toContain(
      "The schema change is ambiguous.",
    );
  });

  it("renders no model-context panel without a context", () => {
    renderDialog();
    expect(
      screen.queryByRole("region", { name: "Last model message" }),
    ).toBeNull();
  });
});

describe("QuestionDialog viewport bounding", () => {
  it("caps the dialog to the viewport and scrolls a tall prompt internally", () => {
    renderDialog({
      questions: [
        {
          header: "Scope of data",
          question: "Pick one? " + "q".repeat(400),
          options: [
            { label: "Visual parity only", description: "d".repeat(300) },
            { label: "Full DevTools parity", description: "e".repeat(300) },
          ],
        },
      ],
      context: {
        text: "A long model message. ".repeat(200),
        thinking: "Thinking hard. ".repeat(200),
      },
    });

    const content = screen
      .getByText("Scope of data")
      .closest('[role="dialog"]') as HTMLElement | null;
    expect(content).toBeTruthy();

    // Height is capped by the shared dvh-aware utility (no fixed height, no
    // unbounded growth) and the content is a flex column so one child can
    // scroll while the header/footer stay put.
    expect(content!.className).toContain("dialog-viewport-max");
    expect(content!.className).toContain("flex-col");
    expect(content!.className).toContain("overflow-hidden");

    // The scrollable region owns the tall content…
    const scroller = content!.querySelector(".overflow-y-auto") as HTMLElement | null;
    expect(scroller).toBeTruthy();
    expect(scroller!.className).toContain("flex-1");
    expect(scroller!.className).toContain("min-h-0");
    expect(scroller!.contains(screen.getByText(/Pick one\?/))).toBe(true);

    // …and the decisions are pinned outside it, so Submit/Deny can never be
    // scrolled out of reach on a long prompt.
    const submit = screen.getByRole("button", { name: /submit/i });
    const cancel = screen.getByRole("button", { name: /^cancel$/i });
    expect(scroller!.contains(submit)).toBe(false);
    expect(scroller!.contains(cancel)).toBe(false);
    expect(submit.closest(".shrink-0")).toBeTruthy();
  });
});