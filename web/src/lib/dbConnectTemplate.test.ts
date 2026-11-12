import { describe, expect, it } from "vitest";
import { isEnvTemplateURL } from "./dbConnectTemplate";

// Mirrors dbconnect.IsTemplate (internal/dbconnect/expand.go): only a BRACED
// reference with a shell-valid name makes a URL a template.
describe("isEnvTemplateURL", () => {
  it.each([
    "${DATABASE_URL}",
    "postgres://app:${DB_PASS}@db:5432/app",
    "postgres://${U}:${P}@h/db",
    "${_PRIVATE_URL1}",
  ])("treats %s as a template", (url) => {
    expect(isEnvTemplateURL(url)).toBe(true);
  });

  it.each([
    "postgres://app:pw@db:5432/app",
    "$DATABASE_URL",
    "postgres://app:pa$HOME@db/app",
    "postgres://app:p$ssword@db/app",
    "${1BAD}",
    "${}",
    "${A-B}",
    "${unterminated",
  ])("treats %s as a literal", (url) => {
    expect(isEnvTemplateURL(url)).toBe(false);
  });
});
