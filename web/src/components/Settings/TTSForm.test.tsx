import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import TTSForm from "./TTSForm";
import { SpeechProvider } from "../Speech/SpeechProvider";

describe("TTSForm licensing and selection", () => {
  it("renders per-engine cards with license prompt", () => {
    render(
      <SpeechProvider>
        <TTSForm />
      </SpeechProvider>
    );
    expect(screen.getByText(/Speech playback/i)).toBeDefined();
    expect(screen.getByText(/Piper/i)).toBeDefined();
    expect(screen.getByText(/license/i)).toBeDefined();
    expect(screen.getByText(/Accept License/i)).toBeDefined();
  });
});
