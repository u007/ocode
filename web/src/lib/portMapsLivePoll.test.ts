import { describe, it, expect } from "vitest";
import {
  LIVE_POLL_MAX_ATTEMPTS,
  pendingLiveForwards,
  shouldPollForLive,
} from "./portMapsLivePoll";
import type { PortMapView } from "@/api/types";

const map = (over: Partial<PortMapView>): PortMapView => ({
  remote_port: 3000,
  local_port: 3000,
  enabled: true,
  live: false,
  ...over,
});

describe("portMapsLivePoll", () => {
  describe("pendingLiveForwards", () => {
    it("treats an enabled but not-yet-live forward as pending", () => {
      expect(pendingLiveForwards([map({ enabled: true, live: false })])).toHaveLength(1);
    });

    it("does not treat a live forward as pending", () => {
      expect(pendingLiveForwards([map({ enabled: true, live: true })])).toHaveLength(0);
    });

    it("never treats a DISABLED forward as pending, or re-fetching could never converge", () => {
      expect(pendingLiveForwards([map({ enabled: false, live: false })])).toHaveLength(0);
    });

    it("reports only the pending ones from a mixed list", () => {
      const got = pendingLiveForwards([
        map({ remote_port: 1, enabled: true, live: true }),
        map({ remote_port: 2, enabled: true, live: false }),
        map({ remote_port: 3, enabled: false, live: false }),
      ]);
      expect(got.map((m) => m.remote_port)).toEqual([2]);
    });
  });

  describe("shouldPollForLive", () => {
    const stuck = [map({ enabled: true, live: false })];

    it("polls while an enabled forward is still opening", () => {
      expect(shouldPollForLive(stuck, 0)).toBe(true);
      expect(shouldPollForLive(stuck, LIVE_POLL_MAX_ATTEMPTS - 1)).toBe(true);
    });

    it("gives up at the budget so a dead host cannot poll forever", () => {
      expect(shouldPollForLive(stuck, LIVE_POLL_MAX_ATTEMPTS)).toBe(false);
      expect(shouldPollForLive(stuck, LIVE_POLL_MAX_ATTEMPTS + 5)).toBe(false);
    });

    it("stops as soon as everything enabled is live", () => {
      expect(shouldPollForLive([map({ enabled: true, live: true })], 0)).toBe(false);
    });

    it("stops when there is nothing at all", () => {
      expect(shouldPollForLive([], 0)).toBe(false);
    });

    it("does not spend the budget on disabled rows", () => {
      expect(shouldPollForLive([map({ enabled: false, live: false })], 0)).toBe(false);
    });
  });
});
