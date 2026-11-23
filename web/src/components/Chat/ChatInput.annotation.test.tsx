// Browser annotation → composer. App resolves the owning chat and calls the
// imperative `addAnnotation`; these tests pin the composer half: the note is
// APPENDED to the draft (never sent, never overwriting typed text), the
// screenshot goes through the ordinary upload path so the existing send-time
// `@.ocode/uploads/<name>` prefix carries it, and a failed upload still leaves
// the note in place with no fabricated file reference.
import { createRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import ChatInput, { type ChatInputHandle } from "./ChatInput";
import { clearDraft, getDraft } from "../../lib/tabDrafts";

vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage: vi.fn().mockResolvedValue(true),
    executeShell: vi.fn().mockResolvedValue({ output: "", exitCode: 0, error: "" }),
    stop: vi.fn(),
    isStreaming: false,
    pendingPermission: null,
  }),
}));

vi.mock("../../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../api/client")>()),
  apiPath: (path: string) => path,
  authHeaders: () => ({}),
}));

const TAB = "annotation-tab";

function textarea(): HTMLTextAreaElement {
  return screen.getByPlaceholderText(/Type a message/i) as HTMLTextAreaElement;
}

function uploadResponse(name: string) {
  return { json: async () => [{ name }] } as unknown as Response;
}

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  clearDraft(TAB);
  fetchMock = vi.fn().mockResolvedValue(uploadResponse("annotation-1.png"));
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  clearDraft(TAB);
});

describe("ChatInput.addAnnotation", () => {
  it("appends the note and uploads the screenshot through the attachments path", async () => {
    const ref = createRef<ChatInputHandle>();
    render(<ChatInput ref={ref} sessionTabId={TAB} projectPath="/proj" />);

    const image = new Blob(["png-bytes"], { type: "image/png" });
    await act(async () => {
      ref.current?.addAnnotation({ text: "Look at the hero", image, imageName: "annotation.png" });
    });

    expect(textarea().value).toContain("Look at the hero");
    // Draft is persisted through the same path typing uses.
    expect(getDraft(TAB)).toContain("Look at the hero");

    // The upload landed on the project-scoped attachments endpoint, and its
    // multipart body carried the annotation Blob under its file name. (The
    // composer also polls unrelated endpoints, so select the upload call.)
    const uploads = fetchMock.mock.calls.filter(([u]) => String(u).includes("/api/uploads"));
    expect(uploads).toHaveLength(1);
    const [url, init] = uploads[0] as [string, RequestInit];
    expect(url).toBe("/api/uploads?project=%2Fproj");
    expect(init.method).toBe("POST");
    const sent = (init.body as FormData).get("file") as File;
    expect(sent).toBeInstanceOf(File);
    expect(sent.name).toBe("annotation.png");
    expect(sent.size).toBe(image.size);
    expect(sent.type).toBe("image/png");

    // The returned name becomes a chip, which is what send() turns into
    // `@.ocode/uploads/annotation-1.png`.
    expect(await screen.findByText("annotation-1.png")).toBeTruthy();
  });

  it("appends to typed text instead of overwriting it", async () => {
    const ref = createRef<ChatInputHandle>();
    render(<ChatInput ref={ref} sessionTabId={TAB} projectPath="/proj" />);

    fireEvent.change(textarea(), { target: { value: "my draft" } });
    await act(async () => {
      ref.current?.addAnnotation({ text: "Note: broken layout" });
    });

    expect(textarea().value).toBe("my draft\n\nNote: broken layout");
    expect(getDraft(TAB)).toBe("my draft\n\nNote: broken layout");
    // No image → no upload.
    expect(fetchMock.mock.calls.filter(([u]) => String(u).includes("/api/uploads"))).toHaveLength(0);
  });

  it("keeps the note when the screenshot upload fails, with no file ref invented", async () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    fetchMock.mockRejectedValue(new Error("boom"));
    const ref = createRef<ChatInputHandle>();
    render(<ChatInput ref={ref} sessionTabId={TAB} projectPath="/proj" />);

    const image = new Blob(["png-bytes"], { type: "image/png" });
    await act(async () => {
      ref.current?.addAnnotation({ text: "Still useful", image });
    });

    await waitFor(() => expect(errorSpy).toHaveBeenCalled());
    expect(textarea().value).toContain("Still useful");
    expect(screen.queryByText(/annotation-1\.png/)).toBeNull();
    // Nothing fabricated into the text either.
    expect(textarea().value).not.toContain(".ocode/uploads");
  });
});
