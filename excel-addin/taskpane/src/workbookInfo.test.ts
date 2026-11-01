import { describe, expect, it } from "vitest";
import { FakePort } from "./fakePort";

describe("FakePort.workbookInfo", () => {
  it("reports an editable, saved, non-blank workbook by default, with the real sheet count", async () => {
    const port = new FakePort().addSheet("Summary", [["Category", "Spent"]]).addSheet("Notes", [["x"]]);
    expect(await port.workbookInfo()).toEqual({
      fileName: "Book1.xlsx", path: "/fake/Book1.xlsx", host: "Excel", platform: "Fake",
      state: "saved", blank: false, readOnly: false, protectedView: false, sheets: 2,
    });
  });

  it("setInfo changes only the fields it names", async () => {
    const port = new FakePort().addSheet("Summary", []);
    port.setInfo({ state: "new", path: "", readOnly: true });
    expect(await port.workbookInfo()).toMatchObject({ state: "new", path: "", readOnly: true, protectedView: false, sheets: 1 });
  });
});
