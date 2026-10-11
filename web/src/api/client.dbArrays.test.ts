import { describe, it, expect, afterEach, vi } from "vitest";
import { api } from "./client";

/**
 * The /api/db/* wire contract says every list field is an ARRAY, never null —
 * the SQLite preview dereferences them unguarded (`schema.foreign_keys.length`,
 * `result.rows.map`), so a null is a hard render crash, not a degraded view.
 *
 * The server now honours that contract (internal/dbbrowse wire_contract_test.go).
 * This suite pins the CLIENT half of it, which exists for a different reason:
 * a remote project's `/api/db/*` requests are proxied to that machine's own
 * `ocode serve --remote` binary, so a freshly built web bundle can talk to a
 * host still running an older server that emits `"foreign_keys": null`. The fix
 * belongs at the fetch boundary, once, so no call site can forget it.
 */

function stubFetch(body: unknown): void {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ),
  );
}

const SCHEMA_WITH_NULLS = {
  name: "users",
  type: "table",
  columns: [{ name: "id", decl_type: "INTEGER", not_null: true, pk: 1, generated: false }],
  indexes: null,
  foreign_keys: null,
  ddl: "CREATE TABLE users(id INTEGER PRIMARY KEY)",
  rowid: true,
};

const RESULT_WITH_NULLS = {
  columns: [{ name: "id", decl_type: "INTEGER" }],
  rows: null,
  row_count: 0,
  truncated: false,
  elapsed_ms: 1,
};

describe("db client array normalization", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("turns a null foreign_keys into an empty array", async () => {
    stubFetch({ schema: SCHEMA_WITH_NULLS, result: RESULT_WITH_NULLS });
    const res = await api.dbTable("/tmp/a.db", "users");
    // The exact expression the crash reported.
    expect(res.schema.foreign_keys.length).toBe(0);
    expect(res.schema.foreign_keys.map((fk) => fk.from)).toEqual([]);
  });

  it("turns a null indexes list (and a null index column list) into empty arrays", async () => {
    stubFetch({
      schema: { ...SCHEMA_WITH_NULLS, indexes: null },
      result: RESULT_WITH_NULLS,
    });
    const res = await api.dbTable("/tmp/a.db", "users");
    expect(res.schema.indexes.length).toBe(0);

    stubFetch({
      schema: {
        ...SCHEMA_WITH_NULLS,
        indexes: [{ name: "idx_a", unique: false, columns: null, origin: "c" }],
      },
      result: RESULT_WITH_NULLS,
    });
    const withIndex = await api.dbTable("/tmp/a.db", "users");
    expect(withIndex.schema.indexes[0].columns).toEqual([]);
  });

  it("turns a null rows list into an empty array", async () => {
    stubFetch(RESULT_WITH_NULLS);
    const res = await api.dbQuery("/tmp/a.db", "SELECT * FROM users");
    expect(res.rows.map((row) => row.length)).toEqual([]);
    expect(res.rows.length).toBe(0);
  });

  it("turns a null tables list into an empty array", async () => {
    stubFetch({ is_sqlite: true, path: "/tmp/a.db", tables: null });
    const res = await api.dbInfo("/tmp/a.db");
    expect(res.tables).toEqual([]);
  });

  it("keeps populated arrays and their contents untouched", async () => {
    stubFetch({
      schema: {
        ...SCHEMA_WITH_NULLS,
        indexes: [{ name: "idx_a", unique: true, columns: ["a"], origin: "c" }],
        foreign_keys: [{ from: "org_id", table: "orgs", to: "id" }],
      },
      result: { columns: RESULT_WITH_NULLS.columns, rows: [[1]], row_count: 1 },
    });
    const res = await api.dbTable("/tmp/a.db", "users");
    expect(res.schema.foreign_keys).toEqual([{ from: "org_id", table: "orgs", to: "id" }]);
    expect(res.schema.indexes[0].columns).toEqual(["a"]);
    expect(res.result.rows).toEqual([[1]]);
  });
});