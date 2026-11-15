// Test for #1479: no job in any .github/workflows/*.yml that can mint an OIDC
// token (`id-token: write`, so it signs and publishes) may turn a cache on or
// pull a build cache: a poisoned cache would be signed as ours. Run with
// `node --test 'tools/supply-chain/*.test.mjs'`. Workflows are read as text
// (no YAML library, no git); the line parser is itself tested on fixtures
// that must be flagged, one per spelling.
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";
import assert from "node:assert/strict";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const wfDir = join(root, ".github/workflows");

const clean = (text) =>
  text.split("\n").filter((l) => !/^\s*#/.test(l)).map((l) => l.replace(/\s+#.*$/, ""));
const unquote = (s) => s.replace(/["']/g, "");

// Does a `permissions` value (inline text plus child lines) grant an OIDC token?
function grants(inline, children) {
  const text = unquote([inline, ...children].join("\n"));
  return /(^|[\s{,])id-token:\s*write(?=[\s,}]|$)/.test(text) || /^\s*write-all\s*$/.test(inline ? unquote(inline) : "");
}

// Returns { anchors, jobs: [{ name, signing, reasons }] } for workflow text.
export function analyze(text) {
  const lines = clean(text);
  const anchors = lines.some((l) => /(^|\s)&[\w-]+/.test(l) || /(:|-)\s+\*[A-Za-z_]/.test(l) || /<<\s*:/.test(l));

  // Workflow-level permissions (top-level key).
  const top = lines.findIndex((l) => /^["']?permissions["']?:/.test(l));
  let wfGrants = false;
  if (top >= 0) {
    const kids = [];
    for (let i = top + 1; i < lines.length && /^\s+\S/.test(lines[i]); i++) kids.push(lines[i]);
    wfGrants = grants(lines[top].replace(/^["']?permissions["']?:\s*/, ""), kids);
  }

  const at = lines.findIndex((l) => /^jobs:\s*$/.test(l));
  const raw = [];
  for (let i = at + 1; at >= 0 && i < lines.length; i++) {
    if (/^\S/.test(lines[i])) break;
    const m = /^ {2}["']?([\w-]+)["']?:\s*$/.exec(lines[i]);
    if (m) raw.push({ name: m[1], lines: [] });
    else if (raw.length) raw[raw.length - 1].lines.push(lines[i]);
  }

  const jobs = raw.map((j) => {
    const p = j.lines.findIndex((l) => /^ {4}["']?permissions["']?:/.test(l));
    let signing = wfGrants;
    if (p >= 0) {
      const kids = [];
      for (let i = p + 1; i < j.lines.length && /^ {5,}\S/.test(j.lines[i]); i++) kids.push(j.lines[i]);
      signing = grants(j.lines[p].replace(/^ {4}["']?permissions["']?:\s*/, ""), kids);
    }
    const s = j.lines.findIndex((l) => /^ {4}["']?steps["']?:/.test(l));
    const steps = [];
    for (const l of s >= 0 ? j.lines.slice(s + 1) : []) {
      if (/^\s*- /.test(l) && (!steps.length || /^ {6}- /.test(l))) steps.push([l]);
      else if (steps.length) steps[steps.length - 1].push(l);
    }
    const reasons = steps.map((st) => cacheReason(unquote(st.join("\n")))).filter(Boolean);
    return { name: j.name, signing, reasons };
  });
  return { anchors, jobs };
}

// Value of a step input (text already unquoted), or null when absent.
const input = (step, key) => {
  const m = new RegExp(`(?:^|[\\s{,])${key}:[ \\t]*([^\\s,}]*)`).exec(step);
  return m ? m[1] : null;
};

function cacheReason(step) {
  const uses = /(?:^|[\s{,-])uses:\s*([^\s,}]+)/.exec(step)?.[1] ?? "";
  const action = uses.split("@")[0];
  if (action === "actions/cache" || action.startsWith("actions/cache/"))
    return `${action} restores a cache`;
  if (action === "actions/setup-go" && input(step, "cache") !== "false")
    return "actions/setup-go caches by default; it needs `cache: false`";
  if (action === "actions/setup-node")
    for (const k of ["cache", "cache-dependency-path"])
      if (input(step, k) !== null) return `actions/setup-node sets \`${k}\``;
  for (const k of ["cache-from", "cache-to"])
    if (input(step, k) !== null) return `${action || "step"} sets \`${k}\` (a build cache)`;
  return null;
}

// Flagged jobs of a fixture: the names of signing jobs with a reason.
const flagged = (text) => analyze(text).jobs.filter((j) => j.signing && j.reasons.length).map((j) => j.name);
const wf = (perm, step, jobPerm = perm) =>
  `on: push\n${perm.top ?? ""}jobs:\n  a:\n    runs-on: x\n${jobPerm.job ?? ""}    steps:\n${step}\n`;
const GO = "      - uses: actions/setup-go@v1\n";
const BLOCK = { job: "    permissions:\n      id-token: write\n" };

describe("#1479: the parser flags every spelling (fixtures)", () => {
  const cases = {
    "plain id-token: write": [wf(BLOCK, GO)],
    "quoted 'write'": [wf({ job: "    permissions:\n      id-token: 'write'\n" }, GO)],
    'quoted "write" and key': [wf({ job: '    permissions:\n      "id-token": "write"\n' }, GO)],
    "flow style permissions": [wf({ job: "    permissions: {contents: read, id-token: write}\n" }, GO)],
    "write-all": [wf({ job: "    permissions: write-all\n" }, GO)],
    "workflow-level, inherited": [wf({ top: "permissions:\n  id-token: write\n", job: "" }, GO)],
    "workflow-level write-all, inherited": [wf({ top: "permissions: write-all\n", job: "" }, GO)],
    "setup-go cache: true": [wf(BLOCK, GO + "        with:\n          cache: true\n")],
    "setup-node cache": [wf(BLOCK, "      - uses: actions/setup-node@v1\n        with:\n          cache: npm\n")],
    "setup-node cache-dependency-path": [wf(BLOCK, "      - uses: actions/setup-node@v1\n        with:\n          cache-dependency-path: x\n")],
    "setup-node quoted key": [wf(BLOCK, '      - uses: actions/setup-node@v1\n        with:\n          "cache": npm\n')],
    "flow-style with": [wf(BLOCK, "      - uses: actions/setup-node@v1\n        with: {cache: npm}\n")],
    "quoted uses": [wf(BLOCK, '      - uses: "actions/setup-go@v1"\n')],
    "actions/cache": [wf(BLOCK, "      - uses: actions/cache@v1\n")],
    "actions/cache/restore": [wf(BLOCK, "      - uses: actions/cache/restore@v1\n")],
    "cache-from": [wf(BLOCK, "      - uses: docker/build-push-action@v1\n        with:\n          cache-from: type=gha\n")],
    "cache-to": [wf(BLOCK, "      - uses: docker/build-push-action@v1\n        with:\n          cache-to: type=gha\n")],
    "flow-style step": [wf(BLOCK, "      - {uses: actions/setup-node@v1, with: {cache: npm}}\n")],
  };
  for (const [name, [text]] of Object.entries(cases))
    it(`flags: ${name}`, () => assert.deepEqual(flagged(text), ["a"]));

  it("passes a clean signing job and an unprivileged caching job", () => {
    assert.deepEqual(flagged(wf(BLOCK, GO + "        with:\n          cache: false\n")), []);
    assert.deepEqual(flagged(wf({ job: "    permissions:\n      contents: read\n" }, "      - uses: actions/setup-go@v1\n")), []);
  });
  it("a job's own permissions override the workflow's", () => {
    const t = wf({ top: "permissions:\n  id-token: write\n", job: "    permissions:\n      contents: read\n" }, GO);
    assert.deepEqual(flagged(t), []);
  });
  it("detects anchors, aliases and merge keys", () => {
    for (const t of ["a: &x 1\n", "a: *x\n", "  <<: *x\n"]) assert.equal(analyze(t).anchors, true);
    assert.equal(analyze("a: 1 # *docs\n").anchors, false);
  });
});

describe("#1479: no signing job in any workflow turns a cache on", () => {
  const files = readdirSync(wfDir).filter((f) => /\.ya?ml$/.test(f)).sort();
  const result = Object.fromEntries(files.map((f) => [f, analyze(readFileSync(join(wfDir, f), "utf8"))]));

  for (const f of files)
    it(`${f}: can be checked (no YAML anchors, aliases or merge keys)`, () =>
      assert.equal(result[f].anchors, false, `cannot check: anchors in ${f}`));

  for (const f of ["docker.yml", "live-check-image.yml"])
    it(`${f}: has at least one job holding id-token: write`, () =>
      assert.ok(result[f].jobs.some((j) => j.signing), `no id-token: write job found in ${f}`));

  for (const f of files)
    for (const j of result[f].jobs.filter((x) => x.signing))
      it(`${f} job ${j.name} (id-token: write) turns no cache on`, () =>
        assert.deepEqual(j.reasons, [], `${f} job ${j.name} turns a cache on: ${j.reasons.join("; ")}`));
});
