import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { RowEditorDialog, cellToInput, columnHasDefault, inputToCell } from "./SQLiteDialogs";
import type { DBCell, DBTableSchema } from "../../api/client";

describe("SQLiteDialogs value codec", () => {
  it("renders cells as editable text", () => {
    expect(cellToInput(null)).toBe("");
    expect(cellToInput(5)).toBe("5");
    expect(cellToInput("x")).toBe("x");
    expect(cellToInput({ $blob: true, bytes: 3, preview: "aabb" })).toBe("");
  });

  it("coerces input by declared type", () => {
    expect(inputToCell("", "TEXT")).toBeNull();
    expect(inputToCell("42", "INTEGER")).toBe(42);
    expect(inputToCell("42.9", "INTEGER")).toBe(42);
    expect(inputToCell("4.5", "REAL")).toBe(4.5);
    expect(inputToCell("hello", "TEXT")).toBe("hello");
    // A non-numeric value in a numeric column is kept as text rather than
    // silently becoming NaN.
    expect(inputToCell("abc", "INTEGER")).toBe("abc");
  });
});

// The Postgres Add-row path: an empty field is NULL unless the column is NOT
// NULL with a default, in which case it is omitted so the database fills it.
const ROLE_SCHEMA: DBTableSchema = {
  name: "aimsai_role",
  type: "table",
  columns: [
    {
      name: "id",
      decl_type: "character varying(255)",
      not_null: true,
      pk: 1,
      generated: false,
    },
    {
      name: "name",
      decl_type: "character varying(255)",
      not_null: true,
      pk: 0,
      generated: false,
    },
    {
      name: "permissions",
      decl_type: "text[]",
      not_null: true,
      has_default: true,
      default: "'{}'::text[]",
      pk: 0,
      generated: false,
    },
    {
      name: "createdAt",
      decl_type: "timestamp with time zone",
      not_null: true,
      has_default: true,
      default: "CURRENT_TIMESTAMP",
      pk: 0,
      generated: false,
    },
    {
      name: "description",
      decl_type: "text",
      not_null: false,
      pk: 0,
      generated: false,
    },
  ],
  indexes: [],
  foreign_keys: [],
  ddl: "",
  rowid: false,
};

function renderRowEditor(
  initial: Record<string, DBCell> | null = null,
  schema: DBTableSchema = ROLE_SCHEMA,
) {
  const onSubmit = vi.fn();
  render(
    <RowEditorDialog
      open
      schema={schema}
      initial={initial}
      busy={false}
      error={null}
      parseInput={(text) => (text === "" ? null : text)}
      onCancel={() => {}}
      onSubmit={onSubmit}
    />,
  );
  return onSubmit;
}

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

/** The id column marked as proven UUID-shaped by the caller's sampled rows. */
function withUUIDHint(schema: DBTableSchema = ROLE_SCHEMA): DBTableSchema {
  return {
    ...schema,
    columns: schema.columns.map((c) => (c.name === "id" ? { ...c, uuid_like: true } : c)),
  };
}

describe("RowEditorDialog insert semantics", () => {
  it("pre-fills a UUID for a hinted id and sends it", () => {
    const onSubmit = renderRowEditor(null, withUUIDHint());
    const id = screen.getByLabelText("id") as HTMLInputElement;
    expect(id.value).toMatch(UUID_V4);

    fireEvent.change(screen.getByLabelText("name"), { target: { value: "admin" } });
    fireEvent.click(screen.getByTestId("sqlite-row-save"));
    expect(onSubmit).toHaveBeenCalledWith({ id: id.value, name: "admin", description: null });
  });

  it("regenerates the id from the Generate button", () => {
    renderRowEditor(null, withUUIDHint());
    const first = (screen.getByLabelText("id") as HTMLInputElement).value;
    fireEvent.click(screen.getByRole("button", { name: "Generate UUID for id" }));
    const next = (screen.getByLabelText("id") as HTMLInputElement).value;
    expect(next).toMatch(UUID_V4);
    expect(next).not.toBe(first);
  });

  it("auto-fills a uuid-typed column without a caller hint", () => {
    const schema: DBTableSchema = {
      ...ROLE_SCHEMA,
      columns: ROLE_SCHEMA.columns.map((c) =>
        c.name === "id" ? { ...c, decl_type: "uuid" } : c,
      ),
    };
    renderRowEditor(null, schema);
    expect((screen.getByLabelText("id") as HTMLInputElement).value).toMatch(UUID_V4);
  });

  it("treats an identity/default flag as a default even without an expression", () => {
    expect(
      columnHasDefault({ name: "id", decl_type: "bigint", not_null: true, has_default: true, pk: 1, generated: false }),
    ).toBe(true);
    expect(
      columnHasDefault({ name: "id", decl_type: "text", not_null: true, pk: 1, generated: false }),
    ).toBe(false);
  });

  it("omits empty NOT NULL defaulted columns and keeps nullable ones NULL", () => {
    const onSubmit = renderRowEditor();
    fireEvent.change(screen.getByLabelText("id"), { target: { value: "r1" } });
    fireEvent.change(screen.getByLabelText("name"), { target: { value: "admin" } });
    fireEvent.click(screen.getByTestId("sqlite-row-save"));

    expect(onSubmit).toHaveBeenCalledWith({ id: "r1", name: "admin", description: null });
  });

  it("shows the not-null and default badges", () => {
    renderRowEditor();
    expect(screen.getAllByText("not null")).toHaveLength(4);
    expect(screen.getByText("default '{}'::text[]")).toBeTruthy();
    expect(screen.getByText("default CURRENT_TIMESTAMP")).toBeTruthy();
  });

  it("does not invent an id for a slug-shaped varchar key", () => {
    renderRowEditor();
    expect((screen.getByLabelText("id") as HTMLInputElement).value).toBe("");
  });

  it("blocks an insert that leaves a NOT NULL column without a default empty", () => {
    const onSubmit = renderRowEditor();
    fireEvent.click(screen.getByTestId("sqlite-row-save"));
    expect(screen.getByTestId("db-required-error").textContent).toContain("id, name");
    expect(onSubmit).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("id"), { target: { value: "r1" } });
    fireEvent.change(screen.getByLabelText("name"), { target: { value: "admin" } });
    expect(screen.queryByTestId("db-required-error")).toBeNull();
    fireEvent.click(screen.getByTestId("sqlite-row-save"));
    expect(onSubmit).toHaveBeenCalledWith({ id: "r1", name: "admin", description: null });
  });

  it("does not require a SQLite INTEGER PRIMARY KEY, because NULL assigns rowid", () => {
    const onSubmit = vi.fn();
    const schema: DBTableSchema = {
      name: "users",
      type: "table",
      columns: [
        { name: "id", decl_type: "INTEGER", not_null: true, pk: 1, generated: false },
        { name: "name", decl_type: "TEXT", not_null: false, pk: 0, generated: false },
      ],
      indexes: [],
      foreign_keys: [],
      ddl: "",
      rowid: true,
    };
    render(
      <RowEditorDialog
        open
        schema={schema}
        initial={null}
        busy={false}
        error={null}
        parseInput={(text) => (text === "" ? null : text)}
        onCancel={() => {}}
        onSubmit={onSubmit}
      />,
    );
    fireEvent.change(screen.getByLabelText("name"), { target: { value: "carol" } });
    fireEvent.click(screen.getByTestId("sqlite-row-save"));
    expect(onSubmit).toHaveBeenCalledWith({ id: null, name: "carol" });
  });

  it("still sends every field on an edit, including an explicitly cleared one", () => {
    const onSubmit = renderRowEditor({
      id: "r1",
      name: "admin",
      permissions: "{}",
      createdAt: "2026-01-01 00:00:00+00",
      description: null,
    });
    fireEvent.click(screen.getByTestId("sqlite-row-save"));
    expect(onSubmit).toHaveBeenCalledWith({
      id: "r1",
      name: "admin",
      permissions: "{}",
      createdAt: "2026-01-01 00:00:00+00",
      description: null,
    });
  });
});
