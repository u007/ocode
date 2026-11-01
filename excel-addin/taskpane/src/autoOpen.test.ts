import { describe, expect, it } from "vitest";
import { FakePort } from "./fakePort";

describe("auto-open setting (FakePort)", () => {
  it("is off until the user turns it on", async () => {
    const port = new FakePort();
    expect(await port.autoOpen()).toBe(false);
  });

  it("turning it on is read back, and the change is recorded like any other port operation", async () => {
    const port = new FakePort();
    await port.setAutoOpen(true);
    expect(await port.autoOpen()).toBe(true);
    expect(port.log).toEqual(["auto-open on"]);
  });

  it("turning it off is read back as off", async () => {
    const port = new FakePort();
    await port.setAutoOpen(true);
    await port.setAutoOpen(false);
    expect(await port.autoOpen()).toBe(false);
    expect(port.log).toEqual(["auto-open on", "auto-open off"]);
  });

  it("does not touch the string settings the bridge identity uses", async () => {
    const port = new FakePort();
    await port.setAutoOpen(true);
    expect(port.settings.size).toBe(0);
  });
});
