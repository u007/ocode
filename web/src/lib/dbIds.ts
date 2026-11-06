// UUIDv4 generation and "this id column holds UUIDs" detection for the DB row
// editors. Free of React and of any one connector, so Postgres and SQLite share
// the same rule.

/** RFC 4122 version 4 UUID. `randomUUID` needs a secure context; plain-http
 * remote projects fall back to `getRandomValues`, and only a browser with
 * neither uses Math.random. The version and variant bits are masked in the
 * fallback paths, so the result is always a well-formed v4 id. */
export function uuidv4(): string {
  const c = globalThis.crypto;
  if (c && typeof c.randomUUID === "function") return c.randomUUID();
  const bytes = new Uint8Array(16);
  if (c && typeof c.getRandomValues === "function") c.getRandomValues(bytes);
  else for (let i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256);
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

/** True only for RFC 4122 version 4 with the 8/9/a/b variant. */
export function isUUIDv4(value: string): boolean {
  return UUID_V4.test(value);
}

/** A declared type that stores an id as text: Postgres `format_type` output or a
 * SQLite declared type. `integer`/`bigint` are deliberately excluded. */
export function isStringColumn(declType: string): boolean {
  const t = declType.trim().toLowerCase();
  return (
    t === "uuid" ||
    t === "text" ||
    t === "clob" ||
    t.startsWith("char") ||
    t.startsWith("varchar") ||
    t.startsWith("character")
  );
}

export interface UUIDCandidate {
  declType: string;
  /** 1-based position in the primary key; 0 when the column is not part of it. */
  pk: number;
  /** True when the database fills the column itself (default or identity). */
  hasDefault: boolean;
  generated: boolean;
}

/** The Generate button's rule: a textual primary-key column the database does
 * not fill itself. */
export function canGenerateUUID(c: UUIDCandidate): boolean {
  return !c.generated && c.pk > 0 && !c.hasDefault && isStringColumn(c.declType);
}

/** Auto-fill needs evidence, not just a shape: a `uuid`-typed column, or a
 * textual PK whose sampled non-null values are ALL UUIDv4. A slug-like PK
 * (`admin`, `user`) therefore never gets a UUID invented for it. */
export function columnHasUUIDIds(c: UUIDCandidate, sample: unknown[]): boolean {
  if (!canGenerateUUID(c)) return false;
  if (c.declType.trim().toLowerCase() === "uuid") return true;
  const values = sample.filter((v): v is string => typeof v === "string" && v.trim() !== "");
  return values.length > 0 && values.every(isUUIDv4);
}
