import { describe, expect, it } from "vitest";
import { validate, withDefaults } from "./schema";

const s = { type: "object", properties: { a: { type: "string" }, n: { type: "integer", minimum: 1, maximum: 5, default: 2 }, e: { enum: ["x", "y"] }, l: { type: "array", items: { type: "number" } } }, required: ["a"], additionalProperties: false } as const;

describe("validate", () => {
  it("accepts valid input", () => expect(validate(s as never, { a: "q", n: 3, e: "x", l: [1, 2] })).toEqual([]));
  it("reports every problem with a path", () => {
    const e = validate(s as never, { n: 9, e: "z", l: [1, "no"], extra: 1 });
    expect(e.join("|")).toMatch(/a is required/);
    expect(e.join("|")).toMatch(/n must be <= 5/);
    expect(e.join("|")).toMatch(/e must be one of/);
    expect(e.join("|")).toMatch(/l\[1\] must be number/);
    expect(e.join("|")).toMatch(/extra is not an allowed property/);
  });
  it("integer rejects fractions; booleans are not numbers", () => {
    expect(validate(s as never, { a: "x", n: 1.5 })).not.toEqual([]);
    expect(validate({ type: "number" }, true)).not.toEqual([]);
  });
  it("applies defaults only to absent properties", () => {
    expect(withDefaults(s as never, { a: "x" })).toEqual({ a: "x", n: 2 });
    expect(withDefaults(s as never, { a: "x", n: 4 })).toEqual({ a: "x", n: 4 });
  });
});
