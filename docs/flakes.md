# Flake record

A flake is a check that failed and passed again on unchanged code. One
heading per flake, one line per sighting: `date · commit · pipeline/job ·
symptom`. The third sighting under a heading gets an issue, linked from
the heading; fixing the cause deletes the heading. Rule and format:
testing-and-ci skill (owner, 2026-09-08).

## security:gosec: killed mid-scan, exit 137, no findings written

- 2026-09-22 · 9b17fa39 (fix/chr-log-changed-paths) · pipeline 1461, job 19810
  · `ERROR: Job failed: exit code 137` after 409s, with gosec still logging
  `Checking package: main` and no `gosec.sarif` produced (`No files to
  upload`). 137 is SIGKILL, so the scan was killed rather than finding
  anything. The branch changes a CI path list and a shell test, which a Go
  scan cannot reach. Retried on the same commit as job 19842: passed in 293s.
  The runner host was also running the CHR exercise (pipeline 1451) and two
  other pipelines around that time. First sighting.

## live-watchers-editor: the cloned shipped watcher's row has no open drawer

- 2026-09-22 · c6397ffe (dev) · local shard 4/4 under Firefox, first deliberate Firefox run of this shard · `FAIL the copy is already expanded, ready to be edited` at `live-watchers-editor.mjs:344` -- the sibling check 66 lines below the 2026-09-21 sighting's, same clone, same drawer-not-open shape. Every check before it passed. The immediately following Firefox run of the same shard on the same commit passed this scenario, and two Chromium runs of it passed, so the engine is not the cause. Second sighting.

- 2026-09-21 · 83609eae (dev, base of batch/v061-wave-4) · local 4-shard Firefox gate run by the #1314 sub-agent · `FAIL the copy opens into the conditions editor, ready to be changed` at `live-watchers-editor.mjs:278` -- the "Port scan (copy)" row appeared after Clone, but its `.drawer` count read 0 when checked straight after the row became visible; every other check passed. The run was on an otherwise idle worktree and the change under test (service-worker registration) does not touch the watchers bench; the same commit passed the scenario in pipeline 1386 (!1078, `gate:scenarios`).

## live-city-reach: Escape does not restore the exact pan position

- 2026-09-16 · 83b35730 (feature/m16-upgrade-guard-and-401, !1054) · pipeline 1168, `gate:scenarios 1/4` · `Escape restores the exact pan position (13.4 -> 19.3)` -- the mini-map viewport read 19.3 after the 900 ms settle instead of the 13.4 it started at; every other check in the scenario passed. The branch is backend-only (persist schema, a 401 header, `-backup`); the same scenario passed three times in a row locally at ebbce549, which contains that branch.

- 2026-09-18 · f036645d (fix/v060-audit, !1069) · pipeline 1253, `gate:scenarios 1/4` · `Escape restores the exact pan position (14.3 -> 20.8)` -- same check, same shape as the 2026-09-16 sighting: the viewport read 20.8 after the settle instead of the 14.3 it started at, every other check in the scenario passed. The commit changes two unrelated scenario scripts (live-fleet-setup-standing, live-setup-wizard) and nothing that pans the map; shard 1 passed on the parent commit in pipeline 1252.

## live-topography-trace-list: a keyboard re-trace lands on no row

- 2026-09-14 · 4a4655b2 (feature/wizard-upgrade-safety) · pipeline 1101, `gate:scenarios 4/4` · `the list stays open across a keyboard re-trace` and `the second row is now the traced one (-1)` -- `onIndex` came back `-1`, so the re-trace selected nothing rather than the wrong thing. The branch touches no topography code at all; `dev` passed the same scenario at e4296ef3 (pipeline 1100) an hour earlier, and the same branch passed it at bef16483 (pipeline 1102) with only banner-text and wizard changes in between.

## live-topography-port-trace: waitForSelector(.note-t) times out (10 s) after other scenarios

- 2026-09-09 · 274276e8 (feature/m11-rounds-2, local) · 15-scenario batch (live-city-*, live-watchlist-*, live-topography-edges, this one, ...), scenario 11/15 · `page.waitForSelector: Timeout 10000ms exceeded` waiting for `[data-card="topography"] .note-t` at `live-topography-port-trace.mjs:276`; every check up to it passed. Ran clean against a fresh instance with no baseline feed and no preceding scenarios, same commit.
- 2026-09-09 · b41bd1f1 (feature/m11-rounds-2, local) · same 15-scenario batch, same position, after merging work/1053-rib-hook · same `.note-t` timeout at the same line; ran clean standalone again immediately after, at 2abaaa0e.

## live-rule-regex: goTo times out (10 s) on the runner

- 2026-09-08 · 3fb82271 (!1002) · pipeline 763, gate:scenarios 3/4 · `page.waitForFunction: Timeout 10000ms exceeded` at `live-browser.mjs:385` from `live-rule-regex.mjs:32`; the other 20 scenarios in the shard passed. Same family as #1011 (goTo never settles under runner load).
- 2026-09-08 · 237d4d84 (!1003) · pipeline 770, gate:scenarios 3/4 · `exited 1 without printing a result`; pipeline 773 on the same branch (one merge later, City.svelte only) passed it. 770's gate stage overlapped 773's on the same runner host.

## live-rule-regex: "a fresh pattern after a refusal evaluates normally" fails on a loaded runner

Separate from the `goTo` heading above -- the same scenario, but a real
assertion rather than an infrastructure timeout, so it is recorded on its
own rather than swelling that entry's count.

- 2026-09-13 · 91315101 (!1043) · pipeline 1043, gate:scenarios 3/4 · `FAIL a fresh pattern after a refusal evaluates normally`. The check types a cheap pattern after a refusal and asserts, after a fixed `waitForTimeout(1200)`, that the toggle no longer reads "refused". The scenario's own comment at `live-rule-regex.mjs:88-90` says why it has to guess a duration: "evaluating and idle are DOM-indistinguishable, so this negative assertion still needs a real pause rather than a wait it cannot express". Under runner load the worker's debounce outruns the 1200 ms. Ran three times against fresh instances at this exact commit and passed every time; the branch cannot reach it either, its diff being flag verdict/note plumbing with `ruleMatcher.ts` and `ruleMatcher.worker.ts` untouched.

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
- 2026-09-12 · 43a3f55c (!1036, local workstation) · shard 3/4 rerun · `live-log-every-rule` (`page.goto` 30 s on networkidle), `live-routeros-ingest` (15 s waiting for `#main-content` to be visible, which the log shows already visible) and `live-memory-slider` (`g.mcut` never marked on a proposed shrink) all died before or without a verdict, while a peer agent's vitest ran in another worktree and load average sat near 80. Pipeline 1005's gate:scenarios 3/4 passed all three on this exact commit; the two it did fail are the ones !1036 fixes. The skill's rule holds — a browser-phase failure on a shared host is not evidence until it is reproduced alone.

## live-watchlist-manage: the fenced button never reads "learn again"

- 2026-09-10 · 4d37f0cf (!1026) · pipeline 977, gate:scenarios 4/4 · `FAIL the same button now reads learn again` at `live-watchlist-manage.mjs:216`; the preceding check ("fence now turns the chip to fencing") passed, so the fence itself landed and only the button's relabel was missing. Ran three times standalone at the same commit against a fresh instance: passed every time. The batch's diff cannot reach it -- it touches no frontend file at all, and nothing in the watchlist's own request path.

## live-viewer-surfaces: goTo("Flags") times out waiting for the docket card

- 2026-09-22 · c6397ffe (dev) · local shard 4/4 under Firefox · `goTo("Flags") timed out waiting for card "docket"` at `live-browser.mjs:514`, from `visibleSurfaces` before any surface was read, so the scenario threw and printed no verdict. The diagnostic says the deck and the card were both present with `offsetFromDeckTop: 2880`, so the card had mounted and the roll never settled on it -- the same family as the `live-decommission`, `live-rule-regex`, `live-settings-doors` and `live-log-every-rule` entries. The preceding Firefox run of the same shard on the same commit passed this scenario, as did two Chromium runs. First sighting.

## live-decommission: goTo("Stream") times out (10 s) on the workstation

- 2026-09-09 · 4f17d079 (work/460-ui, local, standalone) · `goTo("Stream") timed out waiting for card "live"`, `offsetFromDeckTop: -105` (card mounted, the roll overshot it — #1011's shape) at `live-browser.mjs:444` from the scenario's own `session()` call, before a single check ran; the same command on the same tree passed immediately after, and three further standalone runs passed. Same family as the `live-rule-regex` and `live-settings-doors` entries above: `goTo` never settles, on a host with other work on it.

## live-learning-window: a detector's rendered line lags its own API count of sources

- 2026-09-10 · 55a84437 (dev) · pipeline 864, gate:scenarios 2/4, job 9998 · three `FAIL`s of the shape `activity_spike's rendered line matches the state its own API data describes -- got "Baselines established (69 sources)", want "... (80 sources)"` (also low_slow_scan 69/80, off_hours_activity 68/80): the page's line was read while the feed was still adding sources, so the API answered later than the render did; pipeline 865, same commit, passed the shard.

- 2026-09-23 · 17542c5e (feature/m19-second-factor, remote gate `MV_GATE_WAIT=1 make live-check-remote`, chromium, 4 shards) · three `FAIL`s of the same shape, this time counting up rather than settled: `activity_spike ... got "Learning -- nearest source 2 of 5 samples (0 of 63 sources ready)", want "... (0 of 80 sources ready)"` (also low_slow_scan 0/62, off_hours_activity 0/62). Same cause as the sighting above -- the rendered line was read while the feed was still adding sources -- so the shortfall is 62/63 of 80 rather than a wrong number. Not re-run at this commit: the same suite's previous full run, unsharded on the workstation the evening before, did not fail this scenario, and the four other failures in this run are all the forced-enrolment door and unrelated to it. Second sighting.

## live-policy: before any push, the popover says an empty table instead of "no table has been pushed"

- 2026-09-10 · 135615f6 (!988, pins-policy dates only) · pipeline 880, gate:scenarios 1/4 · `FAIL before any push, the popover says no table has been pushed -- not an empty table`; four pipelines shared the runner

## test:go: flushForTest and a backupvault race time out on a loaded runner

- 2026-09-16 · 65fadaab (feature/1247-upgrade-fixtures-postgres, !1063) · pipeline 1180, test:go job 14963 · `internal/flags` ×4 (`TestSizedExpectationPersistenceRoundTrip`, `TestPermittedRecordSurvivesReload`, `TestClearedCountSurvivesReload`, `TestSetVerdictPersistsAndSurvivesReload`: `flushForTest: context deadline exceeded`) and `internal/backupvault` `TestUnlockRacingRemovePassphraseDoesNotRevive` (`returned <nil>, want ErrNoPassphrase`); `internal/api` took 214 s. Pipeline 1179's gate:image build was running alongside. Retry 14983 on the same commit passed.

## live-log-every-rule: goTo("Log every rule") times out (10 s) on the runner

- 2026-09-16 · 51dc2834 (chore/deps-2026-09-16, !1062) · pipeline 1179, gate:scenarios 3/4, job 14955 · `timed out waiting for card "log-every-rule"` with the card present in the deck at offset 3600. **Not a flake:** the retry (job 14984) failed identically and the fault reproduced locally under Playwright 1.63 alone — a `scrollend` from an interrupted roll ended the new roll early. Fixed in #1248 (Deck: scrollend only ends a roll once the deck has arrived). Kept here so the symptom is findable.

## TestRunMigrateDataEndToEnd: refuses a destination it just emptied

- 2026-09-14 · b0cf5bb1 (feature/wizard-upgrade-safety, local, `go test ./...`) · full-suite run · failed once; 23 reruns at the same commit (`go test . -count=3` and `-run TestRunMigrateDataEndToEnd -count=20`) all passed. Two agents were running the suite on this workstation at the same time.
  **What the symptom is not:** the reported line, `new-data is not empty (6 entr(y/ies))`, is the test's own second `runMigrateData` call being refused, which is exactly what it asserts — it is expected output, not the failure. The real failing assertion was not captured, so this entry records a sighting and nothing more. The test writes under `t.TempDir()`, so a path collision with the peer run is not the explanation either. Capture the full `--- FAIL` block next time before concluding anything.

## live-account-menu: the foot has no uptime segment

- 2026-09-10 · 751acc43 (dev) · pipeline 891, gate:scenarios 1/4, job 10361 · `FAIL the foot carries uptime as days and hours -- got "0.4.0+g751acc43… · AGPL-3.0"`: the line rendered without its `· up N d N h` tail; pipeline 893 on the same commit passed the shard.

## CamBeaconTests.test_beacon_refires_after_the_period_elapses: cam-porch's beacon line count comes back 2

- 2026-09-19 · df4ba9af (fix/v060-audit, local `python3 -m unittest scripts.seed_demo_test`) · full-file run, this the only failure · `AssertionError: 2 != 1` on `len(cam_beacon_lines)`. `lines_for_round40`'s DNS-beacon block (`scripts/seed-demo.py` ~1169) fires deterministically off `elapsed // CAM_BEACON_SECONDS`, but an earlier, unrelated block in the same function can independently emit a second line matching the test's own filter (cam-porch's mac plus `r40-iot-srv-dns`): it calls `random.choice([("r40-iot-srv-dns", 53), ("r40-iot-srv-ntp", 123)])` for a random `iot`-zone host, so whenever that random pick lands on cam-porch and `r40-iot-srv-dns` together, the count goes to 2. **Root cause confirmed, not just suspected:** the test seeds nothing and reads the shared `random` module, which Python seeds from OS entropy fresh in every process -- `CamBeaconTests` run completely alone (`python3 -m unittest scripts.seed_demo_test.CamBeaconTests`, nothing else in the process) still failed 2 of 20 runs, so this has nothing to do with test order or other tests' random draws; it is a roughly 1-in-10 chance on any given process regardless of what else runs. (An earlier note here blamed #1272's new tests shifting shared state -- ruled out by this isolation run; kept as a correction rather than deleted per this file's own header about superseded reasoning.)
- 2026-09-20 · 8f2c1f54 (agent/v061-lows-scripts worktree, local `python3 -m unittest scripts.seed_demo_test`) · same `2 != 1`, about one full run in three or four. Second sighting; the cause above is confirmed, so the fix went in with this sighting rather than waiting for a third: `CamBeaconTests.setUp` now calls `random.seed(40)`, so the unrelated iot->dns roll lands the same way every run (25 of 25 runs green after). The beacon itself is cadence-driven, so seeding changes nothing about what the test proves.

## security:trivy-fs: the vulnerability database will not download

- 2026-09-20 · f57448b7 (fix/v060-audit) · pipeline 1298, `security:trivy-fs` (job 17011) · `FATAL run error: init error: DB error: failed to download vulnerability DB ... Get "https://mirror.gcr.io/v2/": dial tcp: lookup mirror.gcr.io on 192.168.254.1:53: server misbehaving`. DNS on the runner failed to resolve the mirror; the scan never started. Retried as job 17017 on the same commit and it passed in 41s, so nothing in the tree changed the outcome. Worth knowing if it recurs: the job depends on an external registry being reachable at run time, so a third sighting should probably be an issue about caching the database rather than about the scanner.

## live-topography-port-trace: the picker offers no port chips on first read

- 2026-09-20 · 72965118 (fix/v060-audit, !1069) · pipeline 1307, gate:scenarios 4/4 (job 17183) · `FAIL the picker offers a port the window carried ()` and `the picker offers a port only a rule names`, both with an empty chip list, every check before them passed. The script waits for the `.pill.p.edit` bar (`live-topography-port-trace.mjs:337`) and reads its `.ports .chip` children in the same beat, so the chips can still be a render behind the bar. The commit changed docs/flakes.md only. Pipeline 1309 on a later head is the re-run. First sighting under this heading; the `.note-t` timeout above is the same scenario at a different check.

## live-city-river: wg0's bridge chip is not there on first read

- 2026-09-20 · 36494631 (fix/v060-audit, !1069) · pipeline 1308, gate:scenarios 1/4 (job 17205) · `FAIL wg0's bridge says its state was never pushed (chips: )` -- an empty chip list, every check before it passed. `live-city-river.mjs:87` reads `.city text.chip-t` with no wait after the river checks. The commit changed one advice string in fleet.ts and two comments. Pipeline 1309 on a later head is the re-run. First sighting.

## live-sw-navigation: Firefox reports the service worker failed on favicon.svg

- 2026-09-20 · 3744d7fe (dev, remote gate `scripts/gate-remote.sh --browser firefox --shards 4`, the suite's first Firefox run) · one shard of four · `FAIL no console errors` with `Failed to load 'http://127.0.0.1:PORT/favicon.svg'. A ServiceWorker intercepted the request and encountered an unexpected error.` raised from `workbox-*.js`. Every other check in the script passed. Re-run four times locally on the same commit under Firefox (`MV_BROWSER=firefox node scripts/live-sw-navigation.mjs`) and it passed every time, so the code is not what changed. Only Firefox surfaces a worker fetch failure as a page console error; Chromium and WebKit log it inside the worker where the harness never sees it, so if it recurs it recurs under Firefox only. Worth an issue on the third sighting about what workbox does with the favicon on a cold cache.

## npm run test (frontend): a different handful of unrelated tests fails on each full local run (#1375)

- 2026-09-23 · 9a920057 (feature/m19-second-factor, local `npm run test -- --run`, this sandboxed container) · full suite, 156 files / 3134 tests · 5 failures across four unrelated files, each `Test timed out in 5000ms`: `LiveTable.svelte.test.ts` ("does not cost dramatically more than linearly per row as the mount count grows"), `MetricsTable.svelte.test.ts` ("shows the server-reported winner for a minute the buffer fully covers"), `Topography.svelte.test.ts` ("lane 1's edge to anywhere clears its own limb, whichever slot the lane holds"), `Watchlist.svelte.test.ts` ("keeps set-aside suggestions out of the list until \"show them\" is clicked..."), plus a fifth whose header was lost to a `tail -60` on the captured output -- not re-identified. None of the four touch #1332's diff (`AccountMenu.svelte`, `AuthenticatorOverlay.svelte`, `auth.svelte.ts`). Rerun of just those four files alone, unchanged code: 403/403 passed in 52s.
- 2026-09-23 · 9a920057 (feature/m19-second-factor, local `npm run test -- --run`, same container, immediately after the sighting above) · full suite · 2 failures this time, both `#1332`'s own new/edited tests in `AccountMenu.svelte.test.ts` ("opens AuthenticatorOverlay on click", "opens the dialog in the clicked menu only, not in an unrelated mounted copy"): the account chip's own menu never opened (`aria-expanded="false"`) despite an `await fireEvent.click` + `flushSync()` sequence identical to this file's own `openMenu()` helper, which the other 12 tests in the same run used successfully. 8 further isolated runs of `AccountMenu.svelte.test.ts` alone, unchanged code: 14/14 passed every time. A different file failed each full run (LiveTable and friends the first time, AccountMenu the second), which points at contention from running 3134 tests in one process on this container rather than a fault in any one file.
- 2026-09-26 · 09bb07fe (fix/1368-paste-where, `npx vitest run` in `frontend/`, shared runner also busy with unrelated full test/build runs from other worktrees per `ps aux`) · full suite, 160 files / 3281 tests · 1 failure: `LiveTable.svelte.test.ts` ("keeps the flat stripes when events arrive during a round trip through group mode"), `Test timed out in 30000ms`. Not touched by #1368's diff (`SetupWizard.svelte`/`.test.ts`, `docs/routeros-setup.md`, `CHANGELOG.md`). Isolated re-run of just that file, unchanged code: 70/70 passed in 28s. Third sighting under this heading; filed as #1375.

## internal/api: TestHourTopsFollowsAHostRenameThroughTheRing reads an empty, complete "now" bucket

- 2026-09-23 · d8632dce (feature/1253-backend, local `go test ./internal/api/ -count=1`, this sandboxed container) · full package run · `before the rename, Talker = "" (Complete=true), want "old-name"` at `restamp_hourtops_test.go:52`, on the very first assertion -- before any HTTP call the test itself makes. The event is stamped against a `now` captured before `registerAdmin` (which now drives two extra HTTP round trips to enrol and confirm a TOTP factor, #1253), and `thisMinute()` right after reads `s.Store.HourTops()`'s *last* bucket off the real clock -- if that setup crosses a minute boundary, the event lands in the previous minute's bucket while the assertion reads a fresh, empty, already-complete one. Re-ran 5/5 immediately after, unchanged code: passed every time. First sighting. Note the fixture change is what widened the window: the same test was not flaky before #1253 added those two round trips to `registerAdmin`.

## internal/syslog: TestNextHeaderStartScalesLinearlyOnLongLTRun fails on a timing ratio — #1376

- 2026-09-26 · 18eb8dad (fix/1362-enrol-403, local `go test ./...`, this host with nine agents' builds and test runs alongside) · the same ratio assertion tripped; passed alone straight afterwards. Second sighting.

- 2026-09-23 · 1c0eef18 (feature/m19-second-factor, local `go test ./... -count=1`, this sandboxed container) · one test of 4055 · `nextHeaderStart: 256 KiB 2.273977ms, 1 MiB 24.022903ms, ratio 10.6 ... want about 4x -- the per-offset '>' scan looks unbounded again`, the assertion failing above a ratio of 10. Re-run alone on the same commit immediately afterwards: passed, ratio 3.7 (256 KiB 2.39ms, 1 MiB 8.73ms). The test measures wall-clock scan time at two buffer sizes and compares the ratio, so it reads whatever else the machine was doing; the run that tripped it was the full 55-package suite on a container also hosting other work. Nothing in the branch touches `internal/syslog`. If it recurs, the question is whether the threshold can be made to measure work rather than elapsed time, since a ratio guard on a loaded box will keep doing this.
- 2026-09-26 · 09bb07fe (fix/1368-paste-where, local `go test ./...`, shared runner also busy with unrelated full test/build runs from other worktrees per `ps aux`) · `nextHeaderStart: 256 KiB 2.014817ms, 1 MiB 25.092056ms, ratio 12.5 ... want about 4x`. Re-run alone immediately after, unchanged code: passed, ratio 7.8. Nothing in #1368's diff touches `internal/syslog`. Second sighting.

## live-connection-states: content does not return to its pre-loss position after the banner clears

- 2026-09-24 · 4c0217c6 (fix/v061-audit, !1094) · pipeline 1631, `gate:scenarios 1/4`, job 22799 · `FAIL content returns to its pre-loss position once the banner clears -- got 0, expected ~-12`. The retry on the same commit (job 22873) passed and the pipeline went green. The branch touches no banner, connection or layout code.

## frontend state.svelte.test.ts: "costs about the same to append" (#1304 E2) timing test

- 2026-09-26 · 18eb8dad (fix/1362-enrol-403, local `npx vitest run`, this host under nine parallel agents) · the append-cost comparison failed in the full run and passed alone on the same commit. The branch touches api.ts and auth state, not the store the test measures. Like the syslog ratio test, it compares elapsed time, so it reads host load. First sighting.
