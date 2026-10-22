import { useState } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import PermissionDialog from "./PermissionDialog";
import type { PermissionDecision } from "@/api/types";
import type { PermissionDecideResult } from "./PermissionDialog";

function Scoped(props: Omit<Parameters<typeof PermissionDialog>[0], "container">) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  return (
    <div ref={setContainer}>
      <PermissionDialog {...props} container={container} />
    </div>
  );
}

function renderBashAsk(overrides: Partial<Parameters<typeof PermissionDialog>[0]> = {}) {
  const onDecide = vi.fn(
    async (
      _id: string,
      _d: PermissionDecision,
      _prefix?: string,
    ): Promise<PermissionDecideResult> => ({ ok: true }),
  );
  render(
    <Scoped
      open={true}
      tool="bash"
      command="npm run build"
      scope="bash_prefix"
      prefix="npm run build"
      prefixCandidates={["npm run build", "npm run", "npm"]}
      rule="bash.prefix.npm run build"
      requestId="call-1"
      onDecide={onDecide}
      {...overrides}
    />,
  );
  return { onDecide };
}

describe("PermissionDialog bash prefix choice", () => {
  it("offers every prefix candidate and submits the one the user picked", async () => {
    const { onDecide } = renderBashAsk();
    await screen.findByRole("dialog");

    fireEvent.click(screen.getByRole("button", { name: "Always allow rule" }));

    // Each candidate is rendered with the trailing star that says "starts with".
    const group = await screen.findByRole("group", { name: "Bash prefix rule" });
    expect(group).toHaveTextContent("npm run build *");
    expect(group).toHaveTextContent("npm run *");
    expect(group).toHaveTextContent("npm *");

    fireEvent.click(screen.getByRole("radio", { name: /^npm run \*/ }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => {
      expect(onDecide).toHaveBeenCalledWith("call-1", "always_rule", "npm run");
    });
  });

  it("defaults to the narrowest candidate when the user just confirms", async () => {
    const { onDecide } = renderBashAsk();
    await screen.findByRole("dialog");

    fireEvent.click(screen.getByRole("button", { name: "Always allow rule" }));
    // Narrowest is preselected, so no radio click is needed.
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => {
      expect(onDecide).toHaveBeenCalledWith("call-1", "always_rule", "npm run build");
    });
  });

  it("does not offer any always-allow rule for a bash ask with no command prefix", async () => {
    renderBashAsk({ scope: "tool", prefix: undefined, prefixCandidates: undefined });
    await screen.findByRole("dialog");

    expect(screen.queryByRole("button", { name: "Always allow rule" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Always allow tool" })).toBeNull();
    expect(screen.getByRole("button", { name: "Allow once" })).toBeInTheDocument();
  });

  it("offers no always-rule for a bash-prefix ask without candidates", async () => {
    const { onDecide } = renderBashAsk({ prefixCandidates: undefined });
    await screen.findByRole("dialog");

    expect(screen.queryByRole("button", { name: "Always allow rule" })).toBeNull();
    expect(onDecide).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Allow once" })).toBeInTheDocument();
  });

  it("sends no prefix for allow-once and deny", async () => {
    const { onDecide } = renderBashAsk();
    await screen.findByRole("dialog");

    fireEvent.click(screen.getByRole("button", { name: "Allow once" }));
    await waitFor(() => {
      expect(onDecide).toHaveBeenCalledWith("call-1", "allow");
    });
  });
});
