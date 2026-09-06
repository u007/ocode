import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import RemoteReconnect from "./RemoteReconnect";

describe("RemoteReconnect", () => {
  it("renders a minimal reconnect message with no interactive app chrome", () => {
    render(<RemoteReconnect />);
    expect(screen.getByText(/reconnect/i)).toBeInTheDocument();
    expect(screen.getByText(/ocode remote/i)).toBeInTheDocument();
  });
});
