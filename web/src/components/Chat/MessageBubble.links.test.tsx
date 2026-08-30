import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import MessageBubble, { AssistantText } from "./MessageBubble";

// Regression for the github-dark "blue on blue" mess: FileLink defaults to
// text-link, a prose-safe color that is illegible on solid colored surfaces
// (github-dark maps accent/primary to #58a6ff, and text-link derives from
// exactly those colors). Those surfaces must carry a descendant
// override on the stable `file-link` class that swaps in their own paired
// foreground token. jsdom doesn't resolve Tailwind CSS, so these tests pin
// the class contract between fileLinks.tsx and the surfaces here — the same
// contract MessageBubble.test.tsx uses for theme-surface classes.

describe("FileLink surface overrides", () => {
  it("inline-code chips override the link color with the accent foreground", () => {
    const { container } = render(<AssistantText content={"edit `web/src/App.tsx` now"} />);
    const chip = container.querySelector("code");
    expect(chip).not.toBeNull();
    expect(chip!.className).toContain("bg-accent");
    expect(chip!.className).toContain("text-accent-foreground");
    expect(chip!.className).toContain("[&_.file-link]:text-accent-foreground");
    expect(chip!.className).toContain("[&_.file-link]:underline");
    const link = chip!.querySelector('[role="link"]') as HTMLElement;
    expect(link).not.toBeNull();
    expect(link.className).toContain("file-link");
  });

  it("user bubbles override the link color with the primary foreground", () => {
    const { container } = render(
      <MessageBubble message={{ role: "user", content: "see .ocode/uploads/x.png" }} />,
    );
    const bubble = container.querySelector("div.bg-primary");
    expect(bubble).not.toBeNull();
    expect(bubble!.className).toContain("text-primary-foreground");
    expect(bubble!.className).toContain("[&_.file-link]:text-primary-foreground");
    expect(bubble!.className).toContain("[&_.file-link]:underline");
    const link = bubble!.querySelector('[role="link"]') as HTMLElement;
    expect(link).not.toBeNull();
    expect(link.className).toContain("file-link");
  });

  it("assistant prose keeps the palette-derived link color (no override)", () => {
    const { container } = render(<AssistantText content={"open web/src/App.tsx for details"} />);
    const bubble = container.querySelector("div.bg-muted");
    expect(bubble).not.toBeNull();
    // The muted bubble is not a solid colored surface; the link must stay on
    // the palette-derived text-link color (AA-checked per theme in
    // useTheme.test.ts) so it keeps its link affordance in body text.
    expect(bubble!.className).not.toContain("[&_.file-link]");
    const link = bubble!.querySelector('[role="link"]') as HTMLElement;
    expect(link).not.toBeNull();
    expect(link.className).toContain("text-link");
  });

  it("table header cells override the link color with the accent foreground", () => {
    const { container } = render(
      <AssistantText content={"| File |\n| --- |\n| `web/src/App.tsx` |"} />,
    );
    const th = container.querySelector("th");
    expect(th).not.toBeNull();
    expect(th!.className).toContain("[&_.file-link]:text-accent-foreground");
  });
});
