import { describe, expect, it } from "vitest";
import { fileNameOf } from "./officePort";

describe("fileNameOf", () => {
  it("returns the last segment of a Windows or POSIX path", () => {
    expect(fileNameOf("/Users/jane/Book1.xlsx")).toBe("Book1.xlsx");
    expect(fileNameOf("C:\\Users\\jane\\Book1.xlsx")).toBe("Book1.xlsx");
  });

  it("decodes percent-encoding the host left in the path", () => {
    expect(fileNameOf("/tmp/My%20Budget.xlsx")).toBe("My Budget.xlsx");
  });

  it("does not throw on a filename with a bare percent sign", () => {
    // decodeURIComponent("Report 100%.xlsx") throws URIError; a thrown read would
    // fail the whole workbookInfo call, so the raw segment must come back.
    expect(fileNameOf("/tmp/Report 100%.xlsx")).toBe("Report 100%.xlsx");
    expect(fileNameOf("/tmp/50%off%.xlsx")).toBe("50%off%.xlsx");
  });

  it("returns the input when there is no separator", () => {
    expect(fileNameOf("Book1.xlsx")).toBe("Book1.xlsx");
  });
});
