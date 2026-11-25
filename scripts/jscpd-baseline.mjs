// #1463: compare a jscpd JSON report with the committed clone baseline.
// A clone not in the baseline fails (new duplication); a baseline entry that
// matches no clone fails (stale: delete it, the baseline only shrinks).
// A clone is keyed by its two files plus its size in lines and tokens, not by
// line numbers, so unrelated edits above it do not churn the baseline.
// Usage: node scripts/jscpd-baseline.mjs <report.json> <baseline.txt>
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

export function cloneKey(d) {
  const files = [d.firstFile.name, d.secondFile.name].sort().join(" ~ ");
  return `${files} | ${d.lines} lines | ${d.tokens} tokens`;
}

export function parseBaseline(text) {
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l !== "" && !l.startsWith("#"));
}

// Returns { added, stale }: keys in the report but not the baseline, and
// baseline keys with no clone. Duplicate keys are counted, not collapsed.
export function compare(report, baselineLines) {
  const left = new Map();
  for (const k of baselineLines) left.set(k, (left.get(k) ?? 0) + 1);
  const added = [];
  for (const d of report.duplicates) {
    const k = cloneKey(d);
    const n = left.get(k) ?? 0;
    if (n > 0) left.set(k, n - 1);
    else added.push(k);
  }
  const stale = [];
  for (const [k, n] of left) for (let i = 0; i < n; i++) stale.push(k);
  return { added, stale };
}

function main(argv) {
  const [reportPath, baselinePath] = argv;
  if (!reportPath || !baselinePath) {
    console.error("usage: jscpd-baseline.mjs <report.json> <baseline.txt>");
    return 2;
  }
  const { added, stale } = compare(
    JSON.parse(readFileSync(reportPath, "utf8")),
    parseBaseline(readFileSync(baselinePath, "utf8")),
  );
  for (const k of added) console.error(`new clone (not in baseline): ${k}`);
  for (const k of stale) console.error(`stale baseline entry (no such clone; delete it): ${k}`);
  if (added.length || stale.length) return 1;
  console.log("jscpd: every clone is in the baseline, no stale entries");
  return 0;
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  process.exit(main(process.argv.slice(2)));
}
