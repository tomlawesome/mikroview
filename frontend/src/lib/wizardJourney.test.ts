// SPDX-License-Identifier: AGPL-3.0-only
//
// The way in, and the way out (#1386): the decisions and the helpers a
// jsdom can prove. The six beats themselves are canvas frames, checked
// by eye against round-15's shots and by the live check.

import { beforeEach, describe, expect, it, vi } from "vitest";
import { mix, patterns, reducedMotion, rgb, strike } from "./wizardJourney";
import { wizardJourney } from "./wizardJourney.svelte";
import { wizardState } from "./wizard.svelte";
import { appState } from "./state.svelte";
import type { SetupStatus } from "./types";

function status(): SetupStatus {
  return {
    instance: {
      tlsEnabled: true,
      hosts: ["localhost"],
      syslogPort: ":6514",
      syslogEnabled: true,
      address: "",
      addressCandidates: [],
      backupTransport: "sftp",
    },
    sources: [],
    devices: [],
    pushKinds: [],
    marks: [],
    witnesses: [],
  } as unknown as SetupStatus;
}

describe("the tube: how a letter fails", () => {
  it("every pattern ends by handing the letter back plain", () => {
    for (const kind of Object.keys(patterns)) {
      const seq = patterns[kind]();
      expect(seq.length).toBeGreaterThan(0);
      expect(seq[seq.length - 1]).toEqual([0, "plain"]);
      for (const [ms, st] of seq) {
        expect(ms).toBeGreaterThanOrEqual(0);
        expect(["lit", "dark", "plain"]).toContain(st);
      }
    }
  });

  it("a stutter is lit, dark, lit, dark, lit, then plain", () => {
    expect(patterns.stutter().map(([, st]) => st)).toEqual([
      "lit",
      "dark",
      "lit",
      "dark",
      "lit",
      "plain",
    ]);
  });
});

describe("the inks", () => {
  it("reads three- and six-digit hex tokens", () => {
    expect(rgb("#ffcc00")).toEqual([255, 204, 0]);
    expect(rgb("#fc0")).toEqual([255, 204, 0]);
    expect(rgb("")).toEqual([0, 0, 0]);
  });

  it("glides between two inks rather than snapping", () => {
    expect(mix([0, 0, 0], [100, 200, 50], 0)).toBe("rgb(0,0,0)");
    expect(mix([0, 0, 0], [100, 200, 50], 0.5)).toBe("rgb(50,100,25)");
    expect(mix([0, 0, 0], [100, 200, 50], 1)).toBe("rgb(100,200,50)");
  });
});

describe("the groups strike on like a tube", () => {
  it("staggers each element by the gap through the CSSOM, never a style attribute", () => {
    vi.useFakeTimers();
    const els = [0, 1, 2].map(() => document.createElement("li"));
    strike(els, 90);
    expect(els.map((el) => el.style.animationDelay)).toEqual([
      "0ms",
      "90ms",
      "180ms",
    ]);
    expect(els.every((el) => el.classList.contains("strike"))).toBe(true);
    vi.advanceTimersByTime(1500);
    expect(els.map((el) => el.style.animationDelay)).toEqual(["", "", ""]);
    vi.useRealTimers();
  });

  it("reads the reduced-motion preference without throwing where matchMedia is absent", () => {
    const had = window.matchMedia;
    // @ts-expect-error -- jsdom may not define it; the helper must cope
    delete window.matchMedia;
    expect(reducedMotion()).toBe(false);
    window.matchMedia = had;
  });
});

describe("the way in decides once the shell has loaded", () => {
  beforeEach(() => {
    wizardJourney.end();
    wizardState.close();
    wizardState.reset();
    appState.devices = [];
    window.matchMedia = vi
      .fn()
      .mockReturnValue({
        matches: false,
      }) as unknown as typeof window.matchMedia;
  });

  it("does nothing without an Enter at the door", () => {
    expect(wizardJourney.decide()).toBe("release");
    expect(wizardJourney.active).toBe(false);
    expect(wizardState.open).toBe(false);
  });

  it("lets the door down when the wizard would not launch (a router already sends)", () => {
    wizardJourney.enter();
    expect(wizardJourney.holdDoor).toBe(true);
    appState.devices = [{ id: "rb", name: "rb" } as never];
    wizardState.status = status();
    expect(wizardJourney.decide()).toBe("release");
    expect(wizardJourney.holdDoor).toBe(false);
    expect(wizardJourney.active).toBe(false);
    expect(wizardState.open).toBe(false);
  });

  it("launches the wizard and plays when a first admin has no router sending", () => {
    wizardJourney.enter();
    wizardState.status = status();
    expect(wizardJourney.decide()).toBe("play");
    expect(wizardJourney.phase).toBe("in");
    expect(wizardState.open).toBe(true);
    // the slot is spent: the ordinary rule will not reopen it
    wizardState.close();
    wizardState.maybeAutoLaunch(false);
    expect(wizardState.open).toBe(false);
  });

  it("under reduced motion launches without the journey and lets the door down", () => {
    window.matchMedia = vi
      .fn()
      .mockReturnValue({
        matches: true,
      }) as unknown as typeof window.matchMedia;
    wizardJourney.enter();
    wizardState.status = status();
    expect(wizardJourney.decide()).toBe("release");
    expect(wizardJourney.holdDoor).toBe(false);
    expect(wizardJourney.active).toBe(false);
    expect(wizardState.open).toBe(true);
  });

  it("a second Enter while a journey runs is ignored", () => {
    wizardJourney.begin("out");
    wizardJourney.enter();
    expect(wizardJourney.pending).toBe(false);
    expect(wizardJourney.holdDoor).toBe(false);
  });
});

describe("the way out", () => {
  it("reports it cannot play without a mounted canvas, so Finish hands over outright", () => {
    wizardJourney.end();
    wizardJourney.wayOutHandler = null;
    const swap = vi.fn();
    expect(wizardJourney.wayOut(swap)).toBe(false);
    expect(swap).not.toHaveBeenCalled();
  });

  it("defers to the mounted handler when there is one", () => {
    wizardJourney.end();
    window.matchMedia = vi
      .fn()
      .mockReturnValue({
        matches: false,
      }) as unknown as typeof window.matchMedia;
    const handler = vi.fn().mockReturnValue(true);
    wizardJourney.wayOutHandler = handler;
    const swap = vi.fn();
    expect(wizardJourney.wayOut(swap)).toBe(true);
    expect(handler).toHaveBeenCalledWith(swap);
    wizardJourney.wayOutHandler = null;
  });
});
