# Flake record

A flake is a check that failed and passed again on unchanged code. One
heading per flake, one line per sighting: `date · commit · pipeline/job ·
symptom`. The third sighting under a heading gets an issue, linked from
the heading; fixing the cause deletes the heading. Rule and format:
testing-and-ci skill (owner, 2026-09-08).

## live-rule-regex: goTo times out (10 s) on the runner

- 2026-09-08 · 3fb82271 (!1002) · pipeline 763, gate:scenarios 3/4 · `page.waitForFunction: Timeout 10000ms exceeded` at `live-browser.mjs:385` from `live-rule-regex.mjs:32`; the other 20 scenarios in the shard passed. Same family as #1011 (goTo never settles under runner load).
- 2026-09-08 · 237d4d84 (!1003) · pipeline 770, gate:scenarios 3/4 · `exited 1 without printing a result`; pipeline 773 on the same branch (one merge later, City.svelte only) passed it. 770's gate stage overlapped 773's on the same runner host.

## live-flags-watchlist: the reconnaissance row never appears (5 s)

- 2026-09-08 · 70d828ec (dev) · pipeline 775, gate:scenarios 2/4, job 8448 · `locator.waitFor: Timeout 5000ms exceeded` at `live-flags-watchlist.mjs:130` waiting for `tr.frow.mem` for 192.168.1.61 with text "Internal reconnaissance"; 776's job 8473 ran the same shard on the same commit and passed it.

## live-verdicts: undo never puts the chips back (5 s)

- 2026-09-08 · 9b6ce0b7 (!1004) · pipeline 787, gate:scenarios 4/4, job 8650 · `locator.waitFor: Timeout 5000ms exceeded` at `live-verdicts.mjs:136` waiting for `button.v.checked` to come back on 198.51.100.104's row after clicking undo; every check before it passed, including the ⚑ count going down from 16. Reproduced nothing locally at the same commit: the same shard in the same order reached the same count of 16 and passed, as did the scenario on its own and once more with every core pinned. The batch's diff cannot reach it -- the only file it touches anywhere near this is FilterBar.svelte, and FilterBar is imported by Deck.svelte alone, not by the flags table. Suspected, not observed: `flagsState.refresh()` replaces `list` wholesale every 5 s (App.svelte's STATS_REFRESH_MS) while `undoVerdict` holds a reference to the flag it took before awaiting the DELETE, so a poll landing inside that round trip would leave the reconciliation writing to a detached object and the row still stamped.

## live-settings-doors: goTo("Settings") times out (10 s) on the runner

- 2026-09-08 · 70d828ec (dev) · pipeline 775, gate:scenarios 3/4, job 8449 · `goTo("Settings") timed out waiting for card "engineroom"`, `offsetFromDeckTop: 1440` (card mounted, roll never reached it — #1011's shape, the opposite sign to live-memory-slider's), at `live-browser.mjs:385` from `live-settings-doors.mjs:27`; 776's 3/4 (job 8474) passed on the same commit.

## Scenarios exit 1 without a result when two gate stages overlap on the runner

Pipeline 770 (!1003, 237d4d84) had its gate stage running at the same time as
pipeline 773's on the same host. Five scenarios across three shards died
`exited 1 without printing a result`; all five passed in 773. One sighting
each, recorded together because the cause is shared (#831's contention):

- 2026-09-08 · 237d4d84 · pipeline 770, gate:scenarios 2/4 · `live-history-control`, `live-memory-slider`
- 2026-09-08 · 237d4d84 · pipeline 770, gate:scenarios 3/4 · `live-metrics-views` (and `live-rule-regex`, counted above)
- 2026-09-08 · 237d4d84 · pipeline 770, gate:scenarios 4/4 · `live-topography-furniture`
