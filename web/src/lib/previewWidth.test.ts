import { describe, expect, it, beforeEach, vi } from "vitest";
import {
  PREVIEW_WIDTH_STORAGE_KEY,
  __resetPreviewWidthForTests,
  defaultPaperForLocale,
  getPreviewWidthPreset,
  previewColumnClass,
  previewWidthMenuLabel,
  previewWidthShortLabel,
  resolvePreviewWidth,
  setPreviewWidthPreset,
} from "./previewWidth";

beforeEach(() => {
  window.localStorage.clear();
  __resetPreviewWidthForTests();
});

describe("defaultPaperForLocale", () => {
  it("gives US customary-paper locales Letter and the metric world A4", () => {
    expect(defaultPaperForLocale("en-US")).toBe("letter");
    expect(defaultPaperForLocale("en-US-x-twain")).toBe("letter");
    expect(defaultPaperForLocale("en_LR")).toBe("letter");
    // Letter markets that a metric-vs-imperial list would miss.
    expect(defaultPaperForLocale("en-CA")).toBe("letter");
    expect(defaultPaperForLocale("es-MX")).toBe("letter");
    expect(defaultPaperForLocale("fil-PH")).toBe("letter");
    expect(defaultPaperForLocale("es-CL")).toBe("letter");
    expect(defaultPaperForLocale("en-GB")).toBe("a4");
    expect(defaultPaperForLocale("ja-JP")).toBe("a4");
    expect(defaultPaperForLocale("ms-MY")).toBe("a4");
    // Myanmar is an A4 market; it was wrongly in the Letter set.
    expect(defaultPaperForLocale("en-MM")).toBe("a4");
  });

  it("fails toward A4 for an empty or bare tag", () => {
    expect(defaultPaperForLocale("")).toBe("a4");
    expect(defaultPaperForLocale("en")).toBe("a4");
  });
});

describe("resolvePreviewWidth", () => {
  it("follows the locale in auto mode", () => {
    expect(resolvePreviewWidth("auto", { locale: "en-US" })).toBe("letter");
    expect(resolvePreviewWidth("auto", { locale: "de-DE" })).toBe("a4");
  });

  it("honours an explicit preset regardless of locale", () => {
    expect(resolvePreviewWidth("letter", { locale: "de-DE" })).toBe("letter");
    expect(resolvePreviewWidth("a4", { locale: "en-US" })).toBe("a4");
    expect(resolvePreviewWidth("full", { locale: "en-US" })).toBe("full");
  });

  // The "code stays full width" guarantee is the KIND GATE, not a resolver
  // option: PreviewSurface renders MarkdownViewer only for kind === "markdown",
  // and the header control is gated the same way. This pins the resolver has no
  // hidden prose branch that the docs could mistake for the mechanism.
  it("has no prose branch: an explicit preset always wins", () => {
    expect(resolvePreviewWidth("letter", { locale: "de-DE" })).toBe("letter");
    expect(resolvePreviewWidth("auto", { locale: "en-US" })).toBe("letter");
  });
});

describe("previewColumnClass", () => {
  it("centres a paper column with a fluid horizontal margin", () => {
    const fluid = "px-[max(0.75rem,min(2rem,4%))]";
    const letter = previewColumnClass("letter");
    expect(letter).toContain("mx-auto");
    expect(letter).toContain("max-w-[816px]");
    expect(letter).toContain(fluid);
    // A fixed px-8 left almost no measure in the draggable Files-tab split.
    expect(letter).not.toContain("px-8");

    const a4 = previewColumnClass("a4");
    expect(a4).toContain("mx-auto");
    expect(a4).toContain("max-w-[794px]");
    expect(a4).toContain(fluid);
  });

  it("keeps the edge-to-edge behaviour for full", () => {
    const full = previewColumnClass("full");
    expect(full).toContain("max-w-none");
    // A margin still applies so prose is never flush against the pane edge.
    expect(full).toContain("px-3");
  });
});

describe("labels", () => {
  it("names Folio/F4 inside the A4 entry instead of shipping a duplicate", () => {
    // F4 is 210mm wide — the same column as A4 — so it cannot be a separate
    // preset; the menu label is where the F4 request stays visible.
    expect(previewWidthMenuLabel("a4")).toContain("A4");
    expect(previewWidthMenuLabel("a4")).toContain("F4");
    expect(previewWidthShortLabel("a4")).toBe("A4");
    expect(previewWidthShortLabel("letter")).toBe("Letter");
    expect(previewWidthShortLabel("full")).toBe("Full");
  });

  it("resolves the Auto entry from ITS OWN locale, not the selected preset", () => {
    // The regression this pins: the Auto row used to be labelled with the
    // CURRENT preset's resolution, so with preset=a4 on en-US it read
    // "Auto — A4" while Auto rendered Letter.
    expect(previewWidthMenuLabel("auto", "en-US")).toBe("Auto — Letter");
    expect(previewWidthMenuLabel("auto", "ja-JP")).toBe("Auto — A4");
    expect(previewWidthMenuLabel("letter", "ja-JP")).toBe("Letter (8.5 × 11 in)");
  });
});

describe("persistence", () => {
  it("defaults to auto and stores an explicit choice", () => {
    expect(getPreviewWidthPreset()).toBe("auto");

    setPreviewWidthPreset("letter");
    expect(getPreviewWidthPreset()).toBe("letter");
    expect(window.localStorage.getItem(PREVIEW_WIDTH_STORAGE_KEY)).toBe("letter");
  });

  it("treats a corrupt stored value as auto instead of rendering nonsense", () => {
    window.localStorage.setItem(PREVIEW_WIDTH_STORAGE_KEY, "tabloid");
    expect(getPreviewWidthPreset()).toBe("auto");
  });

  it("moves when another window writes the preference", () => {
    expect(getPreviewWidthPreset()).toBe("auto");
    window.dispatchEvent(
      new StorageEvent("storage", { key: PREVIEW_WIDTH_STORAGE_KEY, newValue: "letter" }),
    );
    expect(getPreviewWidthPreset()).toBe("letter");
  });

  it("ignores a storage event for an unrelated key", () => {
    setPreviewWidthPreset("full");
    window.dispatchEvent(new StorageEvent("storage", { key: "something.else", newValue: "letter" }));
    expect(getPreviewWidthPreset()).toBe("full");
  });

  it("keeps the in-session value when localStorage throws", () => {
    const spy = vi.spyOn(window.localStorage, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });
    setPreviewWidthPreset("letter");
    expect(getPreviewWidthPreset()).toBe("letter");
    spy.mockRestore();
  });
});
