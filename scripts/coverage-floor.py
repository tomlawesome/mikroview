#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
"""Enforce per-package Go coverage floors, and ratchet them upward.

Ported from birdcage's scripts/coverage-floor.py (#1333), which #1290
found had nothing reading `coverage.out` at all -- it was measured and
kept as a CI artifact, and nothing failed on a regression. This is the
mechanism that reads it.

This checks STATEMENT coverage: every percentage here and in
supply-chain/coverage-floors.yml is "how many statements ran at least
once". That is the only number Go's own tooling produces.

Do not read that as "branches are invisible" -- it is narrower than it
sounds, and the difference matters when deciding what this file does not
protect. An untaken branch IS caught, because a body that never runs is
uncovered statements: a function whose true path is never taken reports
66.7%, not 100%.

What statement coverage cannot see is a compound condition. `if a && b`
reports 100% having only ever run (true, true) and (false, true): `b` was
never independently decisive, and no number says so. That is the gap
MC/DC exists to close, and the reason DO-178C and IEC 61508 ask for it at
their highest levels rather than asking for a percentage.

Per-package percentages are computed by attributing each block in the Go
coverage profile to the directory of its source file (the package), and
summing statement counts weighted the same way `go test`'s own per-package
summary line does: a package's coverage is (statements with count > 0) /
(statements). It matches `go test`'s own "coverage: X% of statements"
lines for the same profile because `go test`'s default -coverpkg only
instruments the package under test, so nothing in the merged profile
belongs to any package other than the one whose tests produced it.

Rules enforced, each one a `problems` entry below:
  - a package below its floor: coverage regressed, or a floor was set wrong.
  - a package missing a floor it should have: default mode, a package in the
    coverage profile but absent from the `floors:` mapping; section mode
    (see below), a package listed in that section but absent from the
    profile, i.e. never measured. Either way: a new package must not slip
    in unmeasured -- adding or fixing its floor is a reviewed change to
    supply-chain/coverage-floors.yml, not something this script does
    silently.
  - a package sitting more than RATCHET_SLACK points ABOVE its floor: a floor
    that never rises is a target, not a ratchet. This is what makes it one.

Exit codes: 0 clean; 1 one of the rules above fired; 2 the profile or the
floors file could not be read or parsed (fail red, never quietly green).

Two modes (#1290). Default mode checks the top-level `floors:` mapping
against every package in the profile, except any package listed under
`postgres-floors:` -- those are test:postgres's to ratchet, not test:go's,
because test:go never sets MIKROVIEW_TEST_POSTGRES and so never sees their
Postgres-backed statements run. `--section postgres-floors` is the other
side: it checks only the packages listed in that section, against a
profile test:postgres produced, applying the same three rules to that
section alone -- including a package listed there but absent from the
profile, which default mode has no equivalent check for (a package outside
a section it isn't skipping is simply not in the profile, which is
`floors:`'s own "no test files" case, not an error).

Usage: coverage-floor.py [--section SECTION] <coverage.out> [floors.yml]
  floors.yml defaults to supply-chain/coverage-floors.yml, resolved relative
  to the current directory -- run this from the repository root.

YAML parsing: hand-rolled, not PyYAML, the same choice
scripts/ci-changed-paths-guard.py makes for .gitlab-ci.yml's own lists --
coverage-floors.yml only ever has one flat `floors:` mapping of
`package: integer  # comment`, and a dozen lines of hand-parsing that shape
is simpler to read and audit than pulling in a parser for a format this
file never uses.
"""
import math
import os
import sys

TARGET_BAND = (70, 85)

# The two section names this file's `floors:`/`postgres-floors:` split
# uses. Named here, not just as string literals below, so the default-mode
# / section-mode split reads as one deliberate pair rather than a magic
# string repeated in two places.
DEFAULT_SECTION = "floors"
POSTGRES_SECTION = "postgres-floors"

# How far a package may drift above its floor before this job asks for the
# floor to be raised. Set deliberately loose.
#
# The ratchet only works if the floors track reality, so drift has to be
# caught. But the action that causes drift is somebody adding tests, and a
# check that fails the build for improving coverage teaches people to stop
# improving coverage -- or to switch the check off. A tight value here
# punishes the good move: on a small package a single new test moves the
# percentage several points at once.
#
# Five points is the compromise: small improvements land without ceremony,
# and a package that has genuinely pulled ahead still gets its floor
# raised rather than quietly keeping a floor nobody has looked at in
# months. Raise this if it turns out to nag; do not remove it, or the
# floors rot and the ratchet becomes decoration.
RATCHET_SLACK = 5


def fail(code, message):
    print(f"coverage-floor: {message}", file=sys.stderr)
    raise SystemExit(code)


def find_module_prefix(start_dir):
    """The module path from the nearest go.mod above `start_dir`.

    Coverage-profile file paths are import paths (module path + package
    directory); floors.yml keys are plain package directories
    (`internal/persist`, not `github.com/.../internal/persist`). This
    strips the module path back off so the two line up.
    """
    d = os.path.abspath(start_dir)
    while True:
        candidate = os.path.join(d, "go.mod")
        if os.path.isfile(candidate):
            try:
                with open(candidate) as f:
                    for line in f:
                        line = line.strip()
                        if line.startswith("module "):
                            return line[len("module "):].strip()
            except OSError as err:
                fail(2, f"cannot read {candidate}: {err}")
            fail(2, f"{candidate} has no 'module' line")
        parent = os.path.dirname(d)
        if parent == d:
            fail(2, "no go.mod found above the current directory; run this "
                     "from the repository root")
        d = parent


def parse_profile(path, module_prefix):
    try:
        with open(path) as f:
            lines = f.readlines()
    except OSError as err:
        fail(2, f"cannot read coverage profile {path}: {err}")

    if not lines or not lines[0].startswith("mode:"):
        fail(2, f"{path} does not look like a Go coverage profile "
                 f"(expected a 'mode:' header line first)")

    prefix = module_prefix.rstrip("/") + "/"
    total = {}
    covered = {}
    for lineno, raw in enumerate(lines[1:], start=2):
        line = raw.strip()
        if not line:
            continue
        try:
            filepart, numstmt_s, count_s = line.rsplit(" ", 2)
            file_and_pos = filepart.split(":", 1)
            file = file_and_pos[0]
            numstmt = int(numstmt_s)
            count = int(count_s)
        except (ValueError, IndexError):
            fail(2, f"{path}:{lineno}: cannot parse coverage line: {line!r}")

        if not file.startswith(prefix):
            fail(2, f"{path}:{lineno}: {file!r} is not under module "
                     f"{module_prefix!r}")
        rel = file[len(prefix):]
        pkg = os.path.dirname(rel)
        if pkg == "":
            pkg = "."
        total[pkg] = total.get(pkg, 0) + numstmt
        if count > 0:
            covered[pkg] = covered.get(pkg, 0) + numstmt

    if not total:
        fail(2, f"{path} has no coverage data after the 'mode:' header")
    return total, covered


def parse_floors(path, section=DEFAULT_SECTION, required=True):
    """The `<section>:` mapping in a floors file, as {package: floor}.

    `required` controls what happens when that section is absent or empty:
    True (the default, and always the case for the section a --section run
    names explicitly) fails red with exit 2 -- see `check_section`'s "unknown
    section" case. False (default mode's own look at `postgres-floors:`,
    to build its skip set) just returns {} -- a repo, or a test fixture,
    with no such section is not an error there, it simply has nothing to
    skip.
    """
    try:
        with open(path) as f:
            lines = f.readlines()
    except OSError as err:
        fail(2, f"cannot read floors file {path}: {err}")

    floors = {}
    in_block = False
    for lineno, raw in enumerate(lines, start=1):
        line = raw.rstrip("\n")
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if not line[0].isspace():
            in_block = (stripped.rstrip() == f"{section}:")
            continue
        if not in_block:
            continue
        body = line.split("#", 1)[0].strip()
        if not body:
            continue
        if ":" not in body:
            fail(2, f"{path}:{lineno}: expected 'package: number', got "
                     f"{stripped!r}")
        key, _, value = body.partition(":")
        key = key.strip()
        value = value.strip()
        try:
            floors[key] = int(value)
        except ValueError:
            fail(2, f"{path}:{lineno}: floor for {key!r} is not an integer: "
                     f"{value!r}")

    if not floors:
        if not required:
            return {}
        fail(2, f"{path} has no '{section}:' mapping, or it is empty")
    return floors


def check_default(got, floors_path):
    """The floors: mapping against every measured package, except any
    package postgres-floors: names -- test:postgres's to ratchet, not
    this run's (see the module docstring and #1290)."""
    floors = parse_floors(floors_path, DEFAULT_SECTION)
    skip = parse_floors(floors_path, POSTGRES_SECTION, required=False)

    if skip:
        names = ", ".join(sorted(skip))
        print(f"coverage-floor: skipped {len(skip)} package(s) measured by "
              f"test:postgres: {names}")

    problems = []
    for pkg in sorted(got):
        if pkg in skip:
            continue
        if pkg not in floors:
            problems.append(
                f"{pkg}: got {got[pkg]:.1f}%, no floor in {floors_path} -- "
                f"a new package must not slip in unmeasured -- add a floor "
                f"for it there as a reviewed change.")
            continue
        floor = floors[pkg]
        if got[pkg] < floor:
            problems.append(f"{pkg}: got {got[pkg]:.1f}%, floor {floor}%")
        elif got[pkg] > floor + RATCHET_SLACK:
            raise_to = math.floor(got[pkg])
            problems.append(
                f"{pkg}: got {got[pkg]:.1f}%, floor {floor}% -- more than "
                f"{RATCHET_SLACK} points above its floor. Raise its floor "
                f"in {floors_path} to {raise_to} to lock in the gain.")

    if problems:
        print("coverage-floor: coverage ratchet failed:\n", file=sys.stderr)
        for p in problems:
            print(f"  - {p}", file=sys.stderr)
        return 1

    band_lo, band_hi = TARGET_BAND
    in_band = sum(1 for f in floors.values() if band_lo <= f <= band_hi)
    debt = sum(1 for f in floors.values() if f < band_lo)
    print(f"coverage-floor: {len(floors)} package(s) checked, {in_band} "
          f"inside the {band_lo}-{band_hi}% band, {debt} carrying known "
          f"debt below it (including packages with no test files).")
    return 0


def check_section(got, floors_path, section):
    """One named section (e.g. postgres-floors) against its own profile,
    all three rules, scoped to only the packages that section lists. A
    section absent from the file, or present but empty, fails red via
    parse_floors(..., required=True) -- there is no such thing as an
    empty section to check."""
    floors = parse_floors(floors_path, section)

    problems = []
    for pkg in sorted(floors):
        floor = floors[pkg]
        if pkg not in got:
            problems.append(
                f"{pkg}: no coverage data in the profile -- listed under "
                f"{section}: in {floors_path} but never measured.")
            continue
        value = got[pkg]
        if value < floor:
            problems.append(f"{pkg}: got {value:.1f}%, floor {floor}%")
        elif value > floor + RATCHET_SLACK:
            raise_to = math.floor(value)
            problems.append(
                f"{pkg}: got {value:.1f}%, floor {floor}% -- more than "
                f"{RATCHET_SLACK} points above its floor. Raise its floor "
                f"in {floors_path} to {raise_to} to lock in the gain.")

    if problems:
        print(f"coverage-floor [{section}]: coverage ratchet failed:\n",
              file=sys.stderr)
        for p in problems:
            print(f"  - {p}", file=sys.stderr)
        return 1

    band_lo, band_hi = TARGET_BAND
    in_band = sum(1 for f in floors.values() if band_lo <= f <= band_hi)
    debt = sum(1 for f in floors.values() if f < band_lo)
    print(f"coverage-floor [{section}]: {len(floors)} package(s) checked, "
          f"{in_band} inside the {band_lo}-{band_hi}% band, {debt} "
          f"carrying known debt below it.")
    return 0


def check(profile_path, floors_path, section=None):
    module_prefix = find_module_prefix(".")
    total, covered = parse_profile(profile_path, module_prefix)

    got = {}
    for pkg, stmts in total.items():
        got[pkg] = 100.0 * covered.get(pkg, 0) / stmts if stmts else 0.0

    if section is not None:
        return check_section(got, floors_path, section)
    return check_default(got, floors_path)


if __name__ == "__main__":
    argv = sys.argv[1:]
    section_arg = None
    if argv[:1] == ["--section"]:
        if len(argv) < 2:
            print("usage: coverage-floor.py [--section SECTION] "
                  "<coverage.out> [floors.yml]", file=sys.stderr)
            raise SystemExit(2)
        section_arg = argv[1]
        argv = argv[2:]
    if len(argv) < 1 or len(argv) > 2:
        print("usage: coverage-floor.py [--section SECTION] <coverage.out> "
              "[floors.yml]", file=sys.stderr)
        raise SystemExit(2)
    profile_arg = argv[0]
    floors_arg = (argv[1] if len(argv) > 1
                  else os.path.join("supply-chain", "coverage-floors.yml"))
    raise SystemExit(check(profile_arg, floors_arg, section_arg))
