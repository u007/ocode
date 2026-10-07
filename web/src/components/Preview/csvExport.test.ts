import { describe, expect, it } from "vitest";
import { cellToCsv, downloadCsv, quoteCsvField, resultToCsv } from "./csvExport";
import type { DBBlob, DBResultSet } from "../../api/client";

/**
 * CSV export of a grid or query result.
 *
 * The rules that matter: a field containing a delimiter, quote, CR or LF is
 * quoted (RFC 4180), a quote inside a quoted field is doubled, and a NULL cell
 * is written as the empty string rather than the text "null" — a reader cannot
 * tell a NULL from the four-character string otherwise.
 */

const RESULT: DBResultSet = {
  columns: [
    { name: "id", decl_type: "INTEGER" },
    { name: "name", decl_type: "TEXT" },
  ],
  rows: [
    [1, "alice"],
    [2, null],
  ],
  row_count: 2,
  truncated: false,
  elapsed_ms: 1,
};

describe("quoteCsvField", () => {
  it("leaves a plain field alone", () => {
    expect(quoteCsvField("alice")).toBe("alice");
  });

  it("quotes a field containing a comma", () => {
    expect(quoteCsvField("a,b")).toBe('"a,b"');
  });

  it("quotes a field containing a double quote and doubles it", () => {
    expect(quoteCsvField('say "hi"')).toBe('"say ""hi"""');
  });

  it("quotes a field containing a newline", () => {
    expect(quoteCsvField("a\nb")).toBe('"a\nb"');
  });

  it("renders NULL as the empty string, not the text null", () => {
    expect(quoteCsvField(null)).toBe("");
    expect(quoteCsvField(undefined)).toBe("");
  });

  it("renders a blob as a marker rather than base64 noise", () => {
    const cell: DBBlob = { $blob: true, bytes: 3, preview: "010203", data: "AQID" };
    expect(quoteCsvField(cell)).toBe("[blob 3B]");
  });

  it("keeps a zero and a false, which are values and not absences", () => {
    expect(quoteCsvField(0)).toBe("0");
    expect(quoteCsvField(false)).toBe("false");
  });
});

describe("resultToCsv", () => {
  it("writes a header row then one row per result row", () => {
    expect(resultToCsv(RESULT)).toBe("id,name\n1,alice\n2,\n");
  });

  it("ends the file with a trailing newline so the last row is complete", () => {
    expect(resultToCsv(RESULT).endsWith("\n")).toBe(true);
  });

  it("returns just the header for an empty result", () => {
    const empty = { ...RESULT, rows: [] };
    expect(resultToCsv(empty)).toBe("id,name\n");
  });
});

describe("cellToCsv", () => {
  it("serialises a table's rows without quoting what needs none", () => {
    expect(cellToCsv(["a", "b,c"])).toBe("a,\"b,c\"");
  });
});

describe("downloadCsv", () => {
  it("creates a blob URL, clicks an anchor and revokes it later", () => {
    const created: string[] = [];
    let revoked: string[] = [];
    const created_url = "blob:fake-1";
    const origCreate = URL.createObjectURL;
    const origRevoke = URL.revokeObjectURL;
    URL.createObjectURL = () => {
      created.push("blob");
      return created_url;
    };
    URL.revokeObjectURL = (u: string) => {
      revoked.push(u);
    };
    const clicks: string[] = [];
    const origClick = HTMLAnchorElement.prototype.click;
    HTMLAnchorElement.prototype.click = function click(this: HTMLAnchorElement) {
      clicks.push(this.download);
    };
    try {
      downloadCsv("people.csv", "id,name\n");
    } finally {
      URL.createObjectURL = origCreate;
      URL.revokeObjectURL = origRevoke;
      HTMLAnchorElement.prototype.click = origClick;
    }
    expect(created).toEqual(["blob"]);
    expect(clicks).toEqual(["people.csv"]);
    // Safari navigates to about:blank when the URL is revoked synchronously, so
    // the revoke is deferred.
    expect(revoked).toEqual([]);
  });
});