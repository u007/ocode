import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import PermissionDialog, { extractInterpreterScript } from "./PermissionDialog";

describe("extractInterpreterScript", () => {
  it("extracts python script path", () => {
    expect(extractInterpreterScript("python3 /tmp/foo.py")).toBe("/tmp/foo.py");
  });
  it("extracts node script path", () => {
    expect(extractInterpreterScript("node index.js")).toBe("index.js");
  });
  it("handles deno run", () => {
    expect(extractInterpreterScript("deno run main.ts")).toBe("main.ts");
  });
  it("skips flags", () => {
    expect(extractInterpreterScript("python3 -u /x/y.py")).toBe("/x/y.py");
  });
  it("returns null for inline code", () => {
    expect(extractInterpreterScript('python3 -c "print(1)"')).toBeNull();
  });
  it("returns null for non-interpreter", () => {
    expect(extractInterpreterScript("rm -rf build")).toBeNull();
  });
});

describe("PermissionDialog interpreter script link", () => {
  it("renders clickable script path and calls onOpenFile", async () => {
    const onOpenFile = vi.fn();
    const onDecide = vi.fn(async () => ({ ok: true as const }));
    render(
      <PermissionDialog
        container={document.body}
        open={true}
        tool="bash"
        command="python3 /tmp/script.py"
        requestId="r1"
        onDecide={onDecide}
        onOpenFile={onOpenFile}
      />,
    );
    const link = await screen.findByTestId("interpreter-script-link");
    expect(link).toHaveTextContent("/tmp/script.py");
    fireEvent.click(link);
    expect(onOpenFile).toHaveBeenCalledWith("/tmp/script.py");
  });
});
