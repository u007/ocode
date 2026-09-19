import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import CompactionNotice, {
  COMPACTION_SUMMARY_MARKER,
  isCompactionSummary,
  parseCompactionSummary,
} from "./CompactionNotice";

const SUMMARY = [
  COMPACTION_SUMMARY_MARKER,
  "Compacted summary covering 24 messages",
  "",
  "## Original Request",
  "",
  "Build the thing.",
].join("\n");

describe("isCompactionSummary", () => {
  it("detects the persisted compaction marker", () => {
    expect(isCompactionSummary(SUMMARY)).toBe(true);
    expect(isCompactionSummary("just a message")).toBe(false);
  });

  it("tolerates leading whitespace", () => {
    expect(isCompactionSummary(`\n  ${COMPACTION_SUMMARY_MARKER}\nheader`)).toBe(true);
  });
});

describe("parseCompactionSummary", () => {
  it("splits the header line from the markdown body", () => {
    const { header, body } = parseCompactionSummary(SUMMARY);
    expect(header).toBe("Compacted summary covering 24 messages");
    expect(body).toBe("## Original Request\n\nBuild the thing.");
  });

  it("handles a marker with no body", () => {
    expect(parseCompactionSummary(`${COMPACTION_SUMMARY_MARKER}\nheader only`)).toEqual({
      header: "header only",
      body: "",
    });
  });

  it("handles a bare marker", () => {
    expect(parseCompactionSummary(COMPACTION_SUMMARY_MARKER)).toEqual({ header: "", body: "" });
  });
});

describe("CompactionNotice", () => {
  it("renders the header collapsed and reveals the summary body on click", () => {
    render(<CompactionNotice content={SUMMARY} />);
    expect(screen.getByTestId("compaction-notice")).toBeInTheDocument();
    expect(screen.getByText("Compacted summary covering 24 messages")).toBeInTheDocument();
    // Body is collapsed by default but mounted on expand.
    expect(screen.queryByRole("heading", { name: "Original Request" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button"));
    expect(screen.getByRole("button")).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("heading", { name: "Original Request" })).toBeInTheDocument();
  });

  it("renders a bare marker without a header or body", () => {
    render(<CompactionNotice content={COMPACTION_SUMMARY_MARKER} />);
    expect(screen.getByText("Compacted summary")).toBeInTheDocument();
  });
});
