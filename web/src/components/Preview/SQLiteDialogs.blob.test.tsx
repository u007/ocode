import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { isBlobColumn, RowEditorDialog } from "./SQLiteDialogs";
import type { DBTableSchema } from "../../api/client";

const SCHEMA: DBTableSchema = {
  name: "files",
  type: "table",
  columns: [
    { name: "id", decl_type: "INTEGER", not_null: true, pk: 1, generated: false },
    { name: "label", decl_type: "TEXT", not_null: false, pk: 0, generated: false },
    { name: "data", decl_type: "BLOB", not_null: false, pk: 0, generated: false },
    { name: "thumb", decl_type: "MEDIUMBLOB", not_null: false, pk: 0, generated: false },
  ],
  indexes: [],
  foreign_keys: [],
  ddl: "",
  rowid: true,
};

describe("isBlobColumn", () => {
  it("matches BLOB affinity declarations", () => {
    expect(isBlobColumn("BLOB")).toBe(true);
    expect(isBlobColumn("MEDIUMBLOB")).toBe(true);
    expect(isBlobColumn("blob")).toBe(true);
  });

  it("does not match a text or numeric column", () => {
    expect(isBlobColumn("TEXT")).toBe(false);
    expect(isBlobColumn("INTEGER")).toBe(false);
    expect(isBlobColumn("")).toBe(false);
  });
});

describe("RowEditorDialog blob columns", () => {
  it("renders a file picker instead of a text field for a BLOB column", () => {
    render(
      <RowEditorDialog
        open
        schema={SCHEMA}
        initial={null}
        busy={false}
        error={null}
        onCancel={vi.fn()}
        onSubmit={vi.fn()}
      />,
    );
    expect(screen.getByLabelText("Choose file for data")).toBeTruthy();
    expect(screen.getByLabelText("Choose file for thumb")).toBeTruthy();
    // A text column keeps its text field.
    expect(screen.getByTestId("db-field-label")).toBeTruthy();
    expect(screen.queryByTestId("db-field-data")).toBeNull();
  });

  it("sends a picked file as a base64 $blob value", async () => {
    const onSubmit = vi.fn();
    render(
      <RowEditorDialog
        open
        schema={SCHEMA}
        initial={null}
        busy={false}
        error={null}
        onCancel={vi.fn()}
        onSubmit={onSubmit}
      />,
    );
    const file = new File([new Uint8Array([9, 8, 7])], "x.png", { type: "image/png" });
    fireEvent.change(screen.getByLabelText("Choose file for data"), { target: { files: [file] } });
    await waitFor(() => expect(screen.getByText(/x\.png/)).toBeTruthy());

    fireEvent.click(screen.getByTestId("sqlite-row-save"));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0].data).toEqual({ $blob: true, data: "CQgH" });
    // On a NEW row an untouched blob column is stored as NULL explicitly, so a
    // NOT NULL constraint reports against the field rather than a silent default.
    expect(onSubmit.mock.calls[0][0].thumb).toBeNull();
  });

  it("omits an untouched blob column on update so the value survives", () => {
    const onSubmit = vi.fn();
    render(
      <RowEditorDialog
        open
        schema={SCHEMA}
        initial={{ id: 1, label: "x", data: { $blob: true, bytes: 3, preview: "090807" } }}
        busy={false}
        error={null}
        onCancel={vi.fn()}
        onSubmit={onSubmit}
      />,
    );
    fireEvent.change(screen.getByTestId("db-field-label"), { target: { value: "renamed" } });
    fireEvent.click(screen.getByTestId("sqlite-row-save"));

    const values = onSubmit.mock.calls[0][0];
    expect(values.label).toBe("renamed");
    expect("data" in values).toBe(false);
  });

  it("clears a blob to NULL when the user asks for it", async () => {
    const onSubmit = vi.fn();
    render(
      <RowEditorDialog
        open
        schema={SCHEMA}
        initial={{ id: 1, label: "x", data: { $blob: true, bytes: 3, preview: "090807" } }}
        busy={false}
        error={null}
        onCancel={vi.fn()}
        onSubmit={onSubmit}
      />,
    );
    const file = new File([new Uint8Array([1])], "y.bin");
    fireEvent.change(screen.getByLabelText("Choose file for data"), { target: { files: [file] } });
    await waitFor(() => expect(screen.getByLabelText("Clear data")).toBeTruthy());
    fireEvent.click(screen.getByLabelText("Clear data"));

    fireEvent.click(screen.getByTestId("sqlite-row-save"));
    expect(onSubmit.mock.calls[0][0].data).toBeNull();
  });

  it("starts a new row's untouched blob as NULL rather than leaving it out", () => {
    const onSubmit = vi.fn();
    render(
      <RowEditorDialog
        open
        schema={SCHEMA}
        initial={null}
        busy={false}
        error={null}
        onCancel={vi.fn()}
        onSubmit={onSubmit}
      />,
    );
    fireEvent.click(screen.getByTestId("sqlite-row-save"));
    expect(onSubmit.mock.calls[0][0].data).toBeNull();
  });
});
