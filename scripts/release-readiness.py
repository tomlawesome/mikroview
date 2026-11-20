#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
"""Release readiness checks for promotions into preview and main (#1504).

Specification: docs/releases/readiness-checks.md. Guide: docs/releasing.md.
Python 3, standard library only. The script never fetches; the caller does.

Usage: release-readiness.py --target preview|main [--remote NAME]
Exit: 0 ready, 1 a check failed, 2 usage error (no checks run).
"""
import datetime
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request

NO_TOKEN_HINT = "no GitLab token -- set GITLAB_TOKEN to a read_api token (CI: CI_REUSE_TOKEN)"


class CouldNot(Exception):
    """A check that cannot run; the message follows 'could not run: '."""


class Ctx:
    def __init__(self, target, remote):
        self.target = target
        self.remote = remote
        self.source = "dev" if target == "preview" else "preview"
        self.sha = git("rev-parse", "HEAD").strip()
        self.sha12 = self.sha[:12]
        self.root = git("rev-parse", "--show-toplevel").strip()
        raw = self.show("VERSION")
        self.version_raw = raw.strip() if raw is not None else None
        v = self.version_raw
        self.version = v if v is not None and re.fullmatch(r"\d+\.\d+\.\d+", v) else None

    def show(self, path, sha=None):
        r = subprocess.run(["git", "show", f"{sha or self.sha}:{path}"],
                           capture_output=True, text=True, check=False, cwd=self.root)
        return r.stdout if r.returncode == 0 else None

    def need_version(self):
        if self.version is None:
            raise CouldNot("VERSION is not X.Y.Z")
        return self.version

    def ref(self, branch):
        r = subprocess.run(["git", "rev-parse", "--verify", "-q",
                            f"refs/remotes/{self.remote}/{branch}"],
                           capture_output=True, text=True, check=False)
        if r.returncode != 0:
            raise CouldNot(f"no {self.remote}/{branch} ref; fetch it first")
        return r.stdout.strip()

    def tags(self):
        r = subprocess.run(["git", "ls-remote", "--tags", "--refs", self.remote],
                           capture_output=True, text=True, check=False)
        if r.returncode != 0:
            raise CouldNot(f"git ls-remote --tags {self.remote} failed")
        out = []
        for line in r.stdout.splitlines():
            name = line.split("\t")[-1].removeprefix("refs/tags/")
            out.append(name)
        return out

    def audit_files(self):
        v = self.need_version()
        names = git("ls-tree", "-r", "--name-only", self.sha, ".agents/reviews").splitlines()
        suffix = f"-v{v}-record.md"
        return [n for n in names if n.endswith(suffix) and n.count("/") == 2]

    def record_text(self):
        return self.show(f"docs/releases/v{self.need_version()}.md")


def git(*args):
    r = subprocess.run(["git", *args], capture_output=True, text=True, check=False)
    if r.returncode != 0:
        raise CouldNot("git {} failed".format(" ".join(args[:2])))
    return r.stdout


def token():
    return os.environ.get("CI_REUSE_TOKEN") or os.environ.get("GITLAB_TOKEN") or ""


def api(path):
    tok = token()
    if not tok:
        raise CouldNot(NO_TOKEN_HINT)
    base = os.environ.get("CI_API_V4_URL", "https://gitlab.tomlawson.io/api/v4").rstrip("/")
    pid = os.environ.get("CI_PROJECT_ID", "ai%2Fmikroview")
    url = f"{base}/projects/{pid}/{path}"
    req = urllib.request.Request(url, headers={"PRIVATE-TOKEN": tok})
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        raise CouldNot("GitLab API {} answered HTTP {}".format(path.split("?")[0], e.code))
    except (urllib.error.URLError, OSError, ValueError) as e:
        raise CouldNot("GitLab API {} unreachable or unreadable ({})".format(path.split("?")[0], type(e).__name__))


class Fail(Exception):
    pass


def check_source_tip(c):
    tip = c.ref(c.source)
    dirty = git("status", "--porcelain", "--untracked-files=no").strip()
    if c.sha != tip or dirty:
        raise Fail(f"HEAD {c.sha12} is not {c.remote}/{c.source} ({tip[:12]}), or tracked files are modified -- fetch, "
                   f"then check out {c.remote}/{c.source} cleanly and run again")
    return f"HEAD is {c.remote}/{c.source} and the tree is clean"


def check_contains_target(c):
    tip = c.ref(c.target)
    r = subprocess.run(["git", "merge-base", "--is-ancestor", tip, c.sha], check=False)
    if r.returncode == 0:
        return f"{c.source} contains {c.target}'s tip {tip[:12]}"
    if c.target == "preview":
        raise Fail(f"dev does not contain preview's tip {tip[:12]} -- the main -> dev back-merge "
                   "of the last release is missing; merge it first (docs/releasing.md step 12)")
    raise Fail(f"preview does not contain main's tip {tip[:12]} -- main has a commit preview lacks; "
               "back-merge main -> dev and promote dev -> preview again")


def vtuple(s):
    return tuple(int(x) for x in s.split("."))


def check_version(c):
    if c.version is None:
        if c.version_raw is None:
            raise CouldNot("VERSION is missing at the source commit")
        raise Fail(f"VERSION is '{c.version_raw}', not X.Y.Z -- bump VERSION on the batch before the audit "
                   "(docs/releasing.md step 2)")
    tags = [t for t in c.tags() if re.fullmatch(r"v\d+\.\d+\.\d+", t)]
    if tags:
        newest = max(tags, key=lambda t: vtuple(t[1:]))
        if vtuple(c.version) <= vtuple(newest[1:]):
            raise Fail(f"VERSION is '{c.version}', not above the newest tag {newest} -- bump VERSION on the "
                       "batch before the audit (docs/releasing.md step 2)")
        return f"VERSION {c.version} is above the newest tag {newest}"
    return f"VERSION {c.version} and no release tag yet"


def check_tag_free(c):
    v = c.need_version()
    if "v" + v in c.tags():
        raise Fail(f"tag v{v} already exists -- this version is released; bump VERSION for a "
                   "new release")
    return f"no tag v{v}"


def section(lines, start):
    out = []
    for ln in lines[start + 1:]:
        if ln.startswith("## "):
            break
        out.append(ln)
    return out


def check_changelog(c):
    v = c.need_version()
    text = c.show("CHANGELOG.md")
    if text is None:
        raise CouldNot("CHANGELOG.md is missing at the source commit")
    lines = text.splitlines()
    problems = []
    for i, ln in enumerate(lines):
        if re.match(r"## \[Unreleased\]\s*$", ln):
            if any(x.startswith(("- ", "### ")) for x in section(lines, i)):
                problems.append("entries are still under [Unreleased]")
            break
    idx = next((i for i, ln in enumerate(lines) if ln.startswith(f"## [{v}]")), None)
    if idx is None:
        problems.append(f"no '## [{v}] - YYYY-MM-DD' heading")
    else:
        m = re.fullmatch(rf"## \[{re.escape(v)}\] - (\d{{4}}-\d{{2}}-\d{{2}})\s*", lines[idx])
        ok_date = False
        if m:
            try:
                datetime.date.fromisoformat(m.group(1))
                ok_date = True
            except ValueError:
                pass
        if not ok_date:
            problems.append(f"the date on the [{v}] heading is not a real YYYY-MM-DD date")
        if not any(x.startswith("- ") for x in section(lines, idx)):
            problems.append(f"the [{v}] section is empty (no '- ' entry)")
    if problems:
        raise Fail("{} -- rename '## [Unreleased]' to '## [{}] - YYYY-MM-DD' and add an empty "
                   "'## [Unreleased]' above it, on the release branch (docs/releasing.md step 4)".format("; ".join(problems), v))
    return f"CHANGELOG.md has a dated, non-empty [{v}] section"


def check_release_surfaces(c):
    script = os.path.join(c.root, "scripts", "check-release-surfaces.sh")
    if not os.path.isfile(script):
        raise CouldNot("scripts/check-release-surfaces.sh is missing")
    r = subprocess.run(["sh", "scripts/check-release-surfaces.sh"], capture_output=True,
                       text=True, cwd=c.root, check=False)
    if r.returncode == 0:
        return "scripts/check-release-surfaces.sh passed"
    fails = [ln[len("FAIL:"):].strip() for ln in r.stdout.splitlines() if ln.startswith("FAIL:")]
    detail = "; ".join(fails) if fails else f"exit status {r.returncode} with no FAIL line"
    raise Fail(f"{len(fails) or 1} surface check(s) failed: {detail} -- fix each as it says, "
               "on the batch; make release-surfaces runs it alone")


def check_release_audit(c):
    v = c.need_version()
    files = c.audit_files()
    if not files:
        raise Fail(f"no .agents/reviews/*-v{v}-record.md file -- the pre-release audit for v{v} "
                   "must land on dev with its record before promoting (docs/releasing.md step 3)")
    if len(files) > 1:
        raise Fail(f"{len(files)} audit records for v{v} ({', '.join(files)}) -- the "
                   f"pre-release audit for v{v} must land on dev with its record before "
                   "promoting (docs/releasing.md step 3)")
    body = c.show(files[0]) or ""
    m = re.search(r"^Audited-through: ([0-9a-f]{40})\s*$", body, re.MULTILINE)
    problem = None
    if not m:
        problem = f"{files[0]} has no 'Audited-through: <40-hex sha>' line"
    else:
        r = subprocess.run(["git", "merge-base", "--is-ancestor", m.group(1), c.sha],
                           capture_output=True, check=False)
        if r.returncode != 0:
            problem = f"Audited-through {m.group(1)[:12]} is not in {c.source}'s history"
    if problem:
        raise Fail(f"{problem} -- the pre-release audit for v{v} must land on dev with its record before "
                   "promoting (docs/releasing.md step 3)")
    return f"{files[0]}, audited through {m.group(1)[:12]}"


def check_review_issue(c):
    files = c.audit_files()
    if len(files) != 1:
        raise Fail(f"no single audit record to name the review issue ({len(files)} found) -- "
                   "close the review issue with its evidence before promoting "
                   "(docs/releasing.md step 3)")
    first = (c.show(files[0]) or "").split("\n", 1)[0]
    m = re.search(r"#(\d+)", first)
    if not m:
        raise Fail(f"the first line of {files[0]} names no review issue (#N) -- close the review issue "
                   "with its evidence before promoting (docs/releasing.md step 3)")
    n = m.group(1)
    issue = api(f"issues/{n}")
    state = issue.get("state") if isinstance(issue, dict) else None
    if state != "closed":
        raise Fail("review issue #{} is {} -- close the review issue #{} with its evidence "
                   "before promoting (docs/releasing.md step 3)".format(n, state or "in an unknown state", n))
    return f"review issue #{n} is closed"


def promote_issue(c):
    rec = c.record_text()
    if rec is None:
        return None
    m = re.search(r"^promote-issue:\s*#(\d+)\s*$", rec, re.MULTILINE)
    return int(m.group(1)) if m else None


def check_milestone(c):
    v = c.need_version()
    title = "v" + v
    q = urllib.parse.quote
    ms = api(f"milestones?title={q(title)}")
    if not isinstance(ms, list) or not any(isinstance(m, dict) and m.get("title") == title for m in ms):
        raise Fail(f"no milestone titled {title} -- create it and put the release's issues in it "
                   "(docs/releasing.md step 1)")
    promo = promote_issue(c)
    issues = api(f"issues?milestone={q(title)}&state=opened&per_page=100")
    nums = [i.get("iid") for i in issues if isinstance(i, dict) and i.get("iid") != promo]
    if nums:
        shown = ", ".join(f"#{n}" for n in nums[:10])
        if len(nums) > 10:
            shown += ", ..."
        raise Fail(f"{title} has {len(nums)} open issue(s) besides the promote issue: {shown} "
                   "-- close them, or have the owner move them to the next milestone "
                   "(docs/releasing.md step 1)")
    return f"milestone {title} has no open issue besides the promote issue"


def check_source_pipeline(c):
    q = urllib.parse.quote
    ps = api("pipelines?sha={}&ref={}&source=push".format(c.sha, q(c.source, safe="")))
    tail = "wait for it, or fix it and push again, before promoting"
    if not ps:
        raise Fail(f"no {c.source} push pipeline found for {c.sha12} -- {tail}")
    p = ps[0]
    if p.get("status") != "success":
        raise Fail("the {} push pipeline for {} is {} ({}) -- {}".format(c.source, c.sha12, p.get("status"), p.get("web_url"), tail))
    return f"the {c.source} push pipeline for {c.sha12} succeeded"


def check_go_toolchain(c):
    df = c.show("Dockerfile")
    if df is None:
        raise CouldNot("Dockerfile is missing at the source commit")
    m = re.search(r"^FROM golang:(\d+)\.(\d+)\.(\d+)-alpine", df, re.MULTILINE)
    suffix = ("bump Go on the batch everywhere lint:supply-chain-pins checks (Dockerfile, "
              ".gitlab-ci.yml, supply-chain/pins-policy.json)")
    if not m:
        raise Fail(f"Dockerfile has no 'FROM golang:X.Y.Z-alpine' line -- {suffix}")
    major, minor, patch = m.group(1), m.group(2), int(m.group(3))
    url = os.environ.get("RELEASE_READINESS_GO_DL_URL", "https://go.dev/dl/?mode=json")
    try:
        with urllib.request.urlopen(url, timeout=30) as resp:
            rel = json.load(resp)
    except urllib.error.HTTPError as e:
        raise CouldNot(f"go.dev release list answered HTTP {e.code}")
    except (urllib.error.URLError, OSError, ValueError) as e:
        raise CouldNot(f"go.dev release list unreadable ({type(e).__name__})")
    best = None
    for r in rel:
        mm = re.fullmatch(r"go(\d+)\.(\d+)(?:\.(\d+))?", str(r.get("version", "")))
        if r.get("stable") and mm and mm.group(1) == major and mm.group(2) == minor:
            p = int(mm.group(3) or 0)
            best = p if best is None else max(best, p)
    have = f"go{major}.{minor}.{patch}"
    if best is None:
        raise Fail(f"the image builds with {have}, but go{major}.{minor} is no longer in Go's list of supported "
                   f"releases -- {suffix}")
    want = f"go{major}.{minor}.{best}"
    if best != patch:
        raise Fail(f"the image builds with {have}, newest is {want} -- {suffix}")
    return f"the image builds with {have}, the newest"


def check_release_record(c):
    v = c.need_version()
    path = f"docs/releases/v{v}.md"
    rec = c.show(path)
    if rec is None:
        raise Fail(f"{path} does not exist -- copy docs/releases/TEMPLATE.md to {path} and fill it in "
                   "(docs/releasing.md step 4)")
    guide = c.show("docs/releasing.md")
    if guide is None:
        raise CouldNot("docs/releasing.md is missing at the source commit")
    blob = git("rev-parse", f"{c.sha}:docs/releasing.md").strip()
    lines = [ln.strip() for ln in rec.splitlines()]
    blob_p, sign_p, other_p = [], [], []

    def field(name):
        for ln in lines:
            m = re.fullmatch(rf"{re.escape(name)}:\s*(.*)", ln)
            if m:
                return m.group(1).strip()
        return None

    if field("version") != "v" + v:
        other_p.append(f"version: is not v{v}")
    if field("guide-blob") != blob:
        blob_p.append(f"guide-blob: is not the guide's blob {blob[:12]}")
    if not field("guide-commit"):
        other_p.append("guide-commit: is missing")
    if not re.fullmatch(r"#\d+", field("promote-issue") or ""):
        other_p.append("promote-issue: is missing or not #N")

    # N: numbered steps under "What you do, in short"
    n = 0
    inside = False
    for ln in guide.splitlines():
        if ln.startswith("## "):
            inside = ln.strip() == "## What you do, in short"
            continue
        if inside and re.match(r"\d+\. ", ln):
            n += 1
    steps = []
    for ln in lines:
        m = re.fullmatch(r"step\s+(\S+):\s*(.*)", ln)
        if m:
            steps.append((m.group(1), m.group(2)))
    if [s[0] for s in steps] != [str(i) for i in range(1, n + 1)]:
        other_p.append(f"step lines are not exactly 1 to {n} in order")
    for num, val in steps:
        m = re.fullmatch(r"(done|not-needed):\s*(.*\S)", val)
        if val == "after-merge":
            continue
        if not m or "<" in m.group(2) or ">" in m.group(2):
            other_p.append(f"step {num} has no valid value (done: / not-needed: with text, "
                           "no < or >, or after-merge)")

    commit = git("log", "-1", "--format=%H", c.sha, "--", path).strip()
    author = git("log", "-1", "--format=%an", c.sha, "--", path).strip()
    trailers = [t.strip() for t in git(
        "log", "-1", "--format=%(trailers:key=Signed-off-by,valueonly)", commit).splitlines()
        if t.strip()]
    name = field("Signed-off-by")
    if name is None:
        sign_p.append("no Signed-off-by: line")
    else:
        if not name or re.search(r"[@<>]", name):
            sign_p.append(f"the Signed-off-by name '{name}' must be a name only")
        elif name != author:
            sign_p.append(f"the Signed-off-by name '{name}' differs from the commit author '{author}'")
        elif name not in trailers:
            sign_p.append("the record's last commit has no matching Signed-off-by trailer")
    problems = blob_p + sign_p + other_p
    if problems:
        if blob_p:
            fix = "guide changed; re-read docs/releasing.md and write a new record"
        elif sign_p:
            fix = ('commit the record with git commit --trailer "Signed-off-by: <your name>", '
                   'the name only, and do not squash its merge request')
        else:
            fix = 'fix the record to match docs/releasing.md "The signed release record"'
        raise Fail("{} -- {}".format("; ".join(problems), fix))
    return f"{path} is complete and signed by {name}"


CHECKS = [
    ("source-tip", check_source_tip),
    ("contains-target", check_contains_target),
    ("version", check_version),
    ("tag-free", check_tag_free),
    ("changelog", check_changelog),
    ("release-surfaces", check_release_surfaces),
    ("release-audit", check_release_audit),
    ("review-issue", check_review_issue),
    ("milestone", check_milestone),
    ("source-pipeline", check_source_pipeline),
    ("go-toolchain", check_go_toolchain),
    ("release-record", check_release_record),
]


def usage(msg):
    print(f"release-readiness: {msg}", file=sys.stderr)
    print("usage: release-readiness.py --target preview|main [--remote NAME]", file=sys.stderr)
    return 2


def main(argv):
    target = None
    remote = "gitlab"
    i = 0
    while i < len(argv):
        a = argv[i]
        if a in ("--target", "--remote"):
            if i + 1 >= len(argv):
                return usage(f"{a} needs a value")
            if a == "--target":
                target = argv[i + 1]
            else:
                remote = argv[i + 1]
            i += 2
        else:
            return usage(f"unknown argument {a}")
    if target not in ("preview", "main"):
        return usage("--target must be preview or main")
    r = subprocess.run(["git", "rev-parse", "--is-inside-work-tree"], capture_output=True, text=True, check=False)
    if r.returncode != 0 or r.stdout.strip() != "true":
        return usage("not inside a git work tree")
    try:
        c = Ctx(target, remote)
    except (CouldNot, OSError) as e:
        return usage(f"cannot read the repository ({e})")
    print("release-readiness: target {}, source {} at {}, version {}".format(c.target, c.source, c.sha12, c.version or c.version_raw or "unknown"))
    failed = []
    for cid, fn in CHECKS:
        try:
            msg = fn(c)
            print(f"ok: {cid}: {msg}")
        except Fail as e:
            failed.append(f"FAIL: {cid}: {e.args[0]}")
            print(failed[-1])
        except CouldNot as e:
            failed.append(f"FAIL: {cid}: could not run: {e}")
            print(failed[-1])
        except Exception as e:  # noqa: BLE001 - a check must never stop the run
            failed.append(f"FAIL: {cid}: could not run: {type(e).__name__}")
            print(failed[-1])
    if failed:
        print(f"release-readiness: {len(failed)} of {len(CHECKS)} checks failed:")
        for f in failed:
            print("  " + f)
        return 1
    print(f"release-readiness: Ready ({len(CHECKS)} of {len(CHECKS)} checks passed)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
