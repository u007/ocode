import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import BlobDialog from "./BlobDialog";
import { api, ApiError } from "../../api/client";

vi.mock("../../api/client", () => ({
  ApiError: class ApiError extends Error {
    readonly status: number;
    constructor(message: string, status: number) {
      super(message);
      this.status = status;
    }
  },
  api: {
    dbBlobDownload: vi.fn(),
    dbBlobUpload: vi.fn(),
  },
}));

const download = vi.mocked(api.dbBlobDownload);
const upload = vi.mocked(api.dbBlobUpload);

const PNG = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4]);

function ready(data: Uint8Array, mediaType: string) {
  return { data: data.buffer as ArrayBuffer, mediaType, byteLength: data.length, isNull: false };
}

const baseProps = {
  open: true,
  path: "app.db",
  table: "files",
  column: "data",
  rowKey: { id: 1 },
  cell: { bytes: 12, truncated: false },
  editable: true,
  onClose: vi.fn(),
};

beforeEach(() => {
  download.mockReset();
  // A default so a test that only asserts the pre-fetch notice does not blow up
  // on an undefined promise.
  download.mockResolvedValue(ready(new Uint8Array([1]), "application/octet-stream"));
  upload.mockReset();
  baseProps.onClose = vi.fn();
  vi.stubGlobal("URL", {
    ...URL,
    createObjectURL: vi.fn(() => "blob:fake"),
    revokeObjectURL: vi.fn(),
  });
});
afterEach(() => vi.unstubAllGlobals());

describe("BlobDialog", () => {
  it("fetches the full value rather than trusting the grid preview", async () => {
    download.mockResolvedValue(ready(new Uint8Array([1, 2, 3]), "application/octet-stream"));
    render(<BlobDialog {...baseProps} />);

    await waitFor(() =>
      expect(download).toHaveBeenCalledWith("app.db", "files", "data", { id: 1 }, expect.anything()),
    );
  });

  it("renders a raster image inline when the type is safe", async () => {
    download.mockResolvedValue(ready(PNG, "image/png"));
    render(<BlobDialog {...baseProps} />);

    const img = await screen.findByAltText("Blob content of data");
    expect(img).toHaveAttribute("src", "blob:fake");
    expect(screen.queryByTestId("blob-hex")).toBeNull();
  });

  it("hex-dumps anything that is not an inline-safe image", async () => {
    download.mockResolvedValue(ready(new Uint8Array([0x50, 0x4b, 3, 4]), "application/zip"));
    render(<BlobDialog {...baseProps} />);

    const hex = await screen.findByTestId("blob-hex");
    expect(hex.textContent).toContain("50 4b 03 04");
    expect(hex.textContent).toContain("PK");
    expect(screen.queryByAltText("Blob content of data")).toBeNull();
  });

  // An SVG is a script-carrying document. Rendering it in an <img> would be a
  // same-origin execution sink, so it must fall through to the hex dump.
  it("refuses to render an SVG inline even though the server can send it", async () => {
    download.mockResolvedValue(ready(new Uint8Array([0x3c, 0x73, 0x76, 0x67]), "image/svg+xml"));
    render(<BlobDialog {...baseProps} />);

    expect(await screen.findByTestId("blob-hex")).toBeTruthy();
    expect(screen.queryByAltText("Blob content of data")).toBeNull();
    expect(URL.createObjectURL).not.toHaveBeenCalled();
  });

  it("says the cell is NULL instead of offering an empty file", async () => {
    download.mockResolvedValue({ data: null, mediaType: "", byteLength: 0, isNull: true });
    render(<BlobDialog {...baseProps} />);

    expect(await screen.findByTestId("blob-null")).toBeTruthy();
    // Download is disabled: there is nothing to save.
    expect(screen.getByTestId("blob-download")).toBeDisabled();
  });

  it("warns that only a preview is loaded when the cell was truncated", async () => {
    render(<BlobDialog {...baseProps} cell={{ bytes: 5 * 1024 * 1024, truncated: true }} />);
    expect(screen.getByTestId("blob-size").textContent).toContain("download");
  });

  it("surfaces a fetch failure", async () => {
    download.mockRejectedValue(new ApiError("cell is not a BLOB", 400));
    render(<BlobDialog {...baseProps} />);

    expect(await screen.findByTestId("blob-error")).toHaveTextContent("cell is not a BLOB");
  });

  it("downloads the fetched bytes under a name derived from the column", async () => {
    download.mockResolvedValue(ready(PNG, "image/png"));
    const clicks: string[] = [];
    const origClick = HTMLAnchorElement.prototype.click;
    HTMLAnchorElement.prototype.click = function click(this: HTMLAnchorElement) {
      clicks.push(this.download);
    };
    try {
      render(<BlobDialog {...baseProps} />);
      fireEvent.click(await screen.findByTestId("blob-download"));
    } finally {
      HTMLAnchorElement.prototype.click = origClick;
    }
    expect(clicks).toEqual(["data.png"]);
  });

  it("uploads a chosen file as raw bytes and refetches", async () => {
    download.mockResolvedValue(ready(new Uint8Array([1]), "application/octet-stream"));
    upload.mockResolvedValue({ rows_affected: 1, elapsed_ms: 1 });
    const onReplaced = vi.fn();
    render(<BlobDialog {...baseProps} onReplaced={onReplaced} />);
    await screen.findByTestId("blob-download");

    const before = download.mock.calls.length;
    const file = new File([new Uint8Array([9, 8, 7])], "new.png", { type: "image/png" });
    fireEvent.change(screen.getByLabelText("Replace data"), { target: { files: [file] } });

    await waitFor(() => expect(upload).toHaveBeenCalledTimes(1));
    const call = upload.mock.calls[0];
    expect(call[0]).toBe("app.db");
    expect(Array.from(call[4])).toEqual([9, 8, 7]);
    // The view refreshes to the STORED value, so the counter goes up again.
    await waitFor(() => expect(download.mock.calls.length).toBeGreaterThan(before));
    expect(onReplaced).toHaveBeenCalled();
  });

  it("keeps the upload error visible when the upload is refused", async () => {
    download.mockResolvedValue(ready(new Uint8Array([1]), "application/octet-stream"));
    upload.mockRejectedValue(new ApiError("Blob is larger than the 64 MB limit.", 413));
    render(<BlobDialog {...baseProps} />);
    await screen.findByTestId("blob-download");

    const file = new File([new Uint8Array([1])], "big.bin");
    fireEvent.change(screen.getByLabelText("Replace data"), { target: { files: [file] } });

    expect(await screen.findByTestId("blob-upload-error")).toHaveTextContent("64 MB");
  });

  it("hides the replace control for a read-only table", async () => {
    download.mockResolvedValue(ready(new Uint8Array([1]), "application/octet-stream"));
    render(<BlobDialog {...baseProps} editable={false} />);
    await screen.findByTestId("blob-download");

    expect(screen.queryByTestId("blob-upload")).toBeNull();
    expect(screen.queryByLabelText("Replace data")).toBeNull();
  });
});