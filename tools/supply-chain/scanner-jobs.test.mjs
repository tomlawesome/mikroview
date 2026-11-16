// Tests for #1462 steps 4-7 and #1463: the six approved scanners (shellcheck,
// golangci-lint, ruff, zizmor, knip, jscpd) as report-only CI jobs, each
// pinned. Run with `node --test tools/supply-chain/scanner-jobs.test.mjs`.
// They read the real .gitlab-ci.yml as text, same node:test style as
// trivy-scans.test.mjs. No git and no Python: CI's node image has neither.
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";
import assert from "node:assert/strict";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (p) => readFileSync(join(root, p), "utf8");
const ci = read(".gitlab-ci.yml");

// Text of a top-level key, from `name:` to the next top-level key. With
// comments=false the comment lines are dropped.
function block(name, comments = false) {
  const lines = ci.split("\n");
  const start = lines.findIndex((l) => l.startsWith(`${name}:`));
  if (start < 0) return null;
  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    if (/^[^\s#-][^\n]*:/.test(lines[i])) { end = i; break; }
  }
  const body = lines.slice(start, end);
  return (comments ? body : body.filter((l) => !/^\s*#/.test(l))).join("\n");
}
function job(name, comments = false) {
  const b = block(name, comments);
  assert.ok(b, `no job ${name} in .gitlab-ci.yml`);
  return b;
}
// The lines of a job's second-level key (`  artifacts:` and its children).
function section(text, key) {
  const lines = text.split("\n");
  const at = lines.findIndex((l) => new RegExp(`^ {2}${key}:`).test(l));
  if (at < 0) return "";
  let end = lines.length;
  for (let i = at + 1; i < lines.length; i++) {
    if (/^ {2}\S/.test(lines[i])) { end = i; break; }
  }
  return lines.slice(at, end).join("\n");
}

const jobs = [
  { name: "lint:shellcheck", stage: "lint", kind: "binary", tool: "shellcheck" },
  { name: "lint:golangci", stage: "lint", kind: "binary", tool: "golangci-lint" },
  { name: "lint:ruff", stage: "lint", kind: "binary", tool: "ruff" },
  { name: "lint:knip", stage: "lint", kind: "npm", tool: "knip" },
  { name: "lint:jscpd", stage: "lint", kind: "npm", tool: "jscpd" },
  { name: "security:zizmor", stage: "security", kind: "binary", tool: "zizmor" },
];

describe("#1462 steps 4-7: each scanner job exists in its stage", () => {
  for (const j of jobs) {
    it(`${j.name} exists in stage ${j.stage}, with the usual rules, tags and needs`, () => {
      const t = job(j.name);
      assert.match(t, new RegExp(`^ {2}stage: ${j.stage}\\s*$`, "m"), `${j.name} is not in stage ${j.stage}`);
      assert.match(t, /^ {2}<<:.*\*dev_or_mr/m, `${j.name} does not use a .dev_or_mr* rules anchor`);
      assert.match(t, /^ {2}tags:/m, `${j.name} has no runner tags`);
      assert.match(t, /^ {2}needs: \[\]\s*$/m, `${j.name} lacks needs: []`);
      assert.doesNotMatch(t, /^ {2}interruptible: false/m, `${j.name} is not interruptible`);
    });
  }
});

describe("#1462 steps 4-7: each scanner is pinned, never a bare tag", () => {
  const pkg = existsSync(join(root, "frontend/package.json")) ? JSON.parse(read("frontend/package.json")) : {};
  const lock = existsSync(join(root, "frontend/package-lock.json")) ? JSON.parse(read("frontend/package-lock.json")) : {};
  for (const j of jobs) {
    it(`${j.name} pins ${j.tool}`, () => {
      const t = job(j.name);
      if (j.kind === "binary") {
        const image = (t.match(/^ {2}image:\s*(\S+)/m) ?? [])[1] ?? "";
        const digest = /@sha256:[0-9a-f]{64}$/.test(image);
        const checksum = /sha256sum -c\b/.test(t);
        assert.ok(digest || checksum, `${j.name}: image ${image || "(none)"} has no @sha256:<64 hex> and the script has no sha256sum -c`);
      } else {
        const v = pkg.devDependencies?.[j.tool];
        assert.ok(v, `${j.tool} is not a devDependency in frontend/package.json`);
        assert.match(v, /^\d+\.\d+\.\d+$/, `${j.tool} version "${v}" is not exact (no ^ or ~)`);
        const locked = lock.packages?.[`node_modules/${j.tool}`]?.version;
        assert.equal(locked, v, `${j.tool} is not in frontend/package-lock.json at ${v}`);
        assert.match(t, /\bnpm ci\b/, `${j.name} does not install from the lockfile with npm ci`);
      }
    });
  }
});

// Jobs that reached zero findings and became blocking; scanner-config.test.mjs
// checks they carry no allow_failure. They still keep their report.
const blocking = new Set(["lint:shellcheck", "security:zizmor"]);

describe("#1462 steps 4-7: report-only jobs keep a report as an artifact", () => {
  for (const j of jobs) {
    it(`${j.name} keeps a when: always artifact, and is report-only until it is blocking`, () => {
      const t = job(j.name, true);
      if (blocking.has(j.name)) {
        // blocking: no report-only marking to check
      } else if (j.name === "lint:jscpd") {
        // A bad registry signature must fail the job: only jscpd's own
        // duplication exit code (3) is tolerated.
        assert.doesNotMatch(t, /^ {2}allow_failure: true\s*$/m, "lint:jscpd must not be allow_failure: true");
        assert.match(section(job(j.name), "allow_failure"), /exit_codes:\s*(\[\s*3\s*\]|\n\s*-\s*3\s*$)/m, "lint:jscpd needs allow_failure: { exit_codes: [3] }");
      } else {
        assert.match(t, /^ {2}allow_failure: true\s*$/m, `${j.name} lacks allow_failure: true`);
      }
      if (!blocking.has(j.name)) {
        assert.match(t, /# report-only until one pipeline's noise is triaged \(#1462\)/, `${j.name} lacks the report-only comment`);
      }
      const a = section(job(j.name), "artifacts");
      assert.ok(a, `${j.name} has no artifacts entry`);
      assert.match(a, /\bwhen: always\b/, `${j.name} artifacts lack when: always`);
      assert.match(a, /\bexpire_in: 30 days\b/, `${j.name} artifacts lack expire_in: 30 days`);
      assert.match(a, /\bpaths:/, `${j.name} artifacts name no report path`);
    });
  }
});

describe("#1463: lint:jscpd checks registry signatures and provenance", () => {
  it("runs npm audit signatures after npm ci, not guarded by || true or similar", () => {
    const t = job("lint:jscpd");
    const lines = t.split("\n");
    const audit = lines.filter((l) => /npm audit signatures/.test(l));
    assert.ok(audit.length > 0, "lint:jscpd does not run `npm audit signatures`");
    for (const l of audit) {
      assert.doesNotMatch(l, /\|\||;\s*(true|:|exit 0)\b|&\s*$/, `guarded: ${l.trim()}`);
    }
    assert.doesNotMatch(t, /set \+e/, "lint:jscpd turns off errexit with set +e");
    const ciAt = lines.findIndex((l) => /\bnpm ci\b/.test(l));
    const auditAt = lines.findIndex((l) => /npm audit signatures/.test(l));
    assert.ok(ciAt >= 0 && ciAt < auditAt, "npm audit signatures must come after npm ci");
  });
});

describe("#1462: actionlint is not adopted", () => {
  it("no job or file mentions actionlint", () => {
    const word = "action" + "lint";
    const self = fileURLToPath(import.meta.url);
    const hits = [];
    const walk = (dir) => {
      for (const e of readdirSync(dir)) {
        if (e === ".git" || e === "node_modules") continue;
        const p = join(dir, e);
        const s = statSync(p);
        if (s.isDirectory()) walk(p);
        else if (p !== self && s.size < 5_000_000) {
          const text = readFileSync(p, "utf8");
          if (text.toLowerCase().includes(word)) hits.push(p.slice(root.length + 1));
        }
      }
    };
    walk(root);
    assert.deepEqual(hits, [], `${word} is mentioned in: ${hits.join(", ")}`);
  });
});

describe("#1462: scanner config files", () => {
  it(".golangci.yml enables dupl and does not disable unused", () => {
    const p = join(root, ".golangci.yml");
    assert.ok(existsSync(p), "no .golangci.yml at the root");
    const y = readFileSync(p, "utf8").split("\n").filter((l) => !/^\s*#/.test(l)).join("\n");
    assert.match(y, /^\s*-\s*dupl\s*$/m, "dupl is not listed as enabled");
    assert.doesNotMatch(y, /disable:\s*\n(\s*-\s*\S+\s*\n)*?\s*-\s*unused\b/, "unused is disabled");
  });
  it("ruff.toml selects S", () => {
    const p = join(root, "ruff.toml");
    assert.ok(existsSync(p), "no ruff.toml at the root");
    const y = readFileSync(p, "utf8").split("\n").filter((l) => !/^\s*#/.test(l)).join("\n");
    assert.match(y, /^\s*(extend-)?select\s*=\s*\[[^\]]*["']S["']/m, 'ruff.toml does not select "S"');
  });
});
