import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * `/themes` once answered "not available" in EVERY real build: `handleThemes`
 * gated on `ctx.api.getAppearanceThemes`, while the only production
 * `CommandContext` (the inline api bag in `App.tsx`) still wired the retired
 * `getThemes`/`getTheme`/`getTUISettings` pair. Every command test builds its
 * own api bag, so the whole suite stayed green while the feature was dead.
 *
 * App.tsx is not importable here in a useful way (the bag lives inside a
 * component callback and the module drags in the whole app), so this suite
 * reads the two sources and asserts the PRODUCTION bag supplies every api
 * method `handleThemes` dereferences. A test that injects the methods itself
 * cannot catch this class of bug — that is the point of this file.
 */

const APP_SOURCE = readFileSync(join(__dirname, "..", "..", "App.tsx"), "utf8");
const COMMANDS_SOURCE = readFileSync(join(__dirname, "commands.ts"), "utf8");

/** The production `CommandContext["api"]` literal from `App.tsx`, as source. */
function productionApiBag(): string {
  const dispatchAt = APP_SOURCE.indexOf("await dispatchCommand(cmd, {");
  if (dispatchAt < 0) {
    throw new Error("production dispatchCommand call not found in App.tsx");
  }
  const apiAt = APP_SOURCE.indexOf("api: {", dispatchAt);
  if (apiAt < 0) {
    throw new Error("api bag not found in the production dispatchCommand call");
  }
  // The bag is the last member before `getMessages` in the dispatch context.
  // Failing loudly beats silently slicing the wrong region: a nav-path helper
  // that "finds" something is worse than a broken test.
  const end = APP_SOURCE.indexOf("\n      },\n      getMessages:", apiAt);
  if (end < 0) {
    throw new Error("api bag end marker not found in App.tsx");
  }
  return APP_SOURCE.slice(apiAt, end);
}

/** Every `ctx.api.<method>` a handler body dereferences, deduped + sorted. */
function handlerApiMethods(handler: string): string[] {
  const start = COMMANDS_SOURCE.indexOf(`async function ${handler}(`);
  if (start < 0) throw new Error(`handler ${handler} not found in commands.ts`);
  const nextSection = COMMANDS_SOURCE.indexOf("\n// ─── ", start + 1);
  const body = COMMANDS_SOURCE.slice(start, nextSection < 0 ? undefined : nextSection);
  const names = new Set<string>();
  for (const match of body.matchAll(/ctx\.api\.([A-Za-z0-9_]+)/g)) {
    names.add(match[1]);
  }
  return [...names].sort();
}

describe("production CommandContext wires what /themes needs", () => {
  it("supplies every api method handleThemes dereferences", () => {
    const required = handlerApiMethods("handleThemes");
    // Guard the guard: an empty list would make the loop below vacuous.
    expect(required).toContain("getAppearanceThemes");

    const bag = productionApiBag();
    for (const name of required) {
      expect(bag, `${name} is missing from the production api bag in App.tsx`).toMatch(
        new RegExp(`\\b${name}:`),
      );
    }
  });

  it("delegates each of those methods to the real api client", () => {
    const bag = productionApiBag();
    for (const name of handlerApiMethods("handleThemes")) {
      expect(bag, `${name} must delegate to api.${name}()`).toMatch(
        new RegExp(`\\b${name}:[^\\n]*api\\.${name}\\(`),
      );
    }
  });

  it("reads 'current' from the appearance store, not a second endpoint call", () => {
    const start = COMMANDS_SOURCE.indexOf("async function handleThemes(");
    const body = COMMANDS_SOURCE.slice(start, COMMANDS_SOURCE.indexOf("\n// ─── ", start + 1));
    expect(body).toContain("getColorThemeId()");
    expect(body).not.toContain("getAppearanceSettings");
  });

  it("does not wire the retired legacy theme api from App.tsx", () => {
    const bag = productionApiBag();
    for (const name of ["getThemes", "getTheme", "getTUISettings"]) {
      expect(bag, `${name} was removed from CommandContext but is still wired`).not.toMatch(
        new RegExp(`\\b${name}:`),
      );
    }
  });
});
