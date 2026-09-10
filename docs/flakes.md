# Flake record

A flake is a check that failed and passed again on unchanged code. One
heading per flake, one line per sighting: `date · commit · pipeline/job ·
symptom`. The third sighting under a heading gets an issue, linked from
the heading; fixing the cause deletes the heading. Rule and format:
testing-and-ci skill (owner, 2026-09-08).

## live-topography-port-trace: waitForSelector(.note-t) times out (10 s) after other scenarios

- 2026-09-09 · 274276e8 (feature/m11-rounds-2, local) · 15-scenario batch (live-city-*, live-watchlist-*, live-topography-edges, this one, ...), scenario 11/15 · `page.waitForSelector: Timeout 10000ms exceeded` waiting for `[data-card="topography"] .note-t` at `live-topography-port-trace.mjs:276`; every check up to it passed. Ran clean against a fresh instance with no baseline feed and no preceding scenarios, same commit.
- 2026-09-09 · b41bd1f1 (feature/m11-rounds-2, local) · same 15-scenario batch, same position, after merging work/1053-rib-hook · same `.note-t` timeout at the same line; ran clean standalone again immediately after, at 2abaaa0e.

## live-rule-regex: goTo times out (10 s) on the runner

- 2026-09-08 · 3fb82271 (!1002) · pipeline 763, gate:scenarios 3/4 · `page.waitForFunction: Timeout 10000ms exceeded` at `live-browser.mjs:385` from `live-rule-regex.mjs:32`; the other 20 scenarios in the shard passed. Same family as #1011 (goTo never settles under runner load).
- 2026-09-08 · 237d4d84 (!1003) · pipeline 770, gate:scenarios 3/4 · `exited 1 without printing a result`; pipeline 773 on the same branch (one merge later, City.svelte only) passed it. 770's gate stage overlapped 773's on the same runner host.

## live-flags-watchlist: the reconnaissance row never appears (5 s)

- 2026-09-08 · 70d828ec (dev) · pipeline 775, gate:scenarios 2/4, job 8448 · `locator.waitFor: Timeout 5000ms exceeded` at `live-flags-watchlist.mjs:130` waiting for `tr.frow.mem` for 192.168.1.61 with text "Internal reconnaissance"; 776's job 8473 ran the same shard on the same commit and passed it.
- 2026-09-09 · e0c2f251 (dev) · pipeline 800, gate:scenarios 2/4, job 8848 · same `locator.waitFor` 5000 ms timeout at `live-flags-watchlist.mjs:130` for 192.168.1.61 "Internal reconnaissance"; pipeline 801 on the same commit passed.

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
- 2026-09-10 · 5265a1f8 (!1012) · pipeline 881, gate:scenarios 4/4 · `live-watchlist-manage` exited 1 without printing a result; pipelines 879-882 shared the runner

## live-decommission: goTo("Stream") times out (10 s) on the workstation

- 2026-09-09 · 4f17d079 (work/460-ui, local, standalone) · `goTo("Stream") timed out waiting for card "live"`, `offsetFromDeckTop: -105` (card mounted, the roll overshot it — #1011's shape) at `live-browser.mjs:444` from the scenario's own `session()` call, before a single check ran; the same command on the same tree passed immediately after, and three further standalone runs passed. Same family as the `live-rule-regex` and `live-settings-doors` entries above: `goTo` never settles, on a host with other work on it.

## live-learning-window: a detector's rendered line lags its own API count of sources

- 2026-09-10 · 55a84437 (dev) · pipeline 864, gate:scenarios 2/4, job 9998 · three `FAIL`s of the shape `activity_spike's rendered line matches the state its own API data describes -- got "Baselines established (69 sources)", want "... (80 sources)"` (also low_slow_scan 69/80, off_hours_activity 68/80): the page's line was read while the feed was still adding sources, so the API answered later than the render did; pipeline 865, same commit, passed the shard.

## live-policy: before any push, the popover says an empty table instead of "no table has been pushed"

- 2026-09-10 · 135615f6 (!988, pins-policy dates only) · pipeline 880, gate:scenarios 1/4 · `FAIL before any push, the popover says no table has been pushed -- not an empty table`; four pipelines shared the runner

## live-account-menu: the foot has no uptime segment

- 2026-09-10 · 751acc43 (dev) · pipeline 891, gate:scenarios 1/4, job 10361 · `FAIL the foot carries uptime as days and hours -- got "0.4.0+g751acc43… · AGPL-3.0"`: the line rendered without its `· up N d N h` tail; pipeline 893 on the same commit passed the shard.

## live-device-rename: the config-named refusal text is read empty right after the editor appears

- 2026-09-10 · 992e9da1 (!1025) · pipeline 971, gate:scenarios 1/4, job 11446 · `FAIL the editor says plainly that config.yaml supplies this name` and the `stored and never displayed` grammar check, with the no-field/no-Save checks around them passing; `editor.textContent()` is read straight after `editor.waitFor()` (`live-device-rename.mjs:247`), so a not-yet-rendered refusal reads as empty. The diff (quiet-host script, setup-commands token check, VERSION, changelog) cannot reach the editor; the same scenario passed on 961/963/969 on the same frontend. Branch pushed again and passed as pipeline 975.
