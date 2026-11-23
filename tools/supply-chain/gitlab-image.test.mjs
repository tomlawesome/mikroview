// Tests for #1472 landing step 1: GitLab builds, signs and publishes the
// product image by digest (jobs 1-8 of the design); GitHub is untouched.
// Run with `node --test tools/supply-chain/gitlab-image.test.mjs`. They read
// the real .gitlab-ci.yml (and the two scripts it calls) as text, in the same
// node:test style as trivy-scans.test.mjs.
import { execFileSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";
import assert from "node:assert/strict";

const tmpdirSync = () => mkdtempSync(join(tmpdir(), "gate-"));
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (p) => readFileSync(join(root, p), "utf8");
const ci = read(".gitlab-ci.yml");

// Non-comment text of a top-level key: from `name:` to the next top-level key.
function block(name) {
  const lines = ci.split("\n");
  const start = lines.findIndex((l) => l.startsWith(`${name}:`));
  if (start < 0) return null;
  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    if (/^[^\s#-][^\n]*:/.test(lines[i])) { end = i; break; }
  }
  return lines.slice(start, end).filter((l) => !/^\s*#/.test(l)).join("\n");
}
function job(name) {
  const b = block(name);
  assert.ok(b, `no job ${name} in .gitlab-ci.yml`);
  return b;
}

// Run the leading part of a job's script (everything before its first
// `docker version` / `docker load`) under bash, with node, apk and docker
// stubbed, and report whether `node scripts/ci-reuse-gate.js gate` was
// called. `- *reuse_gate` is replaced by the anchor's own body. The gate
// stub always says "no earlier run", so the script carries on.
function gateRuns(name, env) {
  const anchor = ci.match(/^\.reuse_gate: &reuse_gate \|\n((?: {2}.*\n)+)/m);
  assert.ok(anchor, "no .reuse_gate anchor");
  const anchorBody = anchor[1].split("\n").map((l) => l.replace(/^ {2}/, "")).join("\n");
  const lines = job(name).split("\n");
  const at = lines.findIndex((l) => /^ {2}script:\s*$/.test(l));
  assert.ok(at >= 0, `${name} has no script:`);
  const out = [];
  for (let i = at + 1; i < lines.length; i++) {
    const l = lines[i];
    if (/^ {2}\S/.test(l)) break;
    const m = l.match(/^ {4}- (.*)$/);
    if (!m) continue;
    const item = m[1];
    if (/^docker (version|load)\b/.test(item)) break;
    if (item === "*reuse_gate") { out.push(anchorBody); continue; }
    if (item === "|" || item === ">-") {
      const body = [];
      while (i + 1 < lines.length && (/^ {6}/.test(lines[i + 1]) || !lines[i + 1].trim())) body.push(lines[++i].replace(/^ {6}/, ""));
      if (body.some((b) => /^docker (version|load)\b/.test(b.trim()))) break;
      out.push(item === "|" ? body.join("\n") : body.join(" "));
    } else out.push(item);
  }
  const log = join(tmpdirSync(), "gate.log");
  const prog = [
    "apk() { :; }",
    `node() { echo "$*" >> '${log}'; return 1; }`,
    ...out,
  ].join("\n");
  try {
    execFileSync("bash", ["-c", prog], { env: { PATH: process.env.PATH, ...env }, stdio: "pipe" });
  } catch (e) {
    // a script that exits non-zero before the gate still counts as "gate not run"
  }
  let called = "";
  try { called = readFileSync(log, "utf8"); } catch { /* never called */ }
  return /ci-reuse-gate\.js gate/.test(called);
}
const needsOf = (b) => {
  const m = b.match(/^ {2}needs:\s*(\[[^\]]*\]|(?:\n {4}.*)+)/m);
  return m ? m[1] : "";
};

describe("#1472 .publish_ref anchor", () => {
  it("defines .publish_ref: push to preview, or push of a ^v[0-9] tag", () => {
    const b = block(".publish_ref");
    assert.ok(b, "no .publish_ref anchor");
    assert.match(b, /\$CI_PIPELINE_SOURCE == "push"/);
    assert.match(b, /\$CI_COMMIT_BRANCH == "preview"/);
    assert.match(b, /\$CI_COMMIT_TAG =~ \/\^v\[0-9\]\//);
  });
});

describe("#1472 job 1: build:image builds once into image.tar", () => {
  it("is a .dind job in stage test with needs: []", () => {
    const b = job("build:image");
    assert.match(b, /extends:\s*\.dind/);
    assert.match(b, /stage:\s*test/);
    // No needs, or only the promotion readiness check (#1504), absent elsewhere.
    assert.match(b, /needs:(?:\s*\[\s*\]|\n {4}- \{job: release_readiness, optional: true\}\n(?! {4}-))/);
  });
  it("builds with --load, no provenance or SBOM attestations, VERSION and OCI labels", () => {
    const b = job("build:image");
    assert.match(b, /docker buildx build/);
    assert.match(b, /--load/);
    assert.match(b, /--provenance=false/);
    assert.match(b, /--sbom=false/);
    assert.match(b, /--build-arg VERSION=/);
    for (const l of ["revision", "source", "version", "created"]) {
      assert.match(b, new RegExp(`org\\.opencontainers\\.image\\.${l}`));
    }
  });
  it("saves image.tar as a 1-day artifact", () => {
    const b = job("build:image");
    assert.match(b, /docker save -o image\.tar/);
    assert.match(b, /artifacts:[\s\S]*image\.tar/);
    assert.match(b, /expire_in:\s*1 day/);
  });
  it("the product Dockerfile is built exactly once in the pipeline, by build:image", () => {
    // Other Dockerfiles (gate:image's live-check.Dockerfile) may still be built.
    const text = ci.split("\n").filter((l) => !/^\s*#/.test(l)).join("\n").replace(/\\\n\s*/g, " ");
    const builds = text.split("\n").filter((l) => /docker (buildx )?build\b/.test(l));
    const product = builds.filter((l) => {
      const f = l.match(/(?:^|\s)(?:-f|--file)[ =]+(\S+)/);
      return !f || /^["']?(\.\/)?Dockerfile["']?$/.test(f[1]);
    });
    assert.equal(product.length, 1, `expected one product-image build, found ${product.length}`);
    assert.ok(job("build:image").replace(/\\\n\s*/g, " ").includes(product[0].trim()), "the one product build is not in build:image");
  });
});

describe("#1472 job 2: tests load the tar rather than rebuild", () => {
  for (const name of ["test:container", "check:upgrade-fixture-recorder"]) {
    it(`${name} needs build:image artifacts and docker loads image.tar`, () => {
      const b = job(name);
      assert.match(needsOf(b), /build:image/);
      assert.match(b, /docker load -i image\.tar/);
      assert.doesNotMatch(b, /docker (buildx )?build\b/);
    });
  }
  it("test:container's reuse gate is skipped on .publish_ref and kept on dev and merge requests", () => {
    assert.match(job("test:container"), /publish_ref/);
    const gate = (env) => gateRuns("test:container", env);
    // A publishing run tests what it ships: the gate must not run there.
    assert.equal(gate({ CI_PIPELINE_SOURCE: "push", CI_COMMIT_BRANCH: "preview" }), false, "gate runs on a push to preview");
    assert.equal(gate({ CI_PIPELINE_SOURCE: "push", CI_COMMIT_TAG: "v1.2.3" }), false, "gate runs on a v* tag push");
    // ...and stays for the runs it was written for.
    assert.equal(gate({ CI_PIPELINE_SOURCE: "push", CI_COMMIT_BRANCH: "dev" }), true, "gate is gone on dev");
    assert.equal(gate({ CI_PIPELINE_SOURCE: "merge_request_event" }), true, "gate is gone on merge requests");
  });
});

describe("#1472 jobs 3-4: version check and image scan feed on the tar", () => {
  it("release:check-version needs build:image and checks the tag against -version", () => {
    const b = job("release:check-version");
    assert.match(needsOf(b), /build:image/);
    assert.match(b, /CI_COMMIT_TAG/);
    assert.match(b, /-version/);
  });
  it("security:trivy-image needs build:image, scans --input image.tar, writes image.spdx.json", () => {
    const b = job("security:trivy-image");
    assert.match(needsOf(b), /build:image/);
    assert.match(b, /trivy image --input image\.tar/);
    assert.match(b, /--format spdx-json -o image\.spdx\.json/);
    assert.match(b, /expire_in:\s*90 days/);
  });
});

describe("#1472 job 5: publish:image pushes by digest to the GitLab registry", () => {
  it("is a .dind sync job, uninterruptible, on .publish_ref only", () => {
    const b = job("publish:image");
    assert.match(b, /extends:\s*\.dind/);
    assert.match(b, /stage:\s*sync/);
    assert.match(b, /interruptible:\s*false/);
    assert.match(b, /publish_ref/);
    assert.doesNotMatch(b, /dev_or_mr/);
  });
  it("needs build:image, release:check-version and security:trivy-image", () => {
    const n = needsOf(job("publish:image"));
    for (const j of ["build:image", "release:check-version", "security:trivy-image"]) {
      assert.ok(n.includes(j), `needs lacks ${j}`);
    }
  });
  it("loads the tar, pushes sha-$CI_COMMIT_SHA to $CI_REGISTRY_IMAGE and reads the digest", () => {
    const b = job("publish:image");
    assert.match(b, /docker load -i image\.tar/);
    assert.match(b, /docker push "?\$CI_REGISTRY_IMAGE:sha-\$CI_COMMIT_SHA/);
    assert.match(b, /imagetools inspect/);
    assert.match(b, /\{\{\.Manifest\.Digest\}\}/);
    assert.doesNotMatch(b, /docker (buildx )?build\b/);
  });
  it("writes .mv-supply-chain/gitlab-tested-image.json (90-day artifact) via a recording script", () => {
    const b = job("publish:image");
    assert.match(b, /\.mv-supply-chain\/gitlab-tested-image\.json/);
    assert.match(b, /expire_in:\s*90 days/);
    const script = read("scripts/gitlab-record-tested-image.sh");
    for (const f of ["imageReference", "imageDigest", "commit", "ref", "pipelineId", "pipelineUrl", "builtAt"]) {
      assert.ok(script.includes(f), `evidence writer lacks field ${f}`);
    }
  });
  it("does not move a channel tag", () => {
    assert.doesNotMatch(job("publish:image"), /\$CI_REGISTRY_IMAGE:(preview|\$CI_COMMIT_TAG)\b/);
  });
});

describe("#1472 job 6: sign:release-digest signs the GitLab digest", () => {
  it("runs on .publish_ref, needs publish:image, and does not wait on GHCR", () => {
    const b = job("sign:release-digest");
    assert.match(b, /publish_ref/);
    assert.match(needsOf(b), /publish:image/);
    assert.doesNotMatch(b, /MV_SIGN_WAIT_ATTEMPTS/);
    assert.doesNotMatch(b, /GHCR_PUBLISH_TOKEN|ghcr\.io/);
  });
  it("pins the job image by digest", () => {
    const m = job("sign:release-digest").match(/^ {2}image:\s*(\S+)/m);
    assert.ok(m, "no image: on sign:release-digest");
    assert.match(m[1], /@sha256:[0-9a-f]{64}$/, `${m[1]} is not pinned by digest`);
  });
  it("keeps the signing runner and no dind", () => {
    const b = job("sign:release-digest");
    assert.match(b, /tags:\s*\[mikroview-signing\]/);
    assert.doesNotMatch(b, /\.dind\b/);
  });
  it("sign-release-digest.sh uses the evidence file and GitLab registry, no GHCR token or poll", () => {
    const s = read("scripts/sign-release-digest.sh");
    assert.doesNotMatch(s, /GHCR_PUBLISH_TOKEN/);
    assert.doesNotMatch(s, /waiting for .* on GHCR/);
    assert.match(s, /gitlab-tested-image\.json/);
    assert.match(s, /CI_REGISTRY_IMAGE/);
    assert.match(s, /cosign attest/);
    assert.match(s, /spdxjson/);
    assert.match(s, /image\.spdx\.json/);
  });
  it("needs security:trivy-image for the SBOM it attests", () => {
    assert.match(needsOf(job("sign:release-digest")), /security:trivy-image/);
  });
  // Review finding 6: two runs on one digest could both pass the
  // already-signed check and both sign; a resource_group serialises them.
  it("is serialised by a resource_group", () => {
    assert.match(job("sign:release-digest"), /^ {2}resource_group:\s*\S+/m);
  });
});

describe("#1472 job 7: publish:channel retags only after the signature verifies", () => {
  it("is a .dind sync job needing publish:image and sign:release-digest", () => {
    const b = job("publish:channel");
    assert.match(b, /extends:\s*\.dind/);
    assert.match(b, /stage:\s*sync/);
    const n = needsOf(b);
    assert.ok(n.includes("publish:image") && n.includes("sign:release-digest"));
  });
  it("verify-release-signature.sh on $CI_REGISTRY_IMAGE@digest comes before imagetools create", () => {
    const b = job("publish:channel");
    const v = b.search(/scripts\/verify-release-signature\.sh "?\$CI_REGISTRY_IMAGE@/);
    const c = b.search(/imagetools create --prefer-index=false -t/);
    assert.ok(v >= 0, "no signature verification of the GitLab digest");
    assert.ok(c >= 0, "no imagetools create retag");
    assert.ok(v < c, "retag happens before the signature is verified");
  });
  it("has a resource_group so two publishing pipelines never retag at once", () => {
    assert.match(job("publish:channel"), /^ {2}resource_group:\s*\S+/m);
  });
  it("checks the pipeline's commit is still the head of the ref before it retags", () => {
    const b = job("publish:channel").replace(/\\\n\s*/g, " ");
    const c = b.search(/imagetools create/);
    assert.ok(c >= 0, "no retag");
    const before = b.slice(0, c);
    assert.match(before, /ls-remote|repository\/branches|\/branches\//, "no look-up of the ref's current head before the retag");
    assert.match(before, /CI_COMMIT_SHA/);
    assert.match(before, /preview|CI_COMMIT_BRANCH/);
  });
  it("retags :preview or the tag from the sha- tag and re-reads the digest", () => {
    const b = job("publish:channel");
    assert.match(b, /\$CI_REGISTRY_IMAGE:preview/);
    assert.match(b, /\$CI_COMMIT_TAG/);
    assert.match(b, /sha-\$CI_COMMIT_SHA/);
    assert.match(b, /imagetools inspect/);
  });
});

describe("#1472 job 8: record:upgrade-fixture pulls the GitLab digest", () => {
  it("needs publish:channel, no GHCR poll", () => {
    const b = job("record:upgrade-fixture");
    assert.match(needsOf(b), /publish:channel/);
    assert.doesNotMatch(b, /ghcr\.io/);
    assert.doesNotMatch(b, /docker manifest inspect/);
  });
  it("pulls $CI_REGISTRY_IMAGE@<digest> and passes MV_IMAGE_REF; exit 75 kept for a pull failure", () => {
    const b = job("record:upgrade-fixture");
    assert.match(b, /\$CI_REGISTRY_IMAGE@/);
    assert.match(b, /MV_IMAGE_REF/);
    assert.match(b, /exit 75/);
  });
  it("record-upgrade-fixture.sh honours MV_IMAGE_REF, default still the GHCR tag", () => {
    const s = read("scripts/record-upgrade-fixture.sh");
    assert.match(s, /MV_IMAGE_REF/);
    assert.match(s, /ghcr\.io\/tomlawesome\/mikroview:/);
  });
});

// (Step 1's "no publish-from-gitlab.yml / release-on-tag.yml yet" check is gone:
// step 3 adds them, and github-copy.test.mjs covers them.)

// publish:channel's "is this still preview's head" check, run for real: the
// text from the `if [ -z "${CI_COMMIT_TAG:-}" ]` guard to the success echo is
// extracted from .gitlab-ci.yml and run under bash with `git` and `docker`
// stubbed on PATH. `git ls-remote` matches refs by suffix, so a branch named
// zz/refs/heads/preview adds a second line; only the line whose ref is exactly
// refs/heads/$CI_COMMIT_BRANCH may count.
describe("publish:channel head check", () => {
  const SHA = "a".repeat(40);
  const OTHER = "b".repeat(40);
  const DIGEST = "sha256:" + "c".repeat(64);
  function run(lsRemote, { fail = false } = {}) {
    const lines = job("publish:channel").split("\n");
    const from = lines.findIndex((l) => /^\s+if \[ -z "\$\{CI_COMMIT_TAG:-\}" \]; then\s*$/.test(l));
    const to = lines.findIndex((l, i) => i > from && /echo "publish:channel: published/.test(l));
    assert.ok(from >= 0 && to > from, "head check not found in publish:channel");
    const indent = lines[from].match(/^\s*/)[0].length;
    const body = lines.slice(from, to + 1).map((l) => l.slice(indent)).join("\n");
    const dir = tmpdirSync();
    const stub = (n, s) => { writeFileSync(join(dir, n), `#!/bin/bash\n${s}\n`); chmodSync(join(dir, n), 0o755); };
    const log = join(dir, "docker.log");
    stub("git", fail ? "exit 2" : `printf '%s' '${lsRemote}'`);
    stub("docker", `echo "$*" >> '${log}'\ncase "$*" in *inspect*) echo '${DIGEST}';; esac`);
    let status = 0;
    try {
      execFileSync("bash", ["-c", `set -eu\ndigest='${DIGEST}'\nchannel=reg/img:preview\n${body}`], {
        env: { PATH: `${dir}:${process.env.PATH}`, CI_REPOSITORY_URL: "https://example.invalid/r.git",
          CI_COMMIT_BRANCH: "preview", CI_COMMIT_SHA: SHA, CI_REGISTRY_IMAGE: "reg/img" },
        stdio: "pipe",
      });
    } catch (e) { status = e.status; }
    let calls = "";
    try { calls = readFileSync(log, "utf8"); } catch { /* docker never called */ }
    return { status, published: /imagetools create/.test(calls) };
  }

  it("publishes when the exact ref is this commit, despite a suffix-matching ref", () => {
    const r = run(`${OTHER}\trefs/heads/zz/refs/heads/preview\n${SHA}\trefs/heads/preview\n`);
    assert.deepEqual(r, { status: 0, published: true });
  });
  it("publishes when the exact ref is this commit and nothing else matches", () => {
    assert.deepEqual(run(`${SHA}\trefs/heads/preview\n`), { status: 0, published: true });
  });
  it("refuses when the exact ref is at a different commit", () => {
    const r = run(`${OTHER}\trefs/heads/preview\n${SHA}\trefs/heads/zz/refs/heads/preview\n`);
    assert.deepEqual(r, { status: 1, published: false });
  });
  it("refuses when the exact ref is missing", () => {
    const r = run(`${SHA}\trefs/heads/zz/refs/heads/preview\n`);
    assert.deepEqual(r, { status: 1, published: false });
  });
  it("refuses when ls-remote fails", () => {
    assert.deepEqual(run("", { fail: true }), { status: 1, published: false });
  });
});
