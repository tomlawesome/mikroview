// Tests for #1462 steps 1-4: Trivy pinned by digest, scanning the built
// image and the config, and a disciplined .trivyignore. Run with
// `node --test tools/supply-chain/trivy-scans.test.mjs`. They read the real
// .gitlab-ci.yml as text, same node:test style as pins-policy.test.mjs.
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";
import assert from "node:assert/strict";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const ci = readFileSync(join(root, ".gitlab-ci.yml"), "utf8");
const lines = ci.split("\n");

// Each `trivy <sub> ...` invocation as one string: the starting line plus any
// folded continuation lines (up to the next list item, blank line or key).
function trivyCommands(sub) {
  const out = [];
  const re = new RegExp(`(^|[\\s"'])trivy\\s+${sub}\\b`);
  lines.forEach((line, i) => {
    if (/^\s*#/.test(line) || !re.test(line)) return;
    let cmd = line;
    for (let j = i + 1; j < lines.length; j++) {
      const n = lines[j];
      if (!n.trim() || /^\s*-\s/.test(n) || /^\s*[\w.-]+:(\s|$)/.test(n)) break;
      cmd += " " + n.trim();
    }
    out.push(cmd);
  });
  return out;
}

const hasExitCode1 = (c) => /--exit-code[ =]1\b/.test(c);
function severities(c) {
  const m = c.match(/--severity[ =]([A-Za-z,]+)/);
  return m ? m[1].toUpperCase().split(",") : [];
}
const failsAtHigh = (c) => {
  const s = severities(c);
  return s.includes("HIGH") && s.includes("CRITICAL");
};

describe("#1462 step 1: Trivy pinned by digest", () => {
  it("every aquasec/trivy image reference carries @sha256:<64 hex>", () => {
    const refs = ci.split("\n")
      .filter((l) => !/^\s*#/.test(l))
      .flatMap((l) => l.match(/aquasec\/trivy[^\s"']*/g) ?? []);
    assert.ok(refs.length > 0, "no aquasec/trivy reference found");
    for (const r of refs) {
      assert.match(r, /@sha256:[0-9a-f]{64}$/, `${r} is not pinned by digest`);
    }
  });
});

describe("#1462 step 2: Trivy image scan of the built product image", () => {
  it("a job runs trivy image on the built image, blocking at HIGH,CRITICAL", () => {
    const cmds = trivyCommands("image");
    assert.ok(cmds.length > 0, "no `trivy image` command in .gitlab-ci.yml");
    const ok = cmds.filter(
      (c) => hasExitCode1(c) && failsAtHigh(c) && /\$APP\b|\$\{APP\}|--input\b/.test(c),
    );
    assert.ok(
      ok.length > 0,
      "need `trivy image` on $APP or --input <tar> with --exit-code 1 --severity HIGH,CRITICAL",
    );
  });
});

describe("#1462 step 3: Trivy config scan of Dockerfiles and compose files", () => {
  const files = execFileSync("git", ["ls-files"], { cwd: root, encoding: "utf8" })
    .split("\n")
    .filter((f) => /(^|\/)([^/]*\.)?dockerfile$/i.test(f) || /(^|\/)(docker-)?compose[^/]*\.ya?ml$/i.test(f));

  it("finds Dockerfiles and a compose file to cover", () => {
    assert.ok(files.some((f) => /dockerfile/i.test(f)));
    assert.ok(files.some((f) => /compose/i.test(f)));
  });

  it("a job runs trivy config over all of them, blocking at HIGH,CRITICAL", () => {
    const cmds = trivyCommands("config").filter((c) => hasExitCode1(c) && failsAtHigh(c));
    assert.ok(cmds.length > 0, "no `trivy config --exit-code 1 --severity HIGH,CRITICAL` command");
    const covers = (c, f) =>
      c.split(/\s+/).some((t) => {
        t = t.replace(/^\.\//, "").replace(/\/$/, "");
        return t === "." || t === f || (t && !t.startsWith("-") && f.startsWith(t + "/"));
      });
    const full = cmds.some((c) => files.every((f) => covers(c, f)));
    assert.ok(full, `no trivy config command covers every file: ${files.join(", ")}`);
  });
});

describe("#1462 step 4: .trivyignore entries carry a reason and an expiry", () => {
  it("every entry has a comment above it with a reason and YYYY-MM-DD expiry", () => {
    const p = join(root, ".trivyignore");
    if (!existsSync(p)) return; // no ignore file: nothing to justify
    const ls = readFileSync(p, "utf8").split("\n");
    ls.forEach((l, i) => {
      if (!l.trim() || l.trim().startsWith("#")) return;
      const block = [];
      for (let j = i - 1; j >= 0 && ls[j].trim().startsWith("#"); j--) block.push(ls[j]);
      const text = block.join(" ");
      assert.ok(block.length > 0, `line ${i + 1} (${l}) has no comment above it`);
      assert.match(text, /\b\d{4}-\d{2}-\d{2}\b/, `line ${i + 1} (${l}) lacks an expiry date`);
      assert.ok(text.replace(/[#\d-]/g, "").trim().length > 10, `line ${i + 1} (${l}) lacks a reason`);
    });
  });
});
