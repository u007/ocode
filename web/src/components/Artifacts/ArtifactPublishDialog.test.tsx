import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import ArtifactPublishDialog from "./ArtifactPublishDialog";

describe("ArtifactPublishDialog", () => {
  it("defaults notify_on_publish to unchecked", () => {
    render(<ArtifactPublishDialog open />);
    expect(screen.getByLabelText(/notify on publish/i)).not.toBeChecked();
  });

  it("submitting unchecked sends notify_on_publish: false", async () => {
    const onPublish = vi.fn().mockResolvedValue({});
    render(<ArtifactPublishDialog open onPublish={onPublish} />);
    fireEvent.click(screen.getByRole("button", { name: /publish/i }));
    await waitFor(() => expect(onPublish).toHaveBeenCalled());
    expect(onPublish.mock.calls[0][0]).toEqual({ notify_on_publish: false });
  });

  it("submitting checked sends notify_on_publish: true", async () => {
    const onPublish = vi.fn().mockResolvedValue({});
    render(<ArtifactPublishDialog open onPublish={onPublish} />);
    fireEvent.click(screen.getByLabelText(/notify on publish/i));
    fireEvent.click(screen.getByRole("button", { name: /publish/i }));
    await waitFor(() => expect(onPublish).toHaveBeenCalled());
    expect(onPublish.mock.calls[0][0]).toEqual({ notify_on_publish: true });
  });
});
