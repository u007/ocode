import { describe, it, expect } from "vitest";
import { pulseRowKey, stampPulseHost, resolvePulseProjectPath } from "./pulseRemoteRows";
import type { PulseRow } from "@/api/types";
import type { Project } from "@/api/types";

const OLD = "2020-01-01T00:00:00Z";

/** A row as the SERVER sends it: no `host` key at all (the Go struct has none),
 *  which is exactly the shape `stampPulseHost` exists to complete. */
function row(over: Partial<PulseRow> & { session_id: string }): PulseRow {
  return {
    project_path: "/p",
    title: `t ${over.session_id}`,
    status: "idle",
    current_task: null,
    todo: null,
    pending_ask: null,
    turn_started_at: "",
    updated_at: OLD,
    child_count: 0,
    ...over,
  } as PulseRow;
}

/** A saved remote project as THIS server registers it: the path is verbatim. */
function remote(path: string, host: string): Project {
  return { path, host } as Project;
}

describe("pulseRowKey", () => {
  it("separates the same session id on two hosts", () => {
    expect(pulseRowKey("s1", "")).not.toBe(pulseRowKey("s1", "james@h"));
  });
  it("is stable for the same pair", () => {
    expect(pulseRowKey("s1", "james@h")).toBe(pulseRowKey("s1", "james@h"));
  });
});

describe("stampPulseHost", () => {
  it("labels every row with the host the page came from", () => {
    const stamped = stampPulseHost([row({ session_id: "a" }), row({ session_id: "b" })], "james@h");
    expect(stamped.map((r) => r.host)).toEqual(["james@h", "james@h"]);
  });

  it("never overwrites a host a row already carries", () => {
    const pre = row({ session_id: "a", host: "already@known" });
    expect(stampPulseHost([pre], "james@h")[0].host).toBe("already@known");
  });

  it("stamps \"\" on a local row the server sent without the field", () => {
    // The Go row carries no `host` key, so a local row arrives as `undefined`;
    // the store's `r.host === host` lookups need it to be the empty string.
    const stamped = stampPulseHost([row({ session_id: "a" })], "");
    expect(stamped[0].host).toBe("");
    expect("host" in stamped[0]).toBe(true);
  });

  it("leaves a local row that already carries \"\" untouched", () => {
    const pre = row({ session_id: "a", host: "" });
    expect(stampPulseHost([pre], "")[0]).toBe(pre);
  });
});

describe("resolvePulseProjectPath", () => {
  const host = "james@217.216.72.49";

  it("keeps a registered path that the remote server reported verbatim", () => {
    const out = resolvePulseProjectPath("/home/james/www/aimsai2", host, [
      remote("/home/james/www/aimsai2", host),
    ]);
    expect(out).toEqual({ path: "/home/james/www/aimsai2", resolved: true });
  });

  it("maps a tilde-registered project onto the host's expanded report", () => {
    // The remote server expanded ~ when it saved the project; this server kept
    // the tilde form. A jump keyed on the expanded form matches no saved
    // project and would be refused.
    const out = resolvePulseProjectPath("/home/james/www/aimsai2", host, [
      remote("~/www/aimsai2", host),
    ]);
    expect(out).toEqual({ path: "~/www/aimsai2", resolved: true });
  });

  it("prefers the most specific registered path when several match", () => {
    const out = resolvePulseProjectPath("/home/james/www/app", host, [
      remote("~/www/app", host),
      remote("~/www/app", host),
      remote("~/app", host),
    ]);
    expect(out).toEqual({ path: "~/www/app", resolved: true });
  });

  it("leaves an unknown path alone rather than guessing", () => {
    const out = resolvePulseProjectPath("/srv/nowhere", host, [remote("~/www/aimsai2", host)]);
    expect(out).toEqual({ path: "/srv/nowhere", resolved: false });
  });

  it("ignores projects registered on a different host", () => {
    const out = resolvePulseProjectPath(
      "/home/james/www/aimsai2",
      host,
      [remote("~/www/aimsai2", "someone@else"), remote("/home/james/www/aimsai2", "someone@else")],
    );
    expect(out.resolved).toBe(false);
  });

  it("never resolves a local host's rows — their path is already registered", () => {
    // Local rows come from this server's own registry, which stores expanded
    // paths, so there is nothing to map and a suffix match could only misfire.
    const out = resolvePulseProjectPath("/home/james/www/aimsai2", "", [
      remote("~/www/aimsai2", "james@h"),
    ]);
    expect(out).toEqual({ path: "/home/james/www/aimsai2", resolved: false });
  });

  it("rejects a lone tilde registration, whose tail matches everything", () => {
    const out = resolvePulseProjectPath("/anywhere/at/all", host, [remote("~", host)]);
    expect(out.resolved).toBe(false);
  });
});
