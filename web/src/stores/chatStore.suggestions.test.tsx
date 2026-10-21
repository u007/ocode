import { describe, expect, it } from "vitest";
import { chatReducer, getSessionSlice, initialState, latestSuggestions } from "./chatStore";
import type { Message } from "../api/types";

function initial() {
  return initialState;
}

describe("suggested responses in the chat store", () => {
  it("SET_SUGGESTIONS stores the proposed options on the targeted session", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "SET_SUGGESTIONS",
      sessionId: "a",
      suggestions: ["Run the tests", "Open a PR"],
    });
    expect(getSessionSlice(state, "a").suggestions).toEqual([
      "Run the tests",
      "Open a PR",
    ]);
  });

  it("SET_SUGGESTIONS replaces the previous set rather than appending", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "SET_SUGGESTIONS",
      sessionId: "a",
      suggestions: ["first"],
    });
    state = chatReducer(state, {
      type: "SET_SUGGESTIONS",
      sessionId: "a",
      suggestions: ["second"],
    });
    expect(getSessionSlice(state, "a").suggestions).toEqual(["second"]);
  });

  it("SET_SUGGESTIONS with an empty list clears the row", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "SET_SUGGESTIONS",
      sessionId: "a",
      suggestions: ["first"],
    });
    state = chatReducer(state, {
      type: "SET_SUGGESTIONS",
      sessionId: "a",
      suggestions: [],
    });
    expect(getSessionSlice(state, "a").suggestions).toEqual([]);
  });

  it("does not leak the options into another session", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "SET_SUGGESTIONS",
      sessionId: "a",
      suggestions: ["only for a"],
    });
    expect(getSessionSlice(state, "b").suggestions).toEqual([]);
  });

  it("a new session starts with no suggestions", () => {
    expect(getSessionSlice(initial(), "fresh").suggestions).toEqual([]);
  });
});

describe("latestSuggestions", () => {
  const withSuggestions = (suggestions: string[]): Message => ({
    role: "assistant",
    content: "All done.",
    suggestions,
  });

  it("returns the options of the last assistant reply that carried any", () => {
    const messages: Message[] = [
      withSuggestions(["old one"]),
      { role: "user", content: "more" },
      withSuggestions(["new one"]),
    ];
    expect(latestSuggestions(messages)).toEqual(["new one"]);
  });

  it("an assistant reply with no options clears the row instead of inheriting an older one", () => {
    const messages: Message[] = [
      withSuggestions(["the only set"]),
      { role: "assistant", content: "no options here" },
    ];
    expect(latestSuggestions(messages)).toEqual([]);
  });

  it("returns nothing when no reply carried options", () => {
    expect(latestSuggestions([{ role: "assistant", content: "hi" }])).toEqual([]);
  });

  it("does not cross a user message boundary", () => {
    const messages: Message[] = [
      withSuggestions(["answered before"]),
      { role: "user", content: "a new request" },
    ];
    expect(latestSuggestions(messages)).toEqual([]);
  });

  it("an injected user-role notice does not clear the last reply's options", () => {
    const messages: Message[] = [
      withSuggestions(["still valid"]),
      { role: "user", content: "[ocode:event] out-of-band completion notice, not a user instruction" },
    ];
    expect(latestSuggestions(messages)).toEqual(["still valid"]);
  });

  it("skips tool messages between the final reply and its tool-call round", () => {
    const messages: Message[] = [
      { role: "user", content: "run it" },
      { role: "assistant", content: "" },
      { role: "tool", content: "ok" },
      withSuggestions(["Run the tests"]),
    ];
    expect(latestSuggestions(messages)).toEqual(["Run the tests"]);
  });
});

describe("suggestions belong only to the current reply", () => {
  const user = (content: string): Message => ({ role: "user", content });
  const reply = (content: string, suggestions?: string[]): Message => ({
    role: "assistant",
    content,
    suggestions,
  });

  it("a later reply with no block does not inherit an older reply's options", () => {
    const messages: Message[] = [
      user("fix the build"),
      reply("Fixed it.", ["Run tests", "Open PR"]),
      user("now the docs"),
      reply("Docs updated."),
    ];
    expect(latestSuggestions(messages)).toEqual([]);
  });

  it("SET_MESSAGES clears the row when the new reply carries no options", () => {
    let state = initial();
    state = chatReducer(state, {
      type: "SET_MESSAGES",
      sessionId: "a",
      messages: [user("fix the build"), reply("Fixed it.", ["Run tests", "Open PR"])],
    });
    expect(getSessionSlice(state, "a").suggestions).toEqual(["Run tests", "Open PR"]);

    state = chatReducer(state, {
      type: "SET_MESSAGES",
      sessionId: "a",
      messages: [
        user("fix the build"),
        reply("Fixed it.", ["Run tests", "Open PR"]),
        user("now the docs"),
        reply("Docs updated."),
      ],
    });
    expect(getSessionSlice(state, "a").suggestions).toEqual([]);
  });

  it("ADD_MESSAGE clears the row on a user send and on a reply with no options", () => {
    // The turn-boundary "messages" event appends new messages one at a time
    // when the snapshot extends the transcript, so ADD_MESSAGE must re-derive
    // the row, not leave the previous reply's options in place.
    let state = initial();
    state = chatReducer(state, {
      type: "SET_MESSAGES",
      sessionId: "a",
      messages: [user("fix the build"), reply("Fixed it.", ["Run tests", "Open PR"])],
    });
    state = chatReducer(state, { type: "ADD_MESSAGE", sessionId: "a", message: user("now the docs") });
    expect(getSessionSlice(state, "a").suggestions).toEqual([]);

    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: reply("Docs updated."),
    });
    expect(getSessionSlice(state, "a").suggestions).toEqual([]);
  });

  it("ADD_MESSAGE keeps the row the live event set when the appended copy of that reply carries the same options", () => {
    // Live order: the "suggestions" event for reply B arrives first, then the
    // turn-boundary snapshot appends B. The server persists B's options on that
    // copy, so the row must not be cleared by its arrival.
    let state = initial();
    state = chatReducer(state, {
      type: "SET_MESSAGES",
      sessionId: "a",
      messages: [user("fix the build")],
    });
    state = chatReducer(state, {
      type: "SET_SUGGESTIONS",
      sessionId: "a",
      suggestions: ["Run tests", "Open PR"],
    });
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: reply("Fixed it.", ["Run tests", "Open PR"]),
    });
    expect(getSessionSlice(state, "a").suggestions).toEqual(["Run tests", "Open PR"]);
  });

  it("ADD_MESSAGE restores the row when the appended reply carries options", () => {
    let state = initial();
    state = chatReducer(state, { type: "ADD_MESSAGE", sessionId: "a", message: user("hi") });
    state = chatReducer(state, {
      type: "ADD_MESSAGE",
      sessionId: "a",
      message: reply("Hello.", ["Say more"]),
    });
    expect(getSessionSlice(state, "a").suggestions).toEqual(["Say more"]);
  });
});

describe("suggestions survive a transcript reload", () => {
  it("MERGE_SNAPSHOT restores the options from the persisted assistant message", () => {
    // The reload path: the live "suggestions" event only fires when a reply
    // ARRIVES, so a refreshed page must recover the row from the transcript.
    const messages: Message[] = [
      { role: "user", content: "fix the build" },
      {
        role: "assistant",
        content: "Fixed it.",
        suggestions: ["Run the tests", "Open a PR"],
      },
    ];
    let state = initial();
    state = chatReducer(state, {
      type: "MERGE_SNAPSHOT",
      sessionId: "a",
      messages,
      total: messages.length,
    });
    expect(getSessionSlice(state, "a").suggestions).toEqual([
      "Run the tests",
      "Open a PR",
    ]);
  });

  it("SET_MESSAGES restores the options from the persisted assistant message", () => {
    const messages: Message[] = [
      {
        role: "assistant",
        content: "Fixed it.",
        suggestions: ["Run the tests"],
      },
    ];
    let state = initial();
    state = chatReducer(state, {
      type: "SET_MESSAGES",
      sessionId: "a",
      messages,
    });
    expect(getSessionSlice(state, "a").suggestions).toEqual(["Run the tests"]);
  });
});
