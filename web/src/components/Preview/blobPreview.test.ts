import { describe, expect, it } from "vitest";
import {
  blobFilename,
  describeBlobCell,
  downloadBytes,
  fileToBytes,
  formatBytes,
  formatHexDump,
  isInlineSafeMediaType,
  mediaTypeFromHeader,
} from "./blobPreview";

describe("isInlineSafeMediaType", () => {
  it("allows raster images", () => {
    for (const t of ["image/png", "image/jpeg", "image/gif", "image/webp"]) {
      expect(isInlineSafeMediaType(t)).toBe(true);
    }
  });

  it("refuses anything that can execute script", () => {
    for (const t of ["image/svg+xml", "text/html", "application/xhtml+xml", "text/xml"]) {
      expect(isInlineSafeMediaType(t)).toBe(false);
    }
  });

  it("refuses types that merely display", () => {
    for (const t of ["application/pdf", "application/octet-stream", "application/zip"]) {
      expect(isInlineSafeMediaType(t)).toBe(false);
    }
  });

  it("refuses an empty or unknown type", () => {
    expect(isInlineSafeMediaType("")).toBe(false);
    expect(isInlineSafeMediaType("image/avif")).toBe(false);
  });
});

describe("mediaTypeFromHeader", () => {
  it("strips parameters", () => {
    expect(mediaTypeFromHeader("image/png; charset=binary")).toBe("image/png");
  });

  it("lowercases and trims", () => {
    expect(mediaTypeFromHeader("  IMAGE/PNG ")).toBe("image/png");
  });

  it("returns empty for null", () => {
    expect(mediaTypeFromHeader(null)).toBe("");
  });
});

describe("formatBytes", () => {
  it("formats a byte count", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(999)).toBe("999 B");
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(1024 * 1024 * 3)).toBe("3.0 MB");
  });
});

describe("formatHexDump", () => {
  it("renders offset, hex and ascii columns", () => {
    const out = formatHexDump(new Uint8Array([0x68, 0x69]), 16);
    expect(out).toContain("00000000");
    expect(out).toContain("68 69");
    expect(out).toContain("hi");
  });

  it("pads a short final line so the ascii column stays aligned", () => {
    const out = formatHexDump(new Uint8Array([0x41]), 16);
    const lines = out.split("\n");
    expect(lines).toHaveLength(1);
    // Layout is fixed-width: 8 offset + 2 + 47 hex slots + 2 + ascii, so the
    // ASCII column starts at index 59 whether or not the line is full. A
    // full-width line must put its ascii at the same column.
    expect(lines[0].slice(0, 8)).toBe("00000000");
    expect(lines[0].slice(59)).toBe("A");
    // A full-width line puts its ascii at the SAME column; only the ascii
    // column's own width varies with content.
    const full = formatHexDump(new Uint8Array(16).fill(0x42), 16).split("\n")[0];
    expect(full.slice(59)).toBe("BBBBBBBBBBBBBBBB");
  });

  it("renders non-printable bytes as a dot", () => {
    const out = formatHexDump(new Uint8Array([0x00, 0x1f, 0x7f]), 16);
    expect(out.endsWith("...")).toBe(true);
  });

  it("stops at maxBytes and says so", () => {
    const data = new Uint8Array(64).fill(0x41);
    const out = formatHexDump(data, 16);
    const lines = out.split("\n").filter((l) => l.includes("0000"));
    expect(lines).toHaveLength(1);
    expect(out).toContain("64 bytes");
  });

  it("reports an empty value without inventing a line", () => {
    expect(formatHexDump(new Uint8Array([]), 16)).toBe("");
  });
});

describe("blobFilename", () => {
  it("names the file after the column and its sniffed type", () => {
    expect(blobFilename("data", "image/png")).toBe("data.png");
    expect(blobFilename("data", "application/pdf")).toBe("data.pdf");
    expect(blobFilename("data", "application/octet-stream")).toBe("data.bin");
  });

  it("keeps a hostile column name from suggesting a path", () => {
    expect(blobFilename("../../etc/passwd", "image/png")).toBe(".._.._etc_passwd.png");
    expect(blobFilename("a/b", "image/png")).not.toContain("/");
  });
});

describe("describeBlobCell", () => {
  it("warns when only a preview was loaded", () => {
    expect(describeBlobCell({ bytes: 5 * 1024 * 1024, truncated: true })).toContain("download");
  });

  it("just states the size for a fully loaded value", () => {
    expect(describeBlobCell({ bytes: 12, truncated: false })).toBe("12 B");
  });
});

describe("downloadBytes", () => {
  it("offers the bytes as a named file and defers the revoke", () => {
    const urls: string[] = [];
    const origCreate = URL.createObjectURL;
    const origRevoke = URL.revokeObjectURL;
    const revoked: string[] = [];
    URL.createObjectURL = () => {
      urls.push("blob");
      return "blob:z";
    };
    URL.revokeObjectURL = (u: string) => revoked.push(u);
    const clicks: string[] = [];
    const origClick = HTMLAnchorElement.prototype.click;
    HTMLAnchorElement.prototype.click = function click(this: HTMLAnchorElement) {
      clicks.push(this.download);
    };
    try {
      downloadBytes("data.png", new Uint8Array([1, 2]), "image/png");
    } finally {
      URL.createObjectURL = origCreate;
      URL.revokeObjectURL = origRevoke;
      HTMLAnchorElement.prototype.click = origClick;
    }
    expect(urls).toEqual(["blob"]);
    expect(clicks).toEqual(["data.png"]);
    // Not revoked synchronously — Safari would cancel the download.
    expect(revoked).toEqual([]);
  });
});

describe("fileToBytes", () => {
  it("reads a file into bytes", async () => {
    const f = new File([new Uint8Array([9, 8, 7])], "x.bin");
    expect(Array.from(await fileToBytes(f))).toEqual([9, 8, 7]);
  });

  it("reads an empty file as zero bytes rather than failing", async () => {
    const f = new File([], "empty.bin");
    expect((await fileToBytes(f)).length).toBe(0);
  });
});
