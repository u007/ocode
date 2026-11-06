import { describe, expect, it } from "vitest";
import { canGenerateUUID, columnHasUUIDIds, isStringColumn, isUUIDv4, uuidv4 } from "./dbIds";

const col = (over: Partial<Parameters<typeof canGenerateUUID>[0]> = {}) => ({
  declType: "character varying(255)",
  pk: 1,
  hasDefault: false,
  generated: false,
  ...over,
});

describe("uuidv4", () => {
  it("returns a version 4, RFC 4122 variant id", () => {
    for (let i = 0; i < 50; i++) {
      const id = uuidv4();
      expect(id).toMatch(
        /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
      );
      expect(isUUIDv4(id)).toBe(true);
    }
  });

  it("recognises v4 but not another version, a bad variant, or a slug", () => {
    expect(isUUIDv4("b15540d4-36b2-43ce-a4b9-00a33851ac3c")).toBe(true);
    expect(isUUIDv4("b15540d4-36b2-13ce-a4b9-00a33851ac3c")).toBe(false); // v1
    expect(isUUIDv4("b15540d4-36b2-43ce-04b9-00a33851ac3c")).toBe(false); // variant 0
    expect(isUUIDv4("admin")).toBe(false);
    expect(isUUIDv4("")).toBe(false);
  });
});

describe("UUID id columns", () => {
  it("treats a string declaration as textual and an integer one as not", () => {
    expect(isStringColumn("character varying(255)")).toBe(true);
    expect(isStringColumn("TEXT")).toBe(true);
    expect(isStringColumn("UUID")).toBe(true);
    expect(isStringColumn("INTEGER")).toBe(false);
    expect(isStringColumn("bigint")).toBe(false);
  });

  it("can generate for a textual PK without a default", () => {
    expect(canGenerateUUID(col())).toBe(true);
    expect(canGenerateUUID(col({ declType: "text" }))).toBe(true);
    expect(canGenerateUUID(col({ declType: "INTEGER" }))).toBe(false);
  });

  it("refuses a defaulted, generated, or non-KEY column", () => {
    expect(canGenerateUUID(col({ hasDefault: true }))).toBe(false);
    expect(canGenerateUUID(col({ generated: true }))).toBe(false);
    expect(canGenerateUUID(col({ pk: 0 }))).toBe(false);
  });

  it("auto-fills a uuid-typed column even with no rows", () => {
    expect(columnHasUUIDIds(col({ declType: "uuid" }), [])).toBe(true);
    expect(columnHasUUIDIds(col({ declType: "uuid", hasDefault: true }), [])).toBe(false);
  });

  it("auto-fills only when every sampled value is a UUIDv4", () => {
    const u = "b15540d4-36b2-43ce-a4b9-00a33851ac3c";
    expect(columnHasUUIDIds(col(), [u, u.toUpperCase()])).toBe(true);
    expect(columnHasUUIDIds(col(), [u, "admin"])).toBe(false);
    expect(columnHasUUIDIds(col(), ["admin"])).toBe(false);
    expect(columnHasUUIDIds(col(), [null, undefined, ""])).toBe(false);
  });
});
