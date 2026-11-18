// Tests for npm-audit.mjs (#1505). Run with `node --test
// tools/supply-chain/npm-audit.test.mjs`. The script is run as a child process
// on fixture files; "today" is set through NPM_AUDIT_TODAY.
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { after, describe, it } from "node:test";
import assert from "node:assert/strict";

const here = dirname(fileURLToPath(import.meta.url));
const script = join(here, "npm-audit.mjs");
const realExemptions = resolve(here, "..", "..", "supply-chain", "npm-audit-exemptions.json");

const scratchDirs = [];
function scratchDir() {
  const dir = mkdtempSync(join(tmpdir(), "mikroview-npm-audit-"));
  scratchDirs.push(dir);
  return dir;
}
after(() => {
  for (const dir of scratchDirs) rmSync(dir, { recursive: true, force: true });
});

const GHSA_A = "GHSA-aaaa-aaaa-aaaa";
const GHSA_B = "GHSA-bbbb-bbbb-bbbb";
const GHSA_C = "GHSA-cccc-cccc-cccc";
const REAL = "GHSA-fc5q-39xc-vjpf";

function advisory(id, name, severity, source = 1000) {
  return {
    source,
    name,
    title: `Problem in ${name}`,
    url: `https://github.com/advisories/${id}`,
    severity,
    range: "*",
  };
}
function vuln(name, severity, via) {
  return { name, severity, isDirect: false, via, effects: [], range: "*", nodes: [], fixAvailable: false };
}
function report(vulnerabilities) {
  return { auditReportVersion: 2, vulnerabilities, metadata: { vulnerabilities: {} } };
}
function exemption(id, pkg, expires = "2026-12-17", extra = {}) {
  return { id, package: pkg, reason: "no fixed release, build tool only", expires, ...extra };
}

// Writes the fixtures and runs the script; today defaults to a fixed date.
function run(auditContent, exemptionsContent, { today = "2026-11-17", rawAudit, rawExemptions } = {}) {
  const dir = scratchDir();
  const auditPath = join(dir, "audit.json");
  if (rawAudit !== null) {
    writeFileSync(auditPath, rawAudit ?? JSON.stringify(auditContent));
  }
  const args = [script, auditPath];
  if (exemptionsContent !== undefined || rawExemptions !== undefined) {
    const exPath = join(dir, "exemptions.json");
    writeFileSync(exPath, rawExemptions ?? JSON.stringify(exemptionsContent));
    args.push(exPath);
  }
  const r = spawnSync(process.execPath, args, {
    encoding: "utf8",
    env: { ...process.env, NPM_AUDIT_TODAY: today },
  });
  return { status: r.status, out: `${r.stdout ?? ""}${r.stderr ?? ""}`, error: r.error };
}

function highReport(id = GHSA_A, pkg = "foo", severity = "high") {
  return report({ [pkg]: vuln(pkg, severity, [advisory(id, pkg, severity)]) });
}

describe("advisories", () => {
  it("passes a clean report", () => {
    const r = run(report({}), []);
    assert.equal(r.status, 0, r.out);
  });

  it("fails an unexempted high advisory and names it", () => {
    const r = run(highReport(), []);
    assert.notEqual(r.status, 0);
    assert.match(r.out, new RegExp(GHSA_A));
    assert.match(r.out, /foo/);
  });

  it("fails an unexempted critical advisory and names it", () => {
    const r = run(highReport(GHSA_B, "bar", "critical"), []);
    assert.notEqual(r.status, 0);
    assert.match(r.out, new RegExp(GHSA_B));
  });

  it("ignores moderate and low advisories", () => {
    const r = run(
      report({
        m: vuln("m", "moderate", [advisory(GHSA_A, "m", "moderate")]),
        l: vuln("l", "low", [advisory(GHSA_B, "l", "low")]),
      }),
      [],
    );
    assert.equal(r.status, 0, r.out);
  });

  it("does not count a transitive package whose via is only strings as an advisory", () => {
    const r = run(
      report({
        parent: vuln("parent", "high", ["child"]),
        child: vuln("child", "moderate", [advisory(GHSA_A, "child", "moderate")]),
      }),
      [],
    );
    assert.equal(r.status, 0, r.out);
  });

  it("reports one advisory reached through several packages once", () => {
    const r = run(
      report({
        a: vuln("a", "high", [advisory(GHSA_A, "shared", "high")]),
        b: vuln("b", "high", [advisory(GHSA_A, "shared", "high")]),
        c: vuln("c", "high", ["a", advisory(GHSA_A, "shared", "high")]),
      }),
      [],
    );
    assert.notEqual(r.status, 0);
    const count = r.out.split(GHSA_A).length - 1;
    assert.equal(count, 1, `expected ${GHSA_A} once, output:\n${r.out}`);
  });

  it("covers one advisory seen through several packages with a single exemption", () => {
    const r = run(
      report({
        a: vuln("a", "high", [advisory(GHSA_A, "shared", "high")]),
        b: vuln("b", "high", [advisory(GHSA_A, "shared", "high")]),
      }),
      [exemption(GHSA_A, "shared")],
    );
    assert.equal(r.status, 0, r.out);
  });
});

describe("exemptions in use", () => {
  it("passes an exempted high advisory and lists the exemption with its expiry", () => {
    const r = run(highReport(), [exemption(GHSA_A, "foo", "2026-12-17")]);
    assert.equal(r.status, 0, r.out);
    assert.match(r.out, new RegExp(GHSA_A));
    assert.match(r.out, /2026-12-17/);
  });

  it("only exempts the advisory it names", () => {
    const r = run(
      report({
        foo: vuln("foo", "high", [advisory(GHSA_A, "foo", "high")]),
        bar: vuln("bar", "high", [advisory(GHSA_B, "bar", "high")]),
      }),
      [exemption(GHSA_A, "foo")],
    );
    assert.notEqual(r.status, 0);
    assert.match(r.out, new RegExp(GHSA_B));
  });

  it("passes on the expires day itself", () => {
    const r = run(highReport(), [exemption(GHSA_A, "foo", "2026-12-17")], { today: "2026-12-17" });
    assert.equal(r.status, 0, r.out);
  });

  it("fails the day after expires and names the exemption", () => {
    const r = run(highReport(), [exemption(GHSA_A, "foo", "2026-12-17")], { today: "2026-12-18" });
    assert.notEqual(r.status, 0);
    assert.match(r.out, new RegExp(GHSA_A));
    assert.match(r.out, /expired/i);
  });

  it("fails a stale exemption that matches no advisory in the report", () => {
    const r = run(highReport(GHSA_A, "foo"), [exemption(GHSA_A, "foo"), exemption(GHSA_C, "gone")]);
    assert.notEqual(r.status, 0);
    assert.match(r.out, new RegExp(GHSA_C));
    assert.match(r.out, /stale|no advisory|unused|matches no/i);
  });

  it("treats an exemption as stale when the report is clean", () => {
    const r = run(report({}), [exemption(GHSA_A, "foo")]);
    assert.notEqual(r.status, 0);
    assert.match(r.out, new RegExp(GHSA_A));
  });
});

describe("invalid exemptions file", () => {
  // Every case pairs an advisory with an exemption that would cover it were it
  // accepted, and the verdict is read from the problem the validation itself
  // prints, so a case cannot pass on an unrelated failure.
  function rejected(exemptionsContent, message, options) {
    const r = run(highReport(), exemptionsContent, options);
    assert.equal(r.status, 1, r.out);
    assert.match(r.out, message);
    assert.doesNotMatch(r.out, /has no exemption|expired on|stale exemption/);
  }

  it("accepts the same exemption when it is valid", () => {
    const r = run(highReport(), [exemption(GHSA_A, "foo")]);
    assert.equal(r.status, 0, r.out);
  });

  for (const field of ["id", "package", "reason", "expires"]) {
    it(`fails when ${field} is missing`, () => {
      const e = exemption(GHSA_A, "foo");
      delete e[field];
      rejected([e], new RegExp(`field "${field}" is missing or empty`));
    });

    it(`fails when ${field} is empty`, () => {
      const e = exemption(GHSA_A, "foo");
      e[field] = "  ";
      rejected([e], new RegExp(`field "${field}" is missing or empty`));
    });
  }

  it("fails on an impossible calendar date that is still in the future", () => {
    rejected([exemption(GHSA_A, "foo", "2027-02-30")], /"expires" is not a valid YYYY-MM-DD date: 2027-02-30/);
  });

  it("fails on a malformed date that sorts after today", () => {
    rejected([exemption(GHSA_A, "foo", "30/12/2027")], /"expires" is not a valid YYYY-MM-DD date: 30\/12\/2027/);
  });

  it("fails on a duplicate id", () => {
    rejected([exemption(GHSA_A, "foo"), exemption(GHSA_A, "foo")], new RegExp(`duplicate id ${GHSA_A}`));
  });

  it("fails when the exemptions file is not valid JSON", () => {
    rejected(undefined, /is unreadable or not JSON/, { rawExemptions: "not json {" });
  });

  it("fails when the exemptions file is not an array", () => {
    rejected({ id: GHSA_A }, /must be a JSON array/);
  });
});

describe("invalid audit input", () => {
  it("fails when the audit file is not JSON", () => {
    const r = run(null, [], { rawAudit: "this is not json" });
    assert.notEqual(r.status, 0);
  });

  it("fails on an npm error object", () => {
    const r = run({ error: { code: "ENOAUDIT", summary: "audit endpoint returned an error", detail: "x" } }, []);
    assert.notEqual(r.status, 0);
    assert.match(r.out, /ENOAUDIT|error|audit/i);
  });

  it("fails when vulnerabilities is missing", () => {
    const r = run({ auditReportVersion: 2, metadata: {} }, []);
    assert.notEqual(r.status, 0);
    assert.match(r.out, /vulnerabilities/);
  });

  it("fails when the audit file does not exist", () => {
    const r = run(null, [], { rawAudit: null });
    assert.notEqual(r.status, 0);
  });
});

describe("real exemptions file", () => {
  // Emptied 2026-11-18 (#1537): GHSA-fc5q-39xc-vjpf (picomatch) left npm's
  // audit report, so its exemption went stale and the check asked for it to go.
  function runReal(vulnerabilities) {
    const dir = scratchDir();
    const auditPath = join(dir, "audit.json");
    writeFileSync(auditPath, JSON.stringify(report(vulnerabilities)));
    const r = spawnSync(process.execPath, [script, auditPath, realExemptions], { encoding: "utf8" });
    return { status: r.status, out: `${r.stdout ?? ""}${r.stderr ?? ""}` };
  }

  it("is valid and passes a clean report", () => {
    const r = runReal({});
    assert.equal(r.status, 0, r.out);
  });

  it("no longer excuses the picomatch advisory", () => {
    const r = runReal({ picomatch: vuln("picomatch", "high", [advisory(REAL, "picomatch", "high", 1116000)]) });
    assert.notEqual(r.status, 0);
    assert.match(r.out, new RegExp(REAL));
  });
});
