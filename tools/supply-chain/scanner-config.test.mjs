// Tests for #1462 steps 2-3: scanner configuration carries a reason for every
// suppression, and no checkout leaves a token behind. Run with
// `node --test tools/supply-chain/scanner-config.test.mjs`. Same node:test
// style as scanner-jobs.test.mjs: no dependencies, TOML, YAML and JSON are
// read in-file by small line parsers (CI's node image has no Python and no
// git). The parsers are exercised on fixture strings too, so a check that
// passes the repo is also shown to fail on a bad input.
//
// CONVENTION for the builder (reasons in files that cannot hold comments):
//   - TOML, YAML, .shellcheckrc: a `#` comment on the same line as the entry,
//     or on the line directly above it. Per entry, not per list: every
//     element of a multi-line list needs its own. In YAML a nested list item
//     (e.g. `- errcheck` under a rule) is covered by its parent item's reason.
//   - JSON (frontend/knip.json ignore* keys, .jscpd.json "ignore"): either a
//     non-empty "//" string key in the same object as the ignore key, or a
//     non-empty string in a top-level "$reasons" object keyed by the ignored
//     entry (the glob, package or binary name). Empty lists need neither.
//   - A workflow checkout needs `persist-credentials: false` in `with:`, or
//     `# zizmor: ignore[artipacked] <reason>` on its `uses:` line.
// A config file that does not exist yet (.shellcheckrc, frontend/knip.json,
// .jscpd.json) is a failure, not a skip.
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";
import assert from "node:assert/strict";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
function must(p) {
  assert.ok(existsSync(join(root, p)), `${p} does not exist`);
  return readFileSync(join(root, p), "utf8");
}

// ---- shared line helpers (TOML, YAML, rc files) ----------------------------
// Text after an unquoted `#`, or "" if none.
function trailingComment(line) {
  let q = null;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (q) { if (c === q && line[i - 1] !== "\\") q = null; continue; }
    if (c === '"' || c === "'") q = c;
    else if (c === "#") return line.slice(i + 1).trim();
  }
  return "";
}
function firstHash(line) {
  let q = null;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (q) { if (c === q && line[i - 1] !== "\\") q = null; continue; }
    if (c === '"' || c === "'") q = c;
    else if (c === "#") return i;
  }
  return line.length;
}
const stripComment = (line) => line.slice(0, firstHash(line));
const isComment = (line) => /^\s*#/.test(line);
const hasReasonAt = (lines, i) =>
  trailingComment(lines[i]).length >= 3 ||
  (i > 0 && isComment(lines[i - 1]) && lines[i - 1].replace(/^\s*#+/, "").trim().length >= 3);
const indentOf = (l) => l.match(/^\s*/)[0].length;
const strings = (s) => [...s.matchAll(/"([^"]*)"|'([^']*)'/g)].map((m) => m[1] ?? m[2]);

// ---- TOML (ruff.toml) -------------------------------------------------------
const RUFF_LIST_KEYS = new Set(["ignore", "extend-ignore", "per-file-ignores", "extend-per-file-ignores", "exclude", "extend-exclude"]);
const isPerFile = (name) => /(^|\.)(extend-)?per-file-ignores$/.test(name);

// Entry lines of ruff's ignore/exclude/per-file-ignores keys:
// [{ n: line number, line, key, perFile }].
function ruffEntries(text) {
  const lines = text.split("\n");
  const out = [];
  let section = "";
  let open = null; // key of a multi-line array still open
  lines.forEach((raw, i) => {
    const line = stripComment(raw);
    if (!line.trim()) return;
    if (open) {
      if (strings(line).length) out.push({ n: i, line: raw, key: open.key, perFile: open.perFile });
      if (line.includes("]")) open = null;
      return;
    }
    const sec = line.match(/^\s*\[([^\]]+)\]\s*$/);
    if (sec) { section = sec[1].trim(); return; }
    const kv = line.match(/^\s*("[^"]+"|'[^']+'|[A-Za-z0-9_.-]+)\s*=\s*(.*)$/);
    if (!kv) return;
    const key = kv[1].replace(/^["']|["']$/g, "");
    if (isPerFile(section)) {
      out.push({ n: i, line: raw, key: section, perFile: true, pathKey: key, value: kv[2] });
      if (kv[2].includes("[") && !kv[2].includes("]")) open = { key: section, perFile: true };
      return;
    }
    if (!RUFF_LIST_KEYS.has(key)) return;
    const perFile = isPerFile(key);
    if (strings(kv[2]).length) out.push({ n: i, line: raw, key, perFile, value: kv[2] });
    if ((kv[2].includes("[") || kv[2].includes("{")) && !/[\]}]\s*$/.test(kv[2].trim())) open = { key, perFile };
  });
  return out;
}
function ruffMissingReasons(text) {
  const lines = text.split("\n");
  return ruffEntries(text).filter((e) => !hasReasonAt(lines, e.n)).map((e) => `ruff.toml:${e.n + 1}: ${lines[e.n].trim()}`);
}
// Rule codes ignored for everything: `ignore`/`extend-ignore` containing S or
// ALL, or a per-file-ignores entry that ignores S or ALL.
function ruffGlobalSecurityIgnores(text) {
  const bad = [];
  for (const e of ruffEntries(text)) {
    const vals = strings(e.perFile && e.value === undefined ? e.line : (e.value ?? e.line));
    const codes = e.perFile ? vals.filter((v) => v !== e.pathKey) : vals;
    if (codes.some((c) => c === "S" || c === "ALL")) bad.push(`ruff.toml:${e.n + 1}: ${e.line.trim()}`);
  }
  return bad.filter((b, i, a) => a.indexOf(b) === i);
}

// ---- .shellcheckrc ----------------------------------------------------------
function shellcheckrcProblems(text) {
  const lines = text.split("\n");
  const bad = [];
  lines.forEach((raw, i) => {
    const m = stripComment(raw).match(/^\s*disable\s*=\s*(.*)$/);
    if (!m) return;
    const codes = m[1].split(",").map((s) => s.trim()).filter(Boolean);
    if (codes.includes("all")) bad.push(`.shellcheckrc:${i + 1}: disable=all is a blanket disable`);
    if (!hasReasonAt(lines, i)) bad.push(`.shellcheckrc:${i + 1}: no reason comment: ${raw.trim()}`);
  });
  return bad;
}

// ---- YAML (.golangci.yml) ---------------------------------------------------
const GOLANGCI_KEYS = new Set(["exclusions", "disable", "exclude", "exclude-dirs", "exclude-files", "exclude-rules", "skip-dirs", "skip-files", "paths", "paths-except", "rules", "presets"]);
const significant = (l) => l.trim() && !isComment(l);

// Line numbers inside an ignore/exclude block that are list items, plus
// inline values on the key line itself.
function golangciEntries(lines) {
  const set = new Set();
  lines.forEach((raw, i) => {
    const m = stripComment(raw).match(/^(\s*)([A-Za-z0-9_-]+):\s*(.*)$/);
    if (!m || !GOLANGCI_KEYS.has(m[2])) return;
    if (m[3].trim() && /^\[/.test(m[3].trim())) set.add(i);
    for (let j = i + 1; j < lines.length; j++) {
      if (!significant(lines[j])) continue;
      const ind = indentOf(lines[j]);
      if (ind <= m[1].length && !/^\s*-\s/.test(lines[j])) break;
      if (ind < m[1].length) break;
      if (/^\s*-\s/.test(lines[j])) set.add(j);
    }
  });
  return [...set].sort((a, b) => a - b);
}
function golangciMissingReasons(text) {
  const lines = text.split("\n");
  const bad = [];
  for (const i of golangciEntries(lines)) {
    let ok = hasReasonAt(lines, i);
    // a nested item is covered by an enclosing list item that has a reason
    for (let j = i - 1, ind = indentOf(lines[i]); !ok && j >= 0; j--) {
      if (!significant(lines[j])) continue;
      const jind = indentOf(lines[j]);
      if (jind < ind) { if (/^\s*-\s/.test(lines[j])) ok = hasReasonAt(lines, j); ind = jind; if (!/^\s*-\s/.test(lines[j]) && !/^\s*[A-Za-z0-9_-]+:\s*$/.test(lines[j])) break; }
    }
    if (!ok) bad.push(`.golangci.yml:${i + 1}: ${lines[i].trim()}`);
  }
  return bad;
}
// errcheck may be silenced only by an exclusion rule whose path is _test.go.
function golangciErrcheckOutsideTests(text) {
  const lines = text.split("\n");
  const bad = [];
  lines.forEach((raw, i) => {
    const m = stripComment(raw).match(/^(\s*)([A-Za-z0-9_-]+):\s*(.*)$/);
    if (!m) return;
    const end = (from, ind) => {
      let j = from + 1;
      while (j < lines.length && (!significant(lines[j]) || indentOf(lines[j]) > ind || (indentOf(lines[j]) === ind && /^\s*-\s/.test(lines[j])))) j++;
      return j;
    };
    if (m[2] === "disable") {
      const body = lines.slice(i, end(i, m[1].length)).map(stripComment).join("\n");
      if (/\berrcheck\b/.test(body)) bad.push(`.golangci.yml:${i + 1}: errcheck is disabled for the whole repo`);
    }
    if (m[2] === "rules" && /^\s*-\s/.test(lines[i + 1] ?? "")) {
      let j = i + 1;
      const stop = end(i, m[1].length);
      while (j < stop) {
        const itemInd = indentOf(lines[j]);
        let k = j + 1;
        while (k < stop && !(significant(lines[k]) && indentOf(lines[k]) <= itemInd && /^\s*-\s/.test(lines[k]))) k++;
        const item = lines.slice(j, k).map(stripComment).join("\n");
        if (/\berrcheck\b/.test(item) && !/path:\s*['"]?[^\n]*_test\\?\.go/.test(item)) bad.push(`.golangci.yml:${j + 1}: errcheck excluded outside _test.go`);
        j = k;
      }
    }
  });
  return bad;
}

// ---- JSON (knip.json, .jscpd.json) ------------------------------------------
// Ignore entries and whether each has a reason, per the convention above.
function jsonIgnoreProblems(file, json, keyTest = /^ignore/) {
  const reasons = json?.$reasons && typeof json.$reasons === "object" ? json.$reasons : {};
  const bad = [];
  const nonEmpty = (v) => typeof v === "string" && v.trim().length >= 3;
  (function walk(obj, path) {
    if (!obj || typeof obj !== "object" || Array.isArray(obj)) return;
    for (const [k, v] of Object.entries(obj)) {
      if (k === "$reasons") continue;
      if (keyTest.test(k) && (Array.isArray(v) || (v && typeof v === "object"))) {
        const entries = Array.isArray(v) ? v.filter((x) => typeof x === "string") : Object.keys(v);
        for (const e of entries) {
          if (!nonEmpty(obj["//"]) && !nonEmpty(reasons[e])) bad.push(`${file}: ${[...path, k].join(".")} entry ${JSON.stringify(e)} has no reason`);
        }
      } else walk(v, [...path, k]);
    }
  })(json, []);
  return bad;
}

// ---- workflows --------------------------------------------------------------
// Checkout steps with neither persist-credentials: false nor a reasoned
// zizmor ignore on the `uses:` line.
function checkoutProblems(file, text) {
  const lines = text.split("\n");
  const bad = [];
  lines.forEach((raw, i) => {
    if (!/^\s*(-\s+)?uses:\s*actions\/checkout(@|\s|$)/.test(raw)) return;
    let start = i;
    const usesInd = indentOf(raw);
    if (!/^\s*-\s/.test(raw)) {
      for (let j = i - 1; j >= 0; j--) {
        if (significant(lines[j]) && /^\s*-\s/.test(lines[j]) && indentOf(lines[j]) < usesInd) { start = j; break; }
      }
    }
    const itemInd = indentOf(lines[start]);
    let end = lines.length;
    for (let j = start + 1; j < lines.length; j++) {
      if (significant(lines[j]) && indentOf(lines[j]) <= itemInd) { end = j; break; }
    }
    const step = lines.slice(start, end).map(stripComment).join("\n");
    const persistOff = /^\s*persist-credentials:\s*['"]?false['"]?\s*$/m.test(step);
    const ignored = /#\s*zizmor:\s*ignore\[artipacked\][\s:,;-]*\S{3,}/.test(raw);
    if (!persistOff && !ignored) bad.push(`${file}:${i + 1}: actions/checkout without persist-credentials: false or a reasoned "# zizmor: ignore[artipacked]"`);
  });
  return bad;
}

// ===========================================================================
describe("#1462 step 3: the parsers catch what they should (fixtures)", () => {
  it("ruff: an entry with no reason is flagged, same-line and line-above reasons pass", () => {
    const bad = `[lint]\nignore = ["E501"]\n`;
    assert.equal(ruffMissingReasons(bad).length, 1);
    const good = `[lint]\nignore = ["E501"] # long URLs\n[lint.per-file-ignores]\n# test asserts\n"tests/**" = ["S101"]\n`;
    assert.deepEqual(ruffMissingReasons(good), []);
    const multi = `exclude = [\n  "docs/design/**", # mock-ups, not shipped\n  "build/**",\n]\n`;
    assert.equal(ruffMissingReasons(multi).length, 1);
    const pf = `[lint.per-file-ignores]\n"tests/**" = ["S101"]\n`;
    assert.equal(ruffMissingReasons(pf).length, 1);
  });
  it("ruff: a global S ignore is flagged, S101 in tests is not", () => {
    assert.equal(ruffGlobalSecurityIgnores(`[lint]\nignore = ["S"]\n`).length, 1);
    assert.equal(ruffGlobalSecurityIgnores(`[lint]\nextend-ignore = ["E501", "ALL"]\n`).length, 1);
    assert.equal(ruffGlobalSecurityIgnores(`[lint.per-file-ignores]\n"**" = ["S"]\n`).length, 1);
    assert.deepEqual(ruffGlobalSecurityIgnores(`[lint]\nextend-select = ["S"]\n[lint.per-file-ignores]\n"tests/**" = ["S101"] # asserts\n`), []);
  });
  it("shellcheckrc: a disable with no reason, or disable=all, is flagged", () => {
    assert.equal(shellcheckrcProblems("disable=SC1007\n").length, 1);
    assert.deepEqual(shellcheckrcProblems("# VAR= cmd idiom is intended\ndisable=SC1007\n"), []);
    assert.equal(shellcheckrcProblems("# all\ndisable=all\n").length, 1);
  });
  it("golangci: a missing reason and errcheck outside _test.go are flagged", () => {
    const noReason = `linters:\n  exclusions:\n    rules:\n      - path: _test\\.go\n        linters:\n          - errcheck\n`;
    assert.ok(golangciMissingReasons(noReason).length >= 1);
    const reasoned = `linters:\n  exclusions:\n    rules:\n      # tests ignore close errors\n      - path: _test\\.go\n        linters:\n          - errcheck\n`;
    assert.deepEqual(golangciMissingReasons(reasoned), []);
    assert.deepEqual(golangciErrcheckOutsideTests(reasoned), []);
    const everywhere = `linters:\n  exclusions:\n    rules:\n      - linters:\n          - errcheck\n`;
    assert.equal(golangciErrcheckOutsideTests(everywhere).length, 1);
    assert.equal(golangciErrcheckOutsideTests(`linters:\n  disable:\n    - errcheck\n`).length, 1);
  });
  it("json: an ignore entry needs a sibling // or a $reasons entry", () => {
    assert.equal(jsonIgnoreProblems("k", { ignoreDependencies: ["undici"] }).length, 1);
    assert.deepEqual(jsonIgnoreProblems("k", { "//": "live-check only", ignoreDependencies: ["undici"] }), []);
    assert.deepEqual(jsonIgnoreProblems("k", { $reasons: { undici: "used by live-check" }, ignoreDependencies: ["undici"] }), []);
    assert.deepEqual(jsonIgnoreProblems("k", { ignoreDependencies: [] }), []);
  });
  it("workflow: a checkout without persist-credentials: false is flagged", () => {
    const bare = `jobs:\n  a:\n    steps:\n      - uses: actions/checkout@v4\n      - run: x\n`;
    assert.equal(checkoutProblems("w.yml", bare).length, 1);
    const off = `jobs:\n  a:\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          persist-credentials: false\n`;
    assert.deepEqual(checkoutProblems("w.yml", off), []);
    const named = `jobs:\n  a:\n    steps:\n      - name: Checkout\n        uses: actions/checkout@v4\n        with:\n          fetch-depth: 0\n      - run: x\n`;
    assert.equal(checkoutProblems("w.yml", named).length, 1);
    const ign = `jobs:\n  a:\n    steps:\n      - uses: actions/checkout@v4 # zizmor: ignore[artipacked] pushes with the token\n`;
    assert.deepEqual(checkoutProblems("w.yml", ign), []);
    const ignNoReason = ign.replace(" pushes with the token", "");
    assert.equal(checkoutProblems("w.yml", ignNoReason).length, 1);
    const otherStepOff = `jobs:\n  a:\n    steps:\n      - uses: actions/checkout@v4\n      - uses: other/x@v1\n        with:\n          persist-credentials: false\n`;
    assert.equal(checkoutProblems("w.yml", otherStepOff).length, 1);
  });
});

describe("#1462 step 3: scanner suppressions carry reasons", () => {
  it("ruff.toml: every ignore/exclude/per-file-ignore entry has a reason comment", () => {
    assert.deepEqual(ruffMissingReasons(must("ruff.toml")), []);
  });
  it("ruff.toml: does not ignore S (or ALL) globally", () => {
    assert.deepEqual(ruffGlobalSecurityIgnores(must("ruff.toml")), []);
  });
  it("ruff.toml: excludes docs/design/**, and ignores S101/S311 only per file", () => {
    const t = must("ruff.toml");
    assert.match(stripAll(t), /docs\/design\/\*\*/, "docs/design/** is not excluded");
    const per = ruffEntries(t).filter((e) => e.perFile);
    for (const c of ["S101", "S311"]) assert.ok(per.some((e) => strings(e.value ?? e.line).includes(c)), `no per-file-ignores entry for ${c}`);
  });
  it(".golangci.yml: every exclusion/disable entry has a reason comment", () => {
    assert.deepEqual(golangciMissingReasons(must(".golangci.yml")), []);
  });
  it(".golangci.yml: errcheck is not disabled outside _test.go", () => {
    assert.deepEqual(golangciErrcheckOutsideTests(must(".golangci.yml")), []);
  });
  it(".golangci.yml: errcheck is excluded for _test.go", () => {
    const t = stripAll(must(".golangci.yml"));
    assert.match(t, /errcheck/, "errcheck is not excluded anywhere");
    assert.match(t, /_test\\?\.go/, "no _test.go path in the exclusions");
  });
  it(".shellcheckrc exists, disables SC1007 with a reason and no blanket disable", () => {
    const t = must(".shellcheckrc");
    assert.match(stripAll(t), /^\s*disable\s*=.*\bSC1007\b/m, "SC1007 is not disabled");
    assert.deepEqual(shellcheckrcProblems(t), []);
  });
  it("frontend/knip.json: every ignore* entry has a reason", () => {
    assert.deepEqual(jsonIgnoreProblems("frontend/knip.json", JSON.parse(must("frontend/knip.json"))), []);
  });
  it(".jscpd.json: every ignore entry has a reason", () => {
    assert.deepEqual(jsonIgnoreProblems(".jscpd.json", JSON.parse(must(".jscpd.json")), /^ignore$/), []);
  });
  it(".jscpd.json has a numeric threshold", () => {
    const j = JSON.parse(must(".jscpd.json"));
    assert.ok(typeof j.threshold === "number" && Number.isFinite(j.threshold) && j.threshold >= 0, `threshold is ${JSON.stringify(j.threshold)}, not a number`);
  });
});

describe("#1462 step 3: workflow checkouts keep no token", () => {
  const dir = join(root, ".github/workflows");
  const files = existsSync(dir) ? readdirSync(dir).filter((f) => /\.ya?ml$/.test(f)).sort() : [];
  it("there are workflows to check", () => assert.ok(files.length > 0, "no .github/workflows/*.yml"));
  for (const f of files) {
    it(`${f}: every actions/checkout has persist-credentials: false or a reasoned zizmor ignore`, () => {
      assert.deepEqual(checkoutProblems(`.github/workflows/${f}`, readFileSync(join(dir, f), "utf8")), []);
    });
  }
});

function stripAll(text) { return text.split("\n").map(stripComment).join("\n"); }

// ---- #1462: blocking scanner jobs --------------------------------------------
// A job is blocking when neither it nor any `extends:` template or `<<: *anchor`
// merge it pulls in carries `allow_failure`. Extend BLOCKING_JOBS as each
// report-only scanner (lint:ruff, lint:golangci, lint:knip, lint:jscpd) is
// promoted.
const BLOCKING_JOBS = ["lint:shellcheck", "security:zizmor"];

// Text of a top-level key of .gitlab-ci.yml without comment lines, or null.
function ciBlock(text, name) {
  const lines = text.split("\n");
  const start = lines.findIndex((l) => l.startsWith(`${name}:`) || l.startsWith(`${name} `) && l.includes(":"));
  if (start < 0) return null;
  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    if (/^[^\s#-][^\n]*:/.test(lines[i])) { end = i; break; }
  }
  return lines.slice(start, end).filter((l) => !isComment(l)).map(stripComment).join("\n");
}
// Every place `name` could get allow_failure from: its own block, then each
// extended template and merged anchor, recursively. Returns offending names.
function allowFailureSources(text, name, seen = new Set()) {
  if (seen.has(name)) return [];
  seen.add(name);
  const b = ciBlock(text, name);
  assert.ok(b, `no job or template ${name} in .gitlab-ci.yml`);
  const out = /^\s*allow_failure:/m.test(b) ? [name] : [];
  const refs = [];
  for (const m of b.matchAll(/^\s*extends:\s*(.+)$/gm)) {
    for (const r of m[1].replace(/[\[\]]/g, "").split(",")) if (r.trim()) refs.push(r.trim());
  }
  for (const m of b.matchAll(/^\s*extends:\s*\n((?:\s+-\s*\S+\n?)+)/gm)) {
    for (const r of m[1].matchAll(/-\s*(\S+)/g)) refs.push(r[1]);
  }
  for (const m of b.matchAll(/<<:\s*(?:\[([^\]]*)\]|(\*\S+))/g)) {
    for (const a of (m[1] ?? m[2]).split(",")) {
      const anchor = a.trim().replace(/^\*/, "");
      if (!anchor) continue;
      // anchor names are defined as `.template: &anchor`
      const def = text.split("\n").find((l) => new RegExp(`^\\S[^#]*:\\s*&${anchor}\\b`).test(l));
      if (def) refs.push(def.slice(0, def.indexOf(":")));
    }
  }
  for (const r of refs) out.push(...allowFailureSources(text, r, seen));
  return out;
}

describe("#1462: scanner jobs that are blocking carry no allow_failure", () => {
  const ci = readFileSync(join(root, ".gitlab-ci.yml"), "utf8");
  it("the helper sees allow_failure through extends and anchors", () => {
    const fx = ".t: &t\n  allow_failure: true\nA:\n  script: x\nB:\n  extends: .t\nC:\n  <<: *t\nD:\n  # allow_failure: true\n  script: x\n";
    assert.deepEqual(allowFailureSources(fx, "A"), []);
    assert.deepEqual(allowFailureSources(fx, "B"), [".t"]);
    assert.deepEqual(allowFailureSources(fx, "C"), [".t"]);
    assert.deepEqual(allowFailureSources(fx, "D"), []);
  });
  for (const name of BLOCKING_JOBS) {
    it(`${name} has no allow_failure`, () => {
      assert.deepEqual(allowFailureSources(ci, name), [], `${name} is still report-only (allow_failure)`);
    });
  }
});
