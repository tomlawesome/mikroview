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
import argparse
import importlib.util
import ipaddress
import os
import pathlib
import random
import shutil
import stat
import tempfile
import threading
import time
import unittest
from unittest import mock

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
        # alex-laptop: ("cyclic", 1800, 0.4, 0) -- on for the first 720s of
        # every 1800s, off for the rest.
        h = next(x for x in seed_demo.HOSTS if x[4] == "alex-laptop")
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
        # alex-laptop is on border-rb5009 only, and only while cyclically on.
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
        heavy = ("rb5009", "lan", 20, "aa:bb:cc:40:01:20", "alex-desktop", 0)  # weight 3.0
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
        # lines_for_round40 also rolls an unrelated random iot->dns line
        # that matches these tests' filter about one run in ten
        # (docs/flakes.md, CamBeaconTests). A fixed seed makes that roll
        # the same every run; the beacon itself is cadence, not chance.
        random.seed(40)

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


class DefaultSyslogPortTests(unittest.TestCase):
    """#1272: live-env.sh up exports MV_SYSLOG_TLS_PORT, never the old
    plaintext MV_SYSLOG_PORT -- the default must read the variable that
    is actually set, or the feeder silently points at nothing."""

    def setUp(self):
        self._old_tls = os.environ.pop("MV_SYSLOG_TLS_PORT", None)

    def tearDown(self):
        if self._old_tls is None:
            os.environ.pop("MV_SYSLOG_TLS_PORT", None)
        else:
            os.environ["MV_SYSLOG_TLS_PORT"] = self._old_tls

    def test_reads_mv_syslog_tls_port(self):
        os.environ["MV_SYSLOG_TLS_PORT"] = "23456"
        self.assertEqual(seed_demo._default_syslog_port(), 23456)

    def test_falls_back_when_unset(self):
        self.assertIsInstance(seed_demo._default_syslog_port(), int)


class FeedRefusedConnectionTests(unittest.TestCase):
    """#1272: a demo feeder pointed at a port nobody is listening on used
    to print "connection refused" to stderr forever and never stop --
    exactly what happens if a release capture is run without the syslog
    TLS port live-env.sh actually exported. It must fail loudly instead
    of quietly sending into the void."""

    def setUp(self):
        # cmd_feed's real inter-tick pacing draws from the shared,
        # unseeded `random` module (random.uniform(3, 6)) -- even with
        # time.sleep stubbed out below, each tick still consumes a draw.
        # Save/restore state so driving several ticks here doesn't also
        # perturb whatever an unrelated, unseeded test elsewhere in this
        # file draws afterward -- general hygiene; investigated as a
        # cause of docs/flakes.md's CamBeaconTests entry and ruled out
        # (that flake reproduces with this test class not even loaded).
        self._random_state = random.getstate()

    def tearDown(self):
        random.setstate(self._random_state)

    def test_all_refused_true_when_every_attempt_was_refused(self):
        self.assertTrue(seed_demo._tick_all_refused([True, True]))

    def test_all_refused_false_when_any_attempt_succeeded(self):
        self.assertFalse(seed_demo._tick_all_refused([True, False]))

    def test_all_refused_false_when_nothing_was_attempted(self):
        # No router had traffic to send this tick -- not evidence of a
        # dead port.
        self.assertFalse(seed_demo._tick_all_refused([]))

    def test_cmd_feed_aborts_after_consecutive_fully_refused_ticks(self):
        orig_lines_for_router = seed_demo.lines_for_router
        orig_send_tls = seed_demo.send_tls
        orig_sleep = seed_demo.time.sleep
        seed_demo.lines_for_router = lambda router, elapsed, tick: ["line"]

        def _always_refused(*_a, **_k):
            raise ConnectionRefusedError("refused")

        seed_demo.send_tls = _always_refused
        seed_demo.time.sleep = lambda *_a, **_k: None
        try:
            args = argparse.Namespace(syslog_host="127.0.0.1", syslog_port=1, once=False)
            with self.assertRaises(seed_demo.FeedConnectionRefused):
                seed_demo.cmd_feed(args)
        finally:
            seed_demo.lines_for_router = orig_lines_for_router
            seed_demo.send_tls = orig_send_tls
            seed_demo.time.sleep = orig_sleep

    def test_cmd_feed_keeps_going_when_at_least_one_router_gets_through(self):
        orig_lines_for_router = seed_demo.lines_for_router
        orig_send_tls = seed_demo.send_tls
        orig_sleep = seed_demo.time.sleep
        seed_demo.lines_for_router = lambda router, elapsed, tick: ["line"]
        calls = {"n": 0}

        def _refused_except_first_router(host, port, src_ip, lines):
            calls["n"] += 1
            if src_ip != next(iter(seed_demo.ROUTERS.values()))["src"]:
                raise ConnectionRefusedError("refused")

        seed_demo.send_tls = _refused_except_first_router
        seed_demo.time.sleep = lambda *_a, **_k: None
        try:
            args = argparse.Namespace(syslog_host="127.0.0.1", syslog_port=1, once=True)
            seed_demo.cmd_feed(args)  # must not raise: --once stops after one tick anyway
            self.assertGreater(calls["n"], 0)
        finally:
            seed_demo.lines_for_router = orig_lines_for_router
            seed_demo.send_tls = orig_send_tls
            seed_demo.time.sleep = orig_sleep


class IngestPushSourceTests(unittest.TestCase):
    """#1302: since #1281 a push is accepted only from the device's own
    enrolled address, so each router's push must leave from its own
    loopback address, as send_tls's already does."""

    def test_push_leaves_from_the_routers_own_address(self):
        import http.server
        import json
        import threading

        seen = []

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                seen.append(self.client_address[0])
                self.rfile.read(int(self.headers["Content-Length"]))
                body = json.dumps({"records": 1}).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *a):
                pass

        srv = http.server.HTTPServer(("127.0.0.1", 0), Handler)
        t = threading.Thread(target=srv.serve_forever, daemon=True)
        t.start()
        try:
            url = f"http://127.0.0.1:{srv.server_address[1]}"
            seed_demo.ingest_push(url, "tok", "filter-rule", [], src_ip="127.0.0.2")
        finally:
            srv.shutdown()
        self.assertEqual(seen, ["127.0.0.2"])


class TotpClaimLockTests(unittest.TestCase):
    """#1466: fresh_totp_code claims a step under a mkdir lock
    ($MV_DIR/totp-last-counter.lock) so concurrent claimers never share one."""

    def setUp(self):
        self.dir = tempfile.mkdtemp(prefix="seed-demo-totp-")
        self.addCleanup(shutil.rmtree, self.dir, ignore_errors=True)
        self.lock = os.path.join(self.dir, "totp-last-counter.lock")
        env = mock.patch.dict(os.environ, {"MV_DIR": self.dir, "MV_TOTP_SECRET": "JBSWY3DPEHPK3PXP"})
        env.start()
        self.addCleanup(env.stop)
        code = mock.patch.object(seed_demo, "totp_code", lambda secret, counter: counter)
        code.start()
        self.addCleanup(code.stop)

    def _claim_in_thread(self, results):
        t = threading.Thread(target=lambda: results.append(seed_demo.fresh_totp_code()), daemon=True)
        t.start()
        return t

    def test_concurrent_claimers_never_share_a_counter(self):
        # A fake clock: waiting for the next 30 s step advances it instead
        # of sleeping; short sleeps (lock polling) really sleep.
        offset = [0.0]
        guard = threading.Lock()
        real_time, real_sleep = time.time, time.sleep

        def fake_time():
            return real_time() + offset[0]

        def fake_sleep(seconds):
            if seconds >= 1:
                with guard:
                    offset[0] += seconds
            else:
                real_sleep(seconds)

        shim = mock.Mock(wraps=time)
        shim.time = fake_time
        shim.sleep = fake_sleep
        results = []
        with mock.patch.object(seed_demo, "time", shim):
            threads = [self._claim_in_thread(results) for _ in range(8)]
            for t in threads:
                t.join(60)
        self.assertEqual(len(results), 8, "every claimer returns")
        self.assertEqual(len(set(results)), 8, f"counters must be distinct: {sorted(results)}")

    def test_stale_lock_does_not_block(self):
        os.mkdir(self.lock)
        old = time.time() - 60
        os.utime(self.lock, (old, old))
        results = []
        t = self._claim_in_thread(results)
        t.join(5)
        self.assertFalse(t.is_alive(), "a lock older than 30 s is removed as stale")
        self.assertEqual(len(results), 1)

    def test_fresh_lock_held_by_someone_else_makes_the_claimer_wait(self):
        os.mkdir(self.lock)
        results = []
        t = self._claim_in_thread(results)
        t.join(1.5)
        self.assertTrue(t.is_alive(), "claimer must wait while a fresh lock is held")
        self.assertEqual(results, [])
        self.assertFalse(os.path.exists(os.path.join(self.dir, "totp-last-counter")))
        os.rmdir(self.lock)
        t.join(5)
        self.assertFalse(t.is_alive(), "claimer proceeds once the lock is released")
        self.assertEqual(len(results), 1)


_REPO = pathlib.Path(__file__).resolve().parent.parent
_SAMPLE_NAMES = _REPO / "docs" / "design" / "sample-names.md"
_V4_RE = r"(?<![\d.])(?:\d{1,3}\.){3}\d{1,3}(?:/\d{1,2})?(?![\d.])"
_V6_RE = r"(?<![0-9A-Fa-f:])(?:[0-9A-Fa-f]{0,4}:){2,7}[0-9A-Fa-f]{0,4}(?:/\d{1,3})?(?![0-9A-Fa-f:])"
_DOC_V4 = [ipaddress.ip_network(n) for n in
           ("198.51.100.0/24", "192.0.2.0/24", "203.0.113.0/24")]
_DOC_V6 = ipaddress.ip_network("2001:db8::/32")
# RFC 1918, built from numbers so this file holds no private address text.
_PRIVATE_V4 = [ipaddress.ip_network((a << 24 | b << 16, p))
               for a, b, p in ((10, 0, 8), (172, 16, 12), (192, 168, 16))]


def _source_v4():
    """Every IPv4 address or CIDR in seed-demo.py's text (comments
    included), as (line number, token) pairs."""
    import re
    out = []
    for n, line in enumerate(_PATH.read_text().splitlines(), 1):
        for tok in re.findall(_V4_RE, line):
            try:
                ipaddress.ip_interface(tok)
            except ValueError:
                continue  # e.g. a version number like 1.2.3.400
            out.append((n, tok))
    return out


def _source_v6():
    import re
    out = []
    for n, line in enumerate(_PATH.read_text().splitlines(), 1):
        for tok in re.findall(_V6_RE, line):
            try:
                ipaddress.ip_interface(tok)
            except ValueError:
                continue  # e.g. a MAC address or a time of day
            out.append((n, tok))
    return out


def _sample_names_blocks():
    """{lower-cased network name: IPv4Network} from the Networks table in
    docs/design/sample-names.md, one-block rows only."""
    import re
    blocks = {}
    for line in _SAMPLE_NAMES.read_text().splitlines():
        m = re.match(r"\|\s*([^|]+?)\s*\|\s*(\d+\.\d+\.\d+\.\d+/\d+)\s*\|", line)
        if m:
            blocks[m.group(1).lower()] = ipaddress.ip_network(m.group(2))
    return blocks


class DocumentationAddressTests(unittest.TestCase):
    """#1481: the seed uses only the documentation ranges and per-network
    blocks in docs/design/sample-names.md, never a private address."""

    def test_no_private_ipv4_anywhere_in_the_source(self):
        bad = [(n, tok) for n, tok in _source_v4()
               if any(ipaddress.ip_interface(tok).ip in net for net in _PRIVATE_V4)]
        self.assertEqual(bad, [], f"private addresses (line, token): {bad[:10]} ... {len(bad)} in all")

    def test_every_address_is_in_a_documentation_range(self):
        # 127.0.0.0/8 is exempt: each router's syslog/push leaves from its
        # own loopback address (#1281, #1302), which is not a sample address.
        bad = [(n, tok) for n, tok in _source_v4()
               if not ipaddress.ip_interface(tok).ip.is_loopback
               and not any(ipaddress.ip_interface(tok).ip in net for net in _DOC_V4)]
        bad += [(n, tok) for n, tok in _source_v6()
                if ipaddress.ip_interface(tok).ip not in _DOC_V6]
        self.assertEqual(bad, [], f"addresses outside the documentation ranges: {bad[:10]} ... {len(bad)} in all")

    def test_every_cidr_is_a_network_address(self):
        # An interface address is host/prefix by nature (RouterOS
        # /ip/address), so `address=` and `wan_addr` values are exempt;
        # every other CIDR names a network and must have no host bits.
        import re
        lines = _PATH.read_text().splitlines()
        bad = []
        for n, tok in _source_v4() + _source_v6():
            if "/" not in tok:
                continue
            if re.search(r'(\baddress=|"(wan_addr|address)":\s*)"' + re.escape(tok) + '"', lines[n - 1]):
                continue
            if ipaddress.ip_interface(tok).ip != ipaddress.ip_interface(tok).network.network_address:
                bad.append((n, tok))
        self.assertEqual(bad, [], f"CIDRs with host bits set: {bad[:10]} ... {len(bad)} in all")

    def test_named_networks_get_the_block_the_table_gives(self):
        # A line that carries exactly one table name as a whole quoted
        # string ("lan", comment="wg0", ...) and IPv4 addresses is taken
        # to configure that network: each address must be in its block.
        # Interface names such as vlan10 are not matched: they do not map
        # to a table name clear-cut, so only quoted network names count.
        import re
        blocks = _sample_names_blocks()
        for want in ("lan", "servers", "iot", "guest", "wg0", "workshop", "cams", "garage"):
            self.assertIn(want, blocks, f"{want} missing from the table")
        # #1481: the 192.0.2.0/24 rows the seed already uses, one /27 per zone.
        for want, cidr in (("staff", "192.0.2.32/27"), ("office", "192.0.2.64/27"),
                           ("voip", "192.0.2.96/27"), ("wifi", "192.0.2.128/27"),
                           ("mgmt", "192.0.2.160/27"), ("core", "192.0.2.192/27")):
            self.assertIn(want, blocks, f"{want} missing from the table")
            self.assertEqual(str(blocks[want]), cidr, f"{want} has the wrong block in the table")
        checked, bad = 0, []
        for n, line in enumerate(_PATH.read_text().splitlines(), 1):
            if line.lstrip().startswith("#"):
                continue
            quoted = {q.lower() for q in re.findall(r'"([^"]+)"', line)}
            names = quoted & set(blocks)
            toks = [t for m, t in _source_v4() if m == n]
            if len(names) != 1 or not toks:
                continue
            block = blocks[names.pop()]
            for tok in toks:
                checked += 1
                if ipaddress.ip_interface(tok).ip not in block:
                    bad.append((n, tok, str(block)))
        self.assertEqual(bad, [], f"addresses outside their network's block (line, addr, block): {bad[:10]}")
        self.assertGreater(checked, 0, "no network-named address lines found; has the seed's layout changed?")


_ROOT = pathlib.Path(__file__).resolve().parent.parent
_DEMO_HISTORY = _ROOT / "cmd" / "demo-history" / "main.go"
_LIVE_ENV = _ROOT / "scripts" / "live-env.sh"


def _demo_history_entries():
    """[(name, ip, ports)] from the demoWatchlist literal in main.go."""
    import re
    text = _DEMO_HISTORY.read_text()
    m = re.search(r"var demoWatchlist = \[\]demoEntry\{(.*?)\n\}", text, re.S)
    assert m, "demoWatchlist literal not found in cmd/demo-history/main.go"
    out = []
    for row in re.finditer(r'\{"[^"]*",\s*"([^"]+)",\s*"([^"]+)",\s*\[\]int\{([^}]*)\}', m.group(1)):
        ports = [int(p) for p in row.group(3).split(",") if p.strip()]
        out.append((row.group(1), row.group(2), ports))
    return out


class DemoHistoryAddressTests(unittest.TestCase):
    """#1481: cmd/demo-history seeds the same estate as seed-demo.py, on
    documentation addresses only."""

    def test_demo_history_entries_match_the_seeds_watchlist_entries(self):
        want = [(e["name"], e["ip"], e["ports"]) for e in seed_demo.WATCHLIST_ENTRIES]
        got = _demo_history_entries()
        self.assertEqual(got, want, "cmd/demo-history demoWatchlist drifted from WATCHLIST_ENTRIES")

    def test_every_address_in_demo_history_is_in_a_documentation_range(self):
        import re
        bad, seen = [], 0
        for n, line in enumerate(_DEMO_HISTORY.read_text().splitlines(), 1):
            for tok in re.findall(_V4_RE, line):
                try:
                    ip = ipaddress.ip_interface(tok).ip
                except ValueError:
                    continue
                seen += 1
                if not ip.is_loopback and not any(ip in net for net in _DOC_V4):
                    bad.append((n, tok))
        self.assertGreater(seen, 0, "no addresses found in cmd/demo-history/main.go")
        self.assertEqual(bad, [], f"addresses outside the documentation ranges (line, addr): {bad[:10]}")


class LiveEnvHomeNetworksTests(unittest.TestCase):
    """#1481: the demo's cfg.yaml tells mikroview which blocks are inside."""

    def test_live_env_demo_config_sets_networks_home_to_the_inside_blocks(self):
        import re
        text = _LIVE_ENV.read_text()
        self.assertIn("networks", text, "scripts/live-env.sh sets no networks key in cfg.yaml")
        got = set(re.findall(r"\b\d+\.\d+\.\d+\.\d+/\d+\b", text[text.index("networks"):]))
        want = {"198.51.100.0/24", "192.0.2.32/27", "192.0.2.64/27", "192.0.2.96/27",
                "192.0.2.128/27", "192.0.2.160/27", "192.0.2.192/27",
                "192.0.2.224/28", "192.0.2.240/29", "192.0.2.248/29"}
        self.assertTrue(want <= got, f"networks.home is missing {sorted(want - got)}")
        self.assertNotIn("192.0.2.0/27", got, "192.0.2.0/27 holds the router WANs: it is outside")
        self.assertIn("home", text[text.index("networks"):], "no home key under networks")


if __name__ == "__main__":
    unittest.main()
