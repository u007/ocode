import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AssistantText } from "./MessageBubble";

// The Speak button must hand the TTS layer the RENDERED text of the message,
// not the raw markdown source — otherwise the user hears "hash Title",
// "asterisk asterisk bold", backticks and link targets read aloud.
const requestSpeech = vi.hoisted(() => vi.fn());
vi.mock("../Speech/SpeechProvider", () => ({ requestSpeech }));

function speakSpoken() {
  const calls = requestSpeech.mock.calls;
  const call = calls[calls.length - 1];
  return call ? (call[0] as string) : "";
}

describe("AssistantText speak", () => {
  it("speaks rendered markdown, not the raw source", async () => {
    render(<AssistantText content={"# Title\n\nSome **bold** text with `code`."} onSpeak={requestSpeech} />);
    // SpeakButton is now async (it disables while processing), so let its
    // state updates settle inside act before asserting.
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /speak message/i }));
    });

    expect(requestSpeech).toHaveBeenCalledTimes(1);
    const spoken = speakSpoken();
    expect(spoken).toContain("Title");
    expect(spoken).toContain("Some bold text with code.");
    expect(spoken).not.toContain("**");
    expect(spoken).not.toContain("#");
    expect(spoken).not.toContain("`");
  });

  it("does not read the Speak button's own label aloud", async () => {
    render(<AssistantText content={"just text"} onSpeak={requestSpeech} />);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /speak message/i }));
    });
    expect(speakSpoken()).toBe("just text");
  });

  it("drops markdown link targets but keeps the link text", async () => {
    render(
      <AssistantText content={"Open [the device page](https://hub.mercstudio.com/device)."} onSpeak={requestSpeech} />,
    );
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /speak message/i }));
    });
    const spoken = speakSpoken();
    expect(spoken).toBe("Open the device page.");
    expect(spoken).not.toContain("https://");
  });

  it("renders no Speak button when onSpeak is omitted", () => {
    render(<AssistantText content={"just text"} />);
    expect(screen.queryByRole("button", { name: /speak message/i })).not.toBeInTheDocument();
  });
});
