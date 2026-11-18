import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { cloneKey, compare, parseBaseline } from "./jscpd-baseline.mjs";

const clone = (a, b, lines, tokens) => ({
  firstFile: { name: a, start: 1 },
  secondFile: { name: b, start: 9 },
  lines,
  tokens,
});
const script = path.join(path.dirname(fileURLToPath(import.meta.url)), "jscpd-baseline.mjs");

function run(report, baseline) {
  const dir = mkdtempSync(path.join(tmpdir(), "jscpd-baseline-"));
  writeFileSync(path.join(dir, "r.json"), JSON.stringify(report));
  writeFileSync(path.join(dir, "b.txt"), baseline);
  try {
    execFileSync("node", [script, path.join(dir, "r.json"), path.join(dir, "b.txt")], { stdio: "pipe" });
    return 0;
  } catch (e) {
    return e.status;
  }
}

test("key ignores line numbers and file order", () => {
  assert.equal(cloneKey(clone("b.ts", "a.ts", 6, 50)), cloneKey(clone("a.ts", "b.ts", 6, 50)));
});

test("a clone in the baseline passes", () => {
  const report = { duplicates: [clone("a.ts", "b.ts", 6, 50)] };
  assert.deepEqual(compare(report, [cloneKey(report.duplicates[0])]), { added: [], stale: [] });
  assert.equal(run(report, `# note\n${cloneKey(report.duplicates[0])}\n`), 0);
});

test("a clone not in the baseline fails", () => {
  const report = { duplicates: [clone("a.ts", "b.ts", 6, 50)] };
  assert.equal(compare(report, []).added.length, 1);
  assert.equal(run(report, "# empty\n"), 1);
});

test("a baseline entry matching no clone is stale and fails", () => {
  const report = { duplicates: [] };
  const entry = cloneKey(clone("a.ts", "b.ts", 6, 50));
  assert.deepEqual(compare(report, [entry]).stale, [entry]);
  assert.equal(run(report, `${entry}\n`), 1);
});

test("a second identical clone beyond the baseline count fails", () => {
  const c = clone("a.ts", "b.ts", 6, 50);
  assert.equal(compare({ duplicates: [c, c] }, [cloneKey(c)]).added.length, 1);
});

test("comments and blank lines are ignored", () => {
  assert.deepEqual(parseBaseline("# x\n\n a \n"), ["a"]);
});
