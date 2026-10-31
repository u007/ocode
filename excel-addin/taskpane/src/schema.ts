// Minimal JSON-Schema validator for tool arguments: exactly the keywords the contract uses
// (type, enum, minimum, maximum, items, properties, required, additionalProperties, default).
// Unknown keywords are ignored on purpose; the bridge contract test pins the vocabulary.

export type Schema = {
  type?: string;
  enum?: unknown[];
  minimum?: number;
  maximum?: number;
  items?: Schema;
  properties?: Record<string, Schema>;
  required?: string[];
  additionalProperties?: boolean;
  default?: unknown;
};

const typeOf = (v: unknown): string => (v === null ? "null" : Array.isArray(v) ? "array" : typeof v);

export function validate(schema: Schema, value: unknown, path = "arguments"): string[] {
  const errs: string[] = [];
  if (schema.enum && !schema.enum.some((e) => e === value)) errs.push(`${path} must be one of ${JSON.stringify(schema.enum)}`);
  if (schema.type) {
    const t = typeOf(value);
    const ok = schema.type === "integer" ? t === "number" && Number.isInteger(value) : schema.type === t;
    if (!ok) {
      errs.push(`${path} must be ${schema.type}, got ${t}`);
      return errs;
    }
  }
  if (typeof value === "number") {
    if (schema.minimum !== undefined && value < schema.minimum) errs.push(`${path} must be >= ${schema.minimum}`);
    if (schema.maximum !== undefined && value > schema.maximum) errs.push(`${path} must be <= ${schema.maximum}`);
  }
  if (Array.isArray(value) && schema.items) value.forEach((v, i) => errs.push(...validate(schema.items!, v, `${path}[${i}]`)));
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const obj = value as Record<string, unknown>;
    for (const r of schema.required ?? []) if (obj[r] === undefined) errs.push(`${path}.${r} is required`);
    for (const [k, v] of Object.entries(obj)) {
      const sub = schema.properties?.[k];
      if (sub) errs.push(...validate(sub, v, `${path}.${k}`));
      else if (schema.additionalProperties === false) errs.push(`${path}.${k} is not an allowed property`);
    }
  }
  return errs;
}

/** Fill defaults for absent top-level properties. */
export function withDefaults<T extends Record<string, unknown>>(schema: Schema, args: T): T {
  const out: Record<string, unknown> = { ...args };
  for (const [k, s] of Object.entries(schema.properties ?? {})) if (out[k] === undefined && s.default !== undefined) out[k] = s.default;
  return out as T;
}
