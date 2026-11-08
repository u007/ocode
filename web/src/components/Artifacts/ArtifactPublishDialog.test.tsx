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
    expect(onPublish.mock.calls[0][0]).toEqual({
      notify_on_publish: false,
      access_mode: "public",
    });
  });

  it("submitting checked sends notify_on_publish: true", async () => {
    const onPublish = vi.fn().mockResolvedValue({});
    render(<ArtifactPublishDialog open onPublish={onPublish} />);
    fireEvent.click(screen.getByLabelText(/notify on publish/i));
    fireEvent.click(screen.getByRole("button", { name: /publish/i }));
    await waitFor(() => expect(onPublish).toHaveBeenCalled());
    expect(onPublish.mock.calls[0][0]).toEqual({
      notify_on_publish: true,
      access_mode: "public",
    });
  });

  it("defaults to public access and hides the password/email fields", () => {
    render(<ArtifactPublishDialog open />);
    expect(screen.getByTestId("access-mode")).toHaveValue("public");
    expect(screen.queryByTestId("access-password")).toBeNull();
    expect(screen.queryByTestId("invite-emails")).toBeNull();
  });

  it("sends the password for password mode", async () => {
    const onPublish = vi.fn().mockResolvedValue({});
    render(<ArtifactPublishDialog open onPublish={onPublish} />);
    fireEvent.change(screen.getByTestId("access-mode"), {
      target: { value: "password" },
    });
    fireEvent.change(screen.getByTestId("access-password"), {
      target: { value: "hunter2hunter2" },
    });
    fireEvent.click(screen.getByRole("button", { name: /publish/i }));
    await waitFor(() => expect(onPublish).toHaveBeenCalled());
    expect(onPublish.mock.calls[0][0]).toEqual({
      notify_on_publish: false,
      access_mode: "password",
      password: "hunter2hunter2",
    });
  });

  it("refuses a short password without calling publish", async () => {
    const onPublish = vi.fn().mockResolvedValue({});
    render(<ArtifactPublishDialog open onPublish={onPublish} />);
    fireEvent.change(screen.getByTestId("access-mode"), {
      target: { value: "password" },
    });
    fireEvent.change(screen.getByTestId("access-password"), {
      target: { value: "short" },
    });
    fireEvent.click(screen.getByRole("button", { name: /publish/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/8 characters/i);
    expect(onPublish).not.toHaveBeenCalled();
  });

  it("splits invite emails on commas, spaces and newlines", async () => {
    const onPublish = vi.fn().mockResolvedValue({});
    render(<ArtifactPublishDialog open onPublish={onPublish} />);
    fireEvent.change(screen.getByTestId("access-mode"), {
      target: { value: "allowlist" },
    });
    fireEvent.change(screen.getByTestId("invite-emails"), {
      target: { value: "a@example.com, b@example.com\nc@example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: /publish/i }));
    await waitFor(() => expect(onPublish).toHaveBeenCalled());
    expect(onPublish.mock.calls[0][0]).toEqual({
      notify_on_publish: false,
      access_mode: "allowlist",
      invite_emails: ["a@example.com", "b@example.com", "c@example.com"],
    });
  });

  it("refuses allowlist mode with no emails", async () => {
    const onPublish = vi.fn().mockResolvedValue({});
    render(<ArtifactPublishDialog open onPublish={onPublish} />);
    fireEvent.change(screen.getByTestId("access-mode"), {
      target: { value: "allowlist" },
    });
    fireEvent.click(screen.getByRole("button", { name: /publish/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /at least one email/i,
    );
    expect(onPublish).not.toHaveBeenCalled();
  });

  it("shows the share link after publishing and keeps the dialog open", async () => {
    const onPublish = vi.fn().mockResolvedValue({
      hub_url: "https://hub.test/p/art_1/tok/",
    });
    const onOpenChange = vi.fn();
    render(
      <ArtifactPublishDialog
        open
        onPublish={onPublish}
        onOpenChange={onOpenChange}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /publish/i }));
    expect(await screen.findByTestId("publish-result-url")).toHaveValue(
      "https://hub.test/p/art_1/tok/",
    );
    expect(screen.queryByTestId("publish-submit")).toBeNull();
    fireEvent.click(screen.getByTestId("publish-done"));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("renders invite links and per-email invite errors", async () => {
    const onPublish = vi.fn().mockResolvedValue({
      hub_url: "https://hub.test/p/art_1/tok/",
      hub_invites: [
        {
          email: "a@example.com",
          inviteUrl: "https://hub.test/i/a",
          emailSent: true,
        },
      ],
      invite_errors: ["b@example.com: hub add invite: status 400"],
    });
    render(<ArtifactPublishDialog open onPublish={onPublish} />);
    fireEvent.change(screen.getByTestId("access-mode"), {
      target: { value: "allowlist" },
    });
    fireEvent.change(screen.getByTestId("invite-emails"), {
      target: { value: "a@example.com,b@example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: /publish/i }));
    expect(
      await screen.findByText("https://hub.test/i/a"),
    ).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(/b@example.com/);
  });

  it("reports a local publish without a hub link", async () => {
    const onPublish = vi.fn().mockResolvedValue({});
    render(<ArtifactPublishDialog open onPublish={onPublish} />);
    fireEvent.click(screen.getByRole("button", { name: /publish/i }));
    expect(await screen.findByTestId("publish-result")).toHaveTextContent(
      /served by this machine/i,
    );
  });
});
