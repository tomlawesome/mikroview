// Judges `npm audit --json` output against supply-chain/npm-audit-exemptions.json
// (#1505).
//
// Usage: node tools/supply-chain/npm-audit.mjs <audit.json> [exemptions.json]
//
// `npm audit --audit-level=high` cannot express "accepted until a date", so CI
// writes the JSON report to a file and this script decides pass or fail. It
// fails on: a high or critical advisory with no unexpired exemption; an expired
// exemption; an exemption matching no advisory in the report (stale); an
// invalid exemptions file; or an audit file that is not an npm audit report.
// Moderate and low advisories are ignored, as with --audit-level=high.
//
// Advisories are the object entries in each package's `via`, identified by the
// GHSA ID at the end of their `url`; string entries are transitive links, not
// advisories. "Today" is the UTC date unless NPM_AUDIT_TODAY=YYYY-MM-DD is set.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repositoryRoot = fileURLToPath(new URL("../../", import.meta.url));
const FIELDS = ["id", "package", "reason", "expires"];
const BLOCKING = new Set(["high", "critical"]);

function validDate(value) {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const d = new Date(`${value}T00:00:00Z`);
  return !Number.isNaN(d.getTime()) && d.toISOString().slice(0, 10) === value;
}

export function today(env = process.env) {
  const override = env.NPM_AUDIT_TODAY;
  if (override !== undefined && override !== "") {
    if (!validDate(override)) throw new Error(`NPM_AUDIT_TODAY is not a valid YYYY-MM-DD date: ${override}`);
    return override;
  }
  return new Date().toISOString().slice(0, 10);
}

export function loadExemptions(path) {
  const problems = [];
  let data;
  try {
    data = JSON.parse(readFileSync(path, "utf8"));
  } catch (err) {
    return { exemptions: [], problems: [`exemptions file ${path} is unreadable or not JSON: ${err.message}`] };
  }
  if (!Array.isArray(data)) {
    return { exemptions: [], problems: [`exemptions file ${path} must be a JSON array`] };
  }
  const seen = new Set();
  const exemptions = [];
  data.forEach((entry, i) => {
    const label = `exemption #${i + 1}`;
    if (entry === null || typeof entry !== "object" || Array.isArray(entry)) {
      problems.push(`${label} is not an object`);
      return;
    }
    let ok = true;
    for (const field of FIELDS) {
      if (typeof entry[field] !== "string" || entry[field].trim() === "") {
        problems.push(`${label} (${entry.id ?? "no id"}): field "${field}" is missing or empty`);
        ok = false;
      }
    }
    if (ok && !validDate(entry.expires)) {
      problems.push(`${label} (${entry.id}): "expires" is not a valid YYYY-MM-DD date: ${entry.expires}`);
      ok = false;
    }
    if (ok && seen.has(entry.id)) {
      problems.push(`${label}: duplicate id ${entry.id}`);
      ok = false;
    }
    if (typeof entry.id === "string") seen.add(entry.id);
    if (ok) exemptions.push(entry);
  });
  return { exemptions, problems };
}

// Returns Map<ghsaId, { id, packages:Set, severity, title }> or throws.
export function collectAdvisories(path) {
  let report;
  try {
    report = JSON.parse(readFileSync(path, "utf8"));
  } catch (err) {
    throw new Error(`audit file ${path} is unreadable or not JSON: ${err.message}`);
  }
  if (report === null || typeof report !== "object" || Array.isArray(report)) {
    throw new Error(`audit file ${path} is not an npm audit report`);
  }
  if (report.error !== undefined) {
    const e = report.error;
    const text = e && typeof e === "object" ? [e.code, e.summary, e.detail].filter(Boolean).join(": ") : String(e);
    throw new Error(`npm audit reported an error: ${text || "unknown"}`);
  }
  const vulns = report.vulnerabilities;
  if (vulns === null || typeof vulns !== "object" || Array.isArray(vulns)) {
    throw new Error(`audit file ${path} has no "vulnerabilities" object, so it is not an npm audit report`);
  }
  const advisories = new Map();
  for (const [name, v] of Object.entries(vulns)) {
    for (const via of Array.isArray(v?.via) ? v.via : []) {
      if (via === null || typeof via !== "object") continue;
      const m = typeof via.url === "string" ? via.url.match(/(GHSA-[^/\s]+)\/?$/i) : null;
      const id = m ? m[1] : (via.url ?? `source-${via.source}`);
      const entry = advisories.get(id) ?? { id, packages: new Set(), severity: via.severity, title: via.title };
      entry.packages.add(via.name ?? name);
      if (BLOCKING.has(via.severity)) entry.severity = via.severity;
      advisories.set(id, entry);
    }
  }
  return advisories;
}

export function judge(advisories, exemptions, now) {
  const problems = [];
  const used = [];
  const byId = new Map(exemptions.map((e) => [e.id, e]));
  for (const adv of advisories.values()) {
    if (!BLOCKING.has(adv.severity)) continue;
    const ex = byId.get(adv.id);
    if (!ex) {
      problems.push(`${adv.severity} advisory ${adv.id} in ${[...adv.packages].join(", ")} has no exemption: ${adv.title ?? ""}`.trimEnd());
    }
  }
  for (const ex of exemptions) {
    if (!advisories.has(ex.id)) {
      problems.push(`stale exemption ${ex.id} (${ex.package}) matches no advisory in the report: delete it`);
    } else if (now > ex.expires) {
      problems.push(`exemption ${ex.id} (${ex.package}) expired on ${ex.expires}`);
    } else {
      used.push(ex);
    }
  }
  return { problems, used };
}

export function main(argv, env = process.env) {
  const [auditPath, exemptionsArg] = argv;
  if (!auditPath) {
    console.error("usage: node tools/supply-chain/npm-audit.mjs <audit.json> [exemptions.json]");
    return 2;
  }
  const exemptionsPath = exemptionsArg ?? resolve(repositoryRoot, "supply-chain", "npm-audit-exemptions.json");
  let now;
  try {
    now = today(env);
  } catch (err) {
    console.error(err.message);
    return 2;
  }
  const loaded = loadExemptions(exemptionsPath);
  let advisories;
  const problems = [...loaded.problems];
  try {
    advisories = collectAdvisories(auditPath);
  } catch (err) {
    problems.push(err.message);
  }
  let used = [];
  if (advisories && loaded.problems.length === 0) {
    const verdict = judge(advisories, loaded.exemptions, now);
    problems.push(...verdict.problems);
    used = verdict.used;
  }
  if (problems.length > 0) {
    console.error(`npm audit check failed (${problems.length} problem${problems.length === 1 ? "" : "s"}):`);
    for (const p of problems) console.error(`  - ${p}`);
    return 1;
  }
  console.log(`npm audit check passed (today ${now}).`);
  for (const ex of used) {
    console.log(`  exempt ${ex.id} (${ex.package}) until ${ex.expires}: ${ex.reason}`);
  }
  return 0;
}

const isMain = process.argv[1] !== undefined && resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) process.exitCode = main(process.argv.slice(2));
