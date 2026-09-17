import { describe, it, expect } from "vitest";
import { getTrustedTerminalProject, remoteForwardTarget } from "./trustedProject";

describe("remoteForwardTarget", () => {
  it("returns the host+path for a single matching remote SSH project", () => {
    const projects = [{ path: "~/www/kakiit", host: "james@217.216.72.49", remote_kind: "ssh" as const }];
    expect(remoteForwardTarget(projects, "~/www/kakiit")).toEqual({
      host: "james@217.216.72.49",
      path: "~/www/kakiit",
    });
  });

  it("returns null for a local project", () => {
    expect(remoteForwardTarget([{ path: "/Users/james/www/ocode" }], "/Users/james/www/ocode")).toBeNull();
  });

  it("returns null for a WSL project (WSL shares the Windows loopback)", () => {
    const projects = [{ path: "~/www/win", host: "wsl:Ubuntu", remote_kind: "wsl" as const }];
    expect(remoteForwardTarget(projects, "~/www/win")).toBeNull();
  });

  it("returns null when the path is absent or ambiguous", () => {
    expect(remoteForwardTarget([], "/nope")).toBeNull();
    expect(remoteForwardTarget([{ path: "/x" }], "")).toBeNull();
    const ambiguous = [
      { path: "/same", host: "user@a", remote_kind: "ssh" as const },
      { path: "/same", host: "user@b", remote_kind: "ssh" as const },
    ];
    expect(remoteForwardTarget(ambiguous, "/same")).toBeNull();
  });

  it("keeps getTrustedTerminalProject's single-match rule intact", () => {
    const projects = [{ path: "/a", host: "user@a", remote_port: 2222 }];
    expect(getTrustedTerminalProject(projects, "/a")).toEqual({ known: true, host: "user@a", remotePort: 2222 });
    expect(getTrustedTerminalProject(projects, "/b")).toEqual({ known: false });
    expect(getTrustedTerminalProject([{ path: "/a" }, { path: "/a" }], "/a")).toEqual({ known: false });
  });
});
