#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
"""Unit tests for the #738 additions to scripts/seed-demo.py: the diurnal
activity curve, per-host presence (a laptop that comes and goes, a guest
that appears once) and weighted "character" talker selection. Run with:

    python3 -m unittest scripts/seed_demo_test.py -v

No pytest dependency: this repo has none, so these use the stdlib
unittest runner, same as scripts/chr-watch's node:test suites use
node's own runner rather than pulling one in.

seed-demo.py is loaded via importlib because its filename has a hyphen
and cannot be imported with a plain `import` statement. Importing it is
safe: everything below `if __name__ == "__main__":` only runs when the
file is executed directly, never on import.
"""
import importlib.util
import os
import pathlib
import random
import shutil
import stat
import tempfile
import time
import unittest

_PATH = pathlib.Path(__file__).resolve().parent / "seed-demo.py"
_spec = importlib.util.spec_from_file_location("seed_demo", _PATH)
seed_demo = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(seed_demo)


def local_epoch_for_hour(hour):
    """An epoch timestamp whose *local* hour is `hour`, independent of
    the machine's timezone (mktime/localtime round-trip)."""
    now = time.localtime()
    struct = (now.tm_year, now.tm_mon, now.tm_mday, hour, 0, 0, 0, 0, -1)
    return time.mktime(struct)


class DiurnalFactorTests(unittest.TestCase):
    def test_night_is_quieter_than_day(self):
        night = seed_demo.diurnal_factor(local_epoch_for_hour(3))
        day = seed_demo.diurnal_factor(local_epoch_for_hour(14))
        self.assertLess(night, day)

    def test_every_hour_in_range(self):
        for hour in range(24):
            factor = seed_demo.diurnal_factor(local_epoch_for_hour(hour))
            self.assertGreaterEqual(factor, 0.15)
            self.assertLessEqual(factor, 1.0)

    def test_business_hours_are_full_volume(self):
        for hour in (9, 14, 15):
            self.assertEqual(seed_demo.diurnal_factor(local_epoch_for_hour(hour)), 1.0)

    def test_small_hours_are_quiet(self):
        for hour in (1, 2, 3, 4):
            self.assertEqual(seed_demo.diurnal_factor(local_epoch_for_hour(hour)), 0.15)

    def test_defaults_to_now(self):
        # Just needs to not raise and to return a value in the same range
        # as the explicit-timestamp form.
        factor = seed_demo.diurnal_factor()
        self.assertGreaterEqual(factor, 0.15)
        self.assertLessEqual(factor, 1.0)


class HostPresenceTests(unittest.TestCase):
    def test_default_host_present_from_its_intro_time(self):
        # core-switch: intro-after-seconds 0, no HOST_PRESENCE override.
        h = next(x for x in seed_demo.HOSTS if x[4] == "core-switch")
        self.assertTrue(seed_demo.host_active(h, 0))
        self.assertTrue(seed_demo.host_active(h, 10_000))

    def test_plain_newcomer_joins_once_and_stays(self):
        # border-rb5009's unnamed newcomer, intro-after-seconds 480.
        h = next(x for x in seed_demo.HOSTS
                  if x[0] == "border-rb5009" and x[5] == 480)
        self.assertFalse(seed_demo.host_active(h, 479))
        self.assertTrue(seed_demo.host_active(h, 480))
        self.assertTrue(seed_demo.host_active(h, 999_999))

    def test_cyclic_host_comes_and_goes(self):
        # tom-laptop: ("cyclic", 1800, 0.4, 0) -- on for the first 720s of
        # every 1800s, off for the rest.
        h = next(x for x in seed_demo.HOSTS if x[4] == "tom-laptop")
        self.assertTrue(seed_demo.host_active(h, 0))
        self.assertTrue(seed_demo.host_active(h, 719))
        self.assertFalse(seed_demo.host_active(h, 721))
        self.assertFalse(seed_demo.host_active(h, 1799))
        # Second cycle: on again from 1800.
        self.assertTrue(seed_demo.host_active(h, 1800))
        self.assertTrue(seed_demo.host_active(h, 1800 + 719))
        self.assertFalse(seed_demo.host_active(h, 1800 + 721))

    def test_once_host_appears_and_never_returns(self):
        # The rb5009 guest with mac ...:53: ("once", 300, 600).
        h = next(x for x in seed_demo.HOSTS if x[3] == "aa:bb:cc:40:04:53")
        self.assertFalse(seed_demo.host_active(h, 0))
        self.assertFalse(seed_demo.host_active(h, 299))
        self.assertTrue(seed_demo.host_active(h, 300))
        self.assertTrue(seed_demo.host_active(h, 899))
        self.assertFalse(seed_demo.host_active(h, 900))
        # Never comes back, unlike a plain newcomer.
        self.assertFalse(seed_demo.host_active(h, 1_000_000))

    def test_unknown_presence_kind_raises(self):
        h = ("border-rb5009", "core", 99, "de:ad:be:ef:00:00", None, 0)
        old = seed_demo.HOST_PRESENCE.get("de:ad:be:ef:00:00")
        seed_demo.HOST_PRESENCE["de:ad:be:ef:00:00"] = ("bogus",)
        try:
            with self.assertRaises(ValueError):
                seed_demo.host_active(h, 0)
        finally:
            if old is None:
                del seed_demo.HOST_PRESENCE["de:ad:be:ef:00:00"]
            else:
                seed_demo.HOST_PRESENCE["de:ad:be:ef:00:00"] = old

    def test_active_hosts_scopes_to_router_and_presence(self):
        # tom-laptop is on border-rb5009 only, and only while cyclically on.
        on = seed_demo.active_hosts("border-rb5009", 0)
        off = seed_demo.active_hosts("border-rb5009", 1000)
        self.assertIn("aa:bb:cc:01:02:01", [x[3] for x in on])
        self.assertNotIn("aa:bb:cc:01:02:01", [x[3] for x in off])
        self.assertNotIn("aa:bb:cc:01:02:01",
                          [x[3] for x in seed_demo.active_hosts("office-hex", 0)])


class WeightedPickTests(unittest.TestCase):
    def test_empty_list_returns_none(self):
        self.assertIsNone(seed_demo.weighted_pick([]))

    def test_single_host_always_picked(self):
        h = ("r", "z", 1, "aa:aa:aa:aa:aa:aa", None, 0)
        self.assertEqual(seed_demo.weighted_pick([h]), h)

    def test_heavier_weight_wins_most_of_the_time(self):
        heavy = ("rb5009", "lan", 20, "aa:bb:cc:40:01:20", "tom-desktop", 0)  # weight 3.0
        light = ("rb5009", "lan", 23, "aa:bb:cc:40:01:23", "tv-lounge", 0)    # weight 0.3
        random.seed(1234)
        picks = [seed_demo.weighted_pick([heavy, light]) for _ in range(300)]
        heavy_count = sum(1 for p in picks if p is heavy)
        # 3.0:0.3 is a 10:1 weight ratio; demand a clear, not a bare, majority.
        self.assertGreater(heavy_count, 250)

    def test_unweighted_hosts_default_to_baseline(self):
        # Two hosts with no HOST_WEIGHT entry split roughly evenly.
        a = ("r", "z", 1, "no:we:ig:ht:00:01", None, 0)
        b = ("r", "z", 2, "no:we:ig:ht:00:02", None, 0)
        random.seed(42)
        picks = [seed_demo.weighted_pick([a, b]) for _ in range(400)]
        a_count = sum(1 for p in picks if p is a)
        self.assertTrue(150 < a_count < 250)


class CamBeaconTests(unittest.TestCase):
    def setUp(self):
        seed_demo._r40_state["last_cam_beacon"] = -1
        seed_demo._r40_state["last_unplanned_wave"] = -1

    def test_beacon_fires_once_per_period(self):
        lines = seed_demo.lines_for_round40("rb5009", elapsed=0, tick=0)
        # cam-porch's mac is aa:bb:cc:40:03:31; assert its beacon line
        # exists on the first call (period boundary 0 != -1).
        self.assertTrue(any("aa:bb:cc:40:03:31" in l and "r40-iot-srv-dns" in l for l in lines))

    def test_beacon_does_not_refire_within_the_same_period(self):
        seed_demo.lines_for_round40("rb5009", elapsed=0, tick=0)
        # Still inside the same CAM_BEACON_SECONDS window.
        lines = seed_demo.lines_for_round40("rb5009", elapsed=10, tick=1)
        cam_beacon_lines = [l for l in lines
                             if "aa:bb:cc:40:03:31" in l and "r40-iot-srv-dns" in l]
        self.assertEqual(cam_beacon_lines, [])

    def test_beacon_refires_after_the_period_elapses(self):
        seed_demo.lines_for_round40("rb5009", elapsed=0, tick=0)
        lines = seed_demo.lines_for_round40(
            "rb5009", elapsed=seed_demo.CAM_BEACON_SECONDS + 1, tick=1)
        cam_beacon_lines = [l for l in lines
                             if "aa:bb:cc:40:03:31" in l and "r40-iot-srv-dns" in l]
        self.assertEqual(len(cam_beacon_lines), 1)


class RuleFiresConsistencyTests(unittest.TestCase):
    """#738 measurement found three rules declared `fires=True` with no
    emitting code path at all (`icmp-out`, `invalid-drop`, `smb-block`) --
    the same defect #696 fixed for three other rules, undetected because
    that fix never became a standing check. This drives every router's
    generator over a long, seeded run and asserts the declared `fires`
    flag matches what the generator actually produces, so a rule cannot
    silently drift out of sync with its own declaration again."""

    def setUp(self):
        seed_demo._r40_state["last_cam_beacon"] = -1
        seed_demo._r40_state["last_unplanned_wave"] = -1
        random.seed(738)

    def test_every_rules_fires_flag_matches_what_the_generator_emits(self):
        import re

        prefix_re = re.compile(r"\|([^|]+)\|")
        fired = set()
        # Elapsed sweeps past every host's intro time and through several
        # full periods of the cyclic laptop and the one-off guest, so
        # both get a real chance to be active.
        elapsed = 0.0
        for tick in range(1500):
            for router in seed_demo.ROUTERS:
                for line in seed_demo.lines_for_router(router, elapsed, tick):
                    m = prefix_re.search(line)
                    if m:
                        fired.add(m.group(1))
            elapsed += 4.5

        declared_true, declared_false = set(), set()
        for rules in seed_demo.FILTER_RULES.values():
            for r in rules:
                (declared_true if r["fires"] else declared_false).add(r["logPrefix"])

        never_fired = declared_true - fired
        self.assertEqual(
            never_fired, set(),
            f"declared fires=True but the generator never emits them: {never_fired}")

        wrongly_fired = declared_false & fired
        self.assertEqual(
            wrongly_fired, set(),
            f"declared fires=False but the generator emits them anyway: {wrongly_fired}")


class SeededAccountsFileTests(unittest.TestCase):
    """#1072: cmd_accounts opened seeded-accounts.txt for writing under
    /tmp/mikroview-atlas-demo/ but nothing created that directory first,
    so the write always raised FileNotFoundError -- caught and printed to
    stderr, then ignored, so the seeded passwords were silently never
    written. Points the reference dir at a fresh tmp subpath that does
    not exist yet, same shape as the real /tmp target before first run,
    and never touches the real /tmp/mikroview-atlas-demo."""

    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="seed-demo-test-")
        self.addCleanup(shutil.rmtree, self._tmp, ignore_errors=True)
        # A subpath that does not exist yet -- mirrors the real target
        # before cmd_accounts has ever run on a fresh host.
        self.ref_dir = os.path.join(self._tmp, "mikroview-atlas-demo")

    def test_write_creates_missing_dir_and_writes_file(self):
        ref = seed_demo._write_seeded_accounts(self.ref_dir)
        self.assertTrue(os.path.isfile(ref))
        with open(ref) as f:
            content = f.read()
        self.assertIn(seed_demo.DEMO_USER_PASSWORD, content)
        self.assertIn(seed_demo.DEMO_VIEWER_PASSWORD, content)

    def test_dir_created_mode_0700(self):
        seed_demo._write_seeded_accounts(self.ref_dir)
        mode = stat.S_IMODE(os.stat(self.ref_dir).st_mode)
        self.assertEqual(mode, 0o700)

    def test_file_created_mode_0600(self):
        ref = seed_demo._write_seeded_accounts(self.ref_dir)
        mode = stat.S_IMODE(os.stat(ref).st_mode)
        self.assertEqual(mode, 0o600)


if __name__ == "__main__":
    unittest.main()
