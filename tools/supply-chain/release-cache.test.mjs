// Test for #1479: no job in .github/workflows/docker.yml that holds
// `id-token: write` (it signs and publishes) may turn a cache on. A poisoned
// cache would otherwise be signed as ours. Run with
// `node --test 'tools/supply-chain/*.test.mjs'`. Reads the workflow as text,
// same node:test style as scanner-jobs.test.mjs; no YAML library, no git.
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";
import assert from "node:assert/strict";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const lines = readFileSync(join(root, ".github/workflows/docker.yml"), "utf8")
  .split("\n")
  .filter((l) => !/^\s*#/.test(l))
  .map((l) => l.replace(/\s+#.*$/, ""));

// Jobs: children of the top-level `jobs:` key, each at two-space indent.
function jobs() {
  const at = lines.findIndex((l) => /^jobs:\s*$/.test(l));
  assert.ok(at >= 0, "no jobs: key in docker.yml");
  const out = [];
  for (let i = at + 1; i < lines.length; i++) {
    if (/^\S/.test(lines[i])) break;
    const m = /^ {2}([\w-]+):\s*$/.exec(lines[i]);
    if (m) out.push({ name: m[1], start: i + 1, lines: [] });
    else if (out.length) out[out.length - 1].lines.push(lines[i]);
  }
  return out;
}

const holdsIdToken = (j) => j.lines.some((l) => /^ {6}id-token:\s*write\s*$/.test(l));

// Steps: each begins with a `- ` at six-space indent.
function steps(j) {
  const out = [];
  for (const l of j.lines) {
    if (/^ {6}- /.test(l)) out.push([l]);
    else if (out.length) out[out.length - 1].push(l);
  }
  return out.map((s) => s.join("\n"));
}

const input = (step, key) => {
  const m = new RegExp(`^\\s+${key}:\\s*(.*?)\\s*$`, "m").exec(step);
  return m ? m[1].replace(/^['"]|['"]$/g, "") : null;
};

// Why this step turns a cache on, or null.
function cacheReason(step) {
  const uses = /uses:\s*(\S+)/.exec(step)?.[1] ?? "";
  const action = uses.split("@")[0];
  if (action === "actions/cache" || action.startsWith("actions/cache/"))
    return `${action} restores a cache`;
  if (action === "actions/setup-go" && input(step, "cache") !== "false")
    return "actions/setup-go caches by default; it needs `cache: false`";
  if (action === "actions/setup-node") {
    for (const k of ["cache", "cache-dependency-path"])
      if (input(step, k) !== null) return `actions/setup-node sets \`${k}\``;
  }
  return null;
}

describe("#1479: the release job turns no cache on", () => {
  const signing = jobs().filter(holdsIdToken);

  it("finds the jobs that hold id-token: write", () => {
    assert.ok(signing.length >= 2, "expected publish-preview and release to hold id-token: write");
  });

  for (const j of signing) {
    it(`${j.name} (id-token: write) has no step that turns a cache on`, () => {
      const bad = steps(j).map(cacheReason).filter(Boolean);
      assert.deepEqual(bad, [], `${j.name} turns a cache on: ${bad.join("; ")}`);
    });
  }
});
