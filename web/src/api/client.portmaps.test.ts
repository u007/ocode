import { describe, it, expect, afterEach, vi } from "vitest";
import { api } from "./client";
import type { PortMapTarget } from "./types";

/** The project-scoped port-forward family is routed by query params
 *  (`?host=&project=`) while the per-forward operations carry the port in the
 *  PATH (`/{port}`, `/{port}/enable`). Those two must be composed in that
 *  order: appending `/{port}/disable` to a URL that already has a query puts
 *  the port inside `project`, so the server sees the project path as
 *  "~/www/app/3510/disable" and answers 400 "host/project_path is not a remote
 *  project registered with this server" (the POST even lands on the add route,
 *  not the enable route). The widget test mocks this module, so only a
 *  URL-level assertion can catch it. */

const TARGET: PortMapTarget = { host: "james@217.216.72.49", path: "~/www/app" };

function stubFetch(): ReturnType<typeof vi.fn> {
  const fetchMock = vi.fn(async () => new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } }));
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

/** The URL the client actually requested (fetch receives a string here). */
function requestedUrl(fetchMock: ReturnType<typeof vi.fn>): string {
  return String(fetchMock.mock.calls[0][0]);
}

/** The decoded (host, project) query pair for a project-scoped URL. */
function queryParams(url: string): { host: string; project: string } {
  const params = new URL(url, "http://localhost").searchParams;
  return { host: params.get("host") ?? "", project: params.get("project") ?? "" };
}

describe("port-forward client routing", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("keeps the remote path out of the port segment when enabling", async () => {
    const fetchMock = stubFetch();
    await api.setPortMapEnabled(3510, true, TARGET);

    const url = requestedUrl(fetchMock);
    expect(url.startsWith("/api/portmaps/3510/enable?")).toBe(true);
    expect(queryParams(url)).toEqual({ host: TARGET.host, project: TARGET.path });
  });

  it("keeps the remote path out of the port segment when disabling", async () => {
    const fetchMock = stubFetch();
    await api.setPortMapEnabled(3510, false, TARGET);

    const url = requestedUrl(fetchMock);
    expect(url.startsWith("/api/portmaps/3510/disable?")).toBe(true);
    expect(queryParams(url)).toEqual({ host: TARGET.host, project: TARGET.path });
    // The old construction produced "...&project=%7E%2Fwww%2Fapp/3510/disable".
    expect(queryParams(url).project.endsWith("/disable")).toBe(false);
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: "POST" });
  });

  it("carries the port in the path when removing", async () => {
    const fetchMock = stubFetch();
    await api.removePortMap(3510, TARGET);

    const url = requestedUrl(fetchMock);
    expect(url.startsWith("/api/portmaps/3510?")).toBe(true);
    expect(queryParams(url)).toEqual({ host: TARGET.host, project: TARGET.path });
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: "DELETE" });
  });

  it("lists and adds with the target query only", async () => {
    const fetchMock = stubFetch();
    await api.listPortMaps(TARGET);
    expect(requestedUrl(fetchMock).startsWith("/api/portmaps?")).toBe(true);
    expect(queryParams(requestedUrl(fetchMock))).toEqual({ host: TARGET.host, project: TARGET.path });

    const addFetch = stubFetch();
    await api.addPortMap(3510, 3510, TARGET);
    expect(requestedUrl(addFetch).startsWith("/api/portmaps?")).toBe(true);
    expect(queryParams(requestedUrl(addFetch))).toEqual({ host: TARGET.host, project: TARGET.path });
  });

  it("uses the desktop single-workspace family when no target is given", async () => {
    const fetchMock = stubFetch();
    await api.setPortMapEnabled(3510, false);

    const url = requestedUrl(fetchMock);
    expect(url).toBe("/api/desktop/portmaps/3510/disable");

    const removeFetch = stubFetch();
    await api.removePortMap(3510);
    expect(requestedUrl(removeFetch)).toBe("/api/desktop/portmaps/3510");

    const listFetch = stubFetch();
    await api.listPortMaps();
    expect(requestedUrl(listFetch)).toBe("/api/desktop/portmaps");
  });
});
