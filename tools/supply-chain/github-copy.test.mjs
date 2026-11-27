// REWRITTEN FOR #1563 (push to GHCR from GitLab). The #1472 design, where
// GitHub copied the image from GitLab, is gone: GitLab's job publish:ghcr
// pushes the signed image, signature and SBOM attestation to GHCR, and
// publish-from-gitlab.yml (preview push), release-on-tag.yml (v* tag) and
// countersign.yml read ONLY ghcr.io, verify there, and attest. GitHub writes
// no image tag. Written from the #1563 spec, not from the workflow code.
// The GitLab side and the "no gitlab host named" scan live in
// scripts/github-workflows-ghcr-only.test.sh; only a cheap guard repeats here.
// Run with `node --test tools/supply-chain/github-copy.test.mjs`.
//
// YAML reading: text only, no dependency (the lint job's node:26-alpine image
// has no python or YAML library). parseYaml() below reads the small block-YAML
// subset GitHub workflows use (maps, sequences of maps, flow lists, quoted
// and plain scalars, | and > block scalars) by indentation, in the style of
// gitlab-image.test.mjs's block(): `on` stays the plain key "on".
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";
import assert from "node:assert/strict";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const WF = ".github/workflows";

function scalar(v) {
  v = v.trim();
  if (/^["']/.test(v)) return v.replace(/^(["'])(.*?)\1(\s+#.*)?$/, "$2");
  v = v.replace(/\s+#.*$/, "");
  if (v === "{}") return {};
  if (v === "[]") return [];
  if (/^\[.*\]$/.test(v)) return v.slice(1, -1).split(",").map((x) => x.trim()).filter(Boolean).map(scalar);
  if (v === "true") return true;
  if (v === "false") return false;
  return v;
}
function parseYaml(text) {
  const raw = text.split("\n").map((t) => ({ t, ind: t.length - t.trimStart().length }));
  const skippable = (l) => !l.t.trim() || l.t.trim().startsWith("#");
  let i = 0;
  const skip = () => { while (i < raw.length && skippable(raw[i])) i++; };
  function blockScalar(parent, fold) {
    const body = [];
    while (i < raw.length && (!raw[i].t.trim() || raw[i].ind > parent)) body.push(raw[i++].t);
    const ind = Math.min(...body.filter((x) => x.trim()).map((x) => x.length - x.trimStart().length), 1e9);
    return body.map((x) => x.slice(Math.min(ind, x.length))).join(fold ? " " : "\n").trimEnd();
  }
  function parse(indent) {
    skip();
    return raw[i].t.trim().startsWith("- ") ? parseSeq(raw[i].ind) : parseMap(raw[i].ind);
  }
  function parseSeq(indent) {
    const out = [];
    for (skip(); i < raw.length && raw[i].ind === indent && raw[i].t.trim().startsWith("- "); skip()) {
      const rest = raw[i].t.trim().slice(2);
      raw[i] = { t: " ".repeat(indent + 2) + rest, ind: indent + 2 };
      out.push(/^("[^"]*"|'[^']*'|[\w.\-]+):(\s|$)/.test(rest) ? parseMap(indent + 2) : (i++, scalar(rest)));
    }
    return out;
  }
  function parseMap(indent) {
    const out = {};
    for (skip(); i < raw.length && raw[i].ind === indent; skip()) {
      const m = raw[i].t.trim().match(/^("[^"]*"|'[^']*'|[^\s:][^:]*?):(?:\s+(.*))?$/);
      if (!m) throw new Error(`cannot read YAML line: ${raw[i].t}`);
      const key = m[1].replace(/^["']|["']$/g, "");
      const val = (m[2] ?? "").trim();
      i++;
      if (/^[|>][+-]?$/.test(val)) out[key] = blockScalar(indent, val[0] === ">");
      else if (val && !val.startsWith("#")) out[key] = scalar(val);
      else {
        skip();
        const nxt = raw[i];
        out[key] = nxt && (nxt.ind > indent || (nxt.ind === indent && nxt.t.trim().startsWith("- "))) ? parse() : null;
      }
    }
    return out;
  }
  return parse();
}

function load(name) {
  const p = join(root, WF, name);
  assert.ok(existsSync(p), `${WF}/${name} does not exist`);
  const text = readFileSync(p, "utf8");
  const doc = parseYaml(text);
  const header = text.split("\n").filter((l) => l.startsWith("#")).join("\n");
  return { text, doc, header };
}
const onOf = (doc) => doc.on;
const jobsOf = (doc) => Object.values(doc.jobs ?? {});
// Every step of every job, in file order (jobs in order, steps in order).
const stepsOf = (doc) => jobsOf(doc).flatMap((j) => j.steps ?? []);
const runOf = (s) => String(s.run ?? "");
const stepText = (s) => `${runOf(s)}\n${JSON.stringify(s.with ?? {})}\n${s.uses ?? ""}`;
const indexOfStep = (steps, re, from = 0) => {
  for (let i = from; i < steps.length; i++) if (re.test(stepText(steps[i]))) return i;
  return -1;
};
const allRun = (doc) => stepsOf(doc).map(runOf).join("\n");
// Shell line continuations folded, so one command reads as one line.
const joinCont = (t) => t.replace(/[ \t]*\\\n\s*/g, " ");
const doc0 = (name) => load(name).doc;
const jobText = (job) => joinCont((job?.steps ?? []).map(stepText).join("\n"));

const NEW = ["publish-from-gitlab.yml", "release-on-tag.yml"];

// Why a run block would let a failing check pass, or null. Review finding 4:
// `|| true` alone was not enough; `|| :` and `set +e` fail open too.
function failOpen(t) {
  if (/\|\|\s*(true|:)(?=[\s;)&|]|$)/m.test(t)) return "|| true / || :";
  if (/(^|[;&|(]\s*)set\s+(-\w+\s+)*\+\w*e/m.test(t)) return "set +e";
  if (/\bset\s+\+o\s+errexit\b/.test(t)) return "set +o errexit";
  return null;
}
const GHCR = "ghcr.io/tomlawesome/mikroview";
const wfFiles = () => readdirSync(join(root, WF)).filter((f) => /\.ya?ml$/.test(f)).sort();
const readRepo = (p) => joinCont(readFileSync(join(root, p), "utf8")).replace(/[ \t]+/g, " ");
const jobEntries = (doc) => Object.entries(doc.jobs ?? {});
const verifyStepsOf = (doc) => stepsOf(doc).filter((s) => /verify-release-signature\.sh|cosign verify-attestation|cosign verify\b/.test(runOf(s)));

describe("#1563: the fail-open guard", () => {
  for (const bad of [
    'scripts/verify-release-signature.sh "${REF}" || true',
    'scripts/verify-release-signature.sh "${REF}" || :',
    'cosign verify-attestation --key cosign.pub "${REF}" > a.jsonl || :\nnext',
    "set +e\nscripts/verify-release-signature.sh \"${REF}\"",
    "set +eu\ncosign verify-attestation x",
    "set +o errexit\ncosign verify-attestation x",
  ]) {
    it(`catches ${JSON.stringify(bad)}`, () => assert.notEqual(failOpen(bad), null));
  }
  for (const good of [
    'set -e\nscripts/verify-release-signature.sh "${REF}"',
    "set -euo pipefail\nx || exit 1",
    'if [ "${count}" != 1 ]; then\n  exit 1\nfi',
  ]) {
    it(`passes ${JSON.stringify(good)}`, () => assert.equal(failOpen(good), null));
  }
});

for (const name of NEW) {
  describe(`#1563 ${name}: shape`, () => {
    it("never installs Node or Go, and never builds", () => {
      const { doc } = load(name);
      const uses = stepsOf(doc).map((s) => s.uses ?? "").join("\n");
      assert.doesNotMatch(uses, /actions\/setup-(node|go)/);
      assert.doesNotMatch(uses, /docker\/build-push-action/);
      const run = allRun(doc);
      assert.doesNotMatch(run, /\bnpm (ci|install|i)\b/);
      assert.doesNotMatch(run, /\bgo (build|install|test)\b/);
      assert.doesNotMatch(run, /\bdocker (buildx )?build\b/);
    });
    it("pins every action by a full 40-hex commit sha", () => {
      const uses = stepsOf(load(name).doc).map((s) => s.uses).filter(Boolean);
      assert.ok(uses.length > 0, "no actions used at all");
      for (const u of uses) assert.match(u, /^[^@\s]+@[0-9a-f]{40}(\s|$)/, `not sha-pinned: ${u}`);
    });
    it("grants only contents: read plus packages, id-token and attestations write", () => {
      const { doc } = load(name);
      const grants = [doc.permissions, ...jobsOf(doc).map((j) => j.permissions)].filter(Boolean);
      assert.ok(grants.length > 0, "no permissions block");
      const merged = {};
      for (const g of grants) {
        assert.equal(typeof g, "object", "permissions must be an explicit map");
        Object.assign(merged, g);
      }
      assert.equal(merged.contents, "read");
      assert.equal(merged["id-token"], "write");
      assert.equal(merged.attestations, "write");
      for (const k of Object.keys(merged)) {
        assert.ok(["contents", "packages", "id-token", "attestations"].includes(k), `unexpected permission ${k}`);
      }
      assert.ok(!Object.values(doc.permissions ?? {}).includes("write-all"));
      assert.equal(doc.permissions?.contents, "read", "workflow default must be contents: read");
    });
    it("only the job that attests holds id-token or attestations write", () => {
      const { doc } = load(name);
      assert.ok(!doc.permissions?.["id-token"] && !doc.permissions?.attestations, "workflow-level id-token/attestations");
      for (const [n, j] of jobEntries(doc)) {
        const attests = (j.steps ?? []).some((s) => /^actions\/attest@/.test(s.uses ?? ""));
        if (!attests) {
          assert.notEqual(j.permissions?.["id-token"], "write", `${n} has id-token: write but does not attest`);
          assert.notEqual(j.permissions?.attestations, "write", `${n} has attestations: write but does not attest`);
        }
      }
    });
  });

  describe(`#1563 ${name}: GitHub writes no image and reads only GHCR`, () => {
    it("has no step that writes an image, tag, or signature", () => {
      const run = joinCont(allRun(load(name).doc));
      assert.doesNotMatch(run, /imagetools create/, "writes a tag");
      assert.doesNotMatch(run, /\bdocker (image )?(push|tag)\b/);
      assert.doesNotMatch(run, /\bcosign (sign|copy|attest)\b/, "signs or attests on GitHub with cosign");
      assert.doesNotMatch(run, /\bgit (push|tag)\b/);
    });
    it("reads the image from ghcr.io/tomlawesome/mikroview and names no GitLab host or read token", () => {
      const { text } = load(name);
      assert.match(text, /ghcr\.io\/tomlawesome\/mikroview/);
      assert.doesNotMatch(text, /gitlab\.tomlawson\.io|registry\.tomlawson\.io|GITLAB_READ_TOKEN|gitlab-await-tested-image/);
    });
    it("waits for the tag on GHCR to resolve, with a bounded wait", () => {
      const run = joinCont(allRun(load(name).doc));
      assert.match(run, /imagetools inspect|manifest inspect|crane digest/, "the GHCR tag is never resolved");
      assert.match(run, /\b(for|while|until)\b[^\n]*|attempt|deadline|timeout/i, "no wait loop");
      assert.match(run, /sleep\b/, "wait loop does not pause between tries");
      assert.match(run, /exit 1/, "the wait never gives up");
    });
    it("fails when the tag resolves to more than one digest or none (one digest only)", () => {
      const run = joinCont(allRun(load(name).doc));
      assert.match(run, /sha256:\[0-9a-f\]\{64\}|\^sha256:/, "the resolved digest is not validated");
    });
  });

  describe(`#1563 ${name}: verify on the GHCR digest, nothing skippable`, () => {
    it("runs verify-release-signature.sh and cosign verify-attestation (spdxjson, cosign.pub) on a ghcr.io digest", () => {
      const { doc } = load(name);
      const t = joinCont(stepsOf(doc).map(stepText).join("\n"));
      assert.match(t, /verify-release-signature\.sh/);
      assert.match(t, /cosign verify-attestation[^\n]*--key cosign\.pub[^\n]*--type spdxjson|cosign verify-attestation[^\n]*--type spdxjson[^\n]*--key cosign\.pub/);
    });
    it("requires exactly one verified attestation", () => {
      const t = joinCont(allRun(load(name).doc));
      const va = t.search(/cosign verify-attestation/);
      assert.ok(va >= 0);
      const tail = t.slice(va);
      assert.match(tail, /grep -c|wc -l|jq -s|length/, "no count of verified attestations");
      assert.match(tail, /(!=|-ne)\s*"?1"?/, "count is not compared with exactly one");
    });
    it("verify steps carry no continue-on-error and do not fail open", () => {
      const { doc } = load(name);
      const checks = verifyStepsOf(doc);
      assert.ok(checks.length > 0, "no verify step");
      for (const s of checks) {
        assert.ok(!s["continue-on-error"], "verify has continue-on-error");
        assert.equal(failOpen(runOf(s)), null, "verify step fails open");
      }
    });
    it("verification comes before the first attest step", () => {
      const steps = stepsOf(load(name).doc);
      const v = steps.findIndex((s) => /verify-release-signature\.sh/.test(runOf(s)));
      const a = steps.findIndex((s) => /^actions\/attest@/.test(s.uses ?? ""));
      assert.ok(v >= 0 && a >= 0 && v < a, "attest comes before the signature verify");
      const va = steps.findIndex((s) => /cosign verify-attestation/.test(runOf(s)));
      assert.ok(va >= 0 && va < a, "attest comes before the attestation verify");
    });
  });

  describe(`#1563 ${name}: attestations`, () => {
    it("attests provenance and the SBOM and verifies both", () => {
      const { doc, text } = load(name);
      const attest = stepsOf(doc).filter((s) => /^actions\/attest@/.test(s.uses ?? ""));
      assert.ok(attest.length >= 2, "need two actions/attest steps");
      assert.ok(attest.some((s) => s.with?.["sbom-path"]), "no SBOM attest (sbom-path)");
      assert.ok(attest.some((s) => !s.with?.["sbom-path"]), "no provenance attest");
      const verifies = joinCont(allRun(doc)).split("\n").filter((l) => /gh attestation verify/.test(l));
      assert.ok(verifies.some((l) => !/--predicate-type/.test(l)), "no provenance verify");
      assert.ok(verifies.some((l) => /--predicate-type https:\/\/spdx\.dev\/Document\/v2\.3/.test(l)), "no SPDX verify");
      assert.match(text, /upload-artifact@[0-9a-f]{40}/, "evidence is not uploaded");
    });
    it("attests the verified SBOM: sbom-path is taken from the verified attestation predicate", () => {
      const steps = stepsOf(load(name).doc);
      const at = steps.findIndex((s) => /^actions\/attest@/.test(s.uses ?? "") && s.with?.["sbom-path"]);
      assert.ok(at >= 0, "no SBOM attest step");
      const path = steps[at].with["sbom-path"];
      const before = joinCont(steps.slice(0, at).map(runOf).join("\n"));
      assert.match(before, /verify-attestation[\s\S]*\.predicate/, "the SBOM is not the verified predicate");
      assert.ok(before.includes(path), "the verified predicate is not written to the attested path");
    });
    it("the attest and verify steps address the GHCR digest, not a tag", () => {
      const { doc } = load(name);
      for (const s of stepsOf(doc).filter((x) => /^actions\/attest@/.test(x.uses ?? ""))) {
        assert.match(String(s.with?.["subject-name"] ?? ""), /ghcr\.io\/tomlawesome\/mikroview|env\./, "attest subject is not the GHCR image");
        assert.match(String(s.with?.["subject-digest"] ?? ""), /\$\{\{/, "attest subject-digest is not taken from the resolved digest");
      }
    });
  });
}

describe("#1563 publish-from-gitlab.yml: triggers and :preview", () => {
  it("runs on push to preview and on workflow_dispatch only", () => {
    const on = onOf(load("publish-from-gitlab.yml").doc);
    assert.deepEqual(on.push?.branches, ["preview"]);
    assert.ok(!on.push?.tags, "must not run on tags");
    assert.deepEqual(Object.keys(on).sort(), ["push", "workflow_dispatch"]);
  });
  it("reads :preview-<short7> from GHCR", () => {
    const run = allRun(load("publish-from-gitlab.yml").doc);
    assert.match(run, /(mikroview|IMAGE\}?):preview-\$\{?[A-Za-z_]+/, "no preview-<short7> tag");
    assert.match(run, /\bcut -c1-7\b|:0:7|--short=7|head -c 7|\bcut -c 1-7\b/, "short sha is not 7 characters");
  });
  it("only the newest pending run is kept (concurrency comment is true)", () => {
    const { text } = load("publish-from-gitlab.yml");
    assert.doesNotMatch(text, /every merge gets its own/i);
    assert.match(text, /only the newest[^\n]*pending|newest pending/i);
  });
});

describe("#1563 release-on-tag.yml: triggers", () => {
  it("runs on push of v* tags only", () => {
    const on = onOf(load("release-on-tag.yml").doc);
    assert.deepEqual(on.push?.tags, ["v*"]);
    assert.ok(!on.push?.branches, "must not run on branches");
    assert.deepEqual(Object.keys(on), ["push"]);
  });
  it("reads the :vX.Y.Z tag named by the pushed tag", () => {
    const { doc, text } = load("release-on-tag.yml");
    assert.match(joinCont(allRun(doc)), /(mikroview|IMAGE\}?):\$\{?(GITHUB_REF_NAME|[A-Za-z_]*TAG[A-Za-z_]*|[A-Za-z_]*VERSION[A-Za-z_]*)/i);
    assert.match(text, /GITHUB_REF_NAME|github\.ref_name/);
  });
  it("creates no GitHub Release and never moves :latest itself", () => {
    const { doc, text } = load("release-on-tag.yml");
    assert.doesNotMatch(text, /softprops\/action-gh-release|gh release create|actions\/create-release/);
    assert.doesNotMatch(allRun(doc), /gh release/);
    assert.doesNotMatch(joinCont(allRun(doc)), /mikroview:latest[^\n]*(create|push)|(create|push)[^\n]*mikroview:latest/);
  });
});

describe("#1472 docker.yml: dispatch only", () => {
  it("has no push, tag or branch triggers", () => {
    assert.deepEqual(Object.keys(onOf(load("docker.yml").doc)), ["workflow_dispatch"]);
  });
  it("every job carries a hard if: false", () => {
    const jobs = Object.entries(load("docker.yml").doc.jobs ?? {});
    assert.ok(jobs.length > 0);
    for (const [n, j] of jobs) {
      assert.ok([false, "false", "${{ false }}"].includes(j.if), `docker.yml job ${n} can run (if: ${JSON.stringify(j.if)})`);
    }
  });
  it("header maps the old jobs to the GitLab jobs and release-on-tag", () => {
    const { header } = load("docker.yml");
    for (const w of ["publish-preview", "release", "promote-to-main", "build:image", "test:container", "publish:image", "release:check-version", "release-on-tag", "latest"]) {
      assert.ok(header.includes(w), `header does not mention ${w}`);
    }
    assert.match(header, /security:/);
    assert.match(header, /#1464/);
  });
  it("is not what publishes: only the two GHCR readers trigger on preview or tags", () => {
    assert.deepEqual(onOf(load("publish-from-gitlab.yml").doc).push.branches, ["preview"]);
    assert.deepEqual(onOf(load("release-on-tag.yml").doc).push.tags, ["v*"]);
    assert.ok(!onOf(load("docker.yml").doc).push, "docker.yml still triggers on push");
  });
});

describe("#1563 countersign.yml: GHCR verify, then keyless signature", () => {
  it("is dispatch-only with a digest input validated as sha256:<64 hex>", () => {
    const { doc } = load("countersign.yml");
    assert.deepEqual(Object.keys(onOf(doc)), ["workflow_dispatch"]);
    assert.ok(onOf(doc).workflow_dispatch.inputs?.digest?.required);
    assert.match(allRun(doc), /\^sha256:\[0-9a-f\]\{64\}\$/);
  });
  it("reads only GHCR: no GitLab host, no read token, no registry login to GitLab", () => {
    const { text } = load("countersign.yml");
    assert.doesNotMatch(text, /gitlab\.tomlawson\.io|registry\.tomlawson\.io|GITLAB_READ_TOKEN/);
    assert.match(text, /ghcr\.io/);
  });
  it("verifies signature and attestation on the GHCR digest before the keyless sign, then verifies keyless", () => {
    const { doc, header } = load("countersign.yml");
    const t = joinCont(stepsOf(doc).map(runOf).join("\n"));
    const vs = t.search(/scripts\/verify-release-signature\.sh/);
    const va = t.search(/cosign verify-attestation[^\n]*--key cosign\.pub/);
    const sg = t.search(/cosign sign --yes/);
    assert.ok(vs >= 0, "verify-release-signature.sh is not run");
    assert.ok(va >= 0, "verify-attestation is not run");
    assert.ok(sg >= 0, "no keyless cosign sign --yes");
    assert.ok(vs < sg && va < sg, "both verifies must precede the keyless sign");
    assert.match(t.slice(vs, vs + 400), /ghcr\.io|IMAGE|GHCR/, "the signature verify does not name the GHCR image");
    const kl = t.slice(sg).split("\n").find((l) => /cosign verify (?!-)/.test(l) && !/--key/.test(l));
    assert.ok(kl, "no keyless cosign verify after the sign");
    assert.match(kl, /--certificate-identity-regexp '\^https:\/\/github\.com\/tomlawesome\/mikroview\/'/);
    assert.match(kl, /--certificate-oidc-issuer https:\/\/token\.actions\.githubusercontent\.com/);
    assert.doesNotMatch(header, /cosign copy/);
    assert.match(header, /publish-from-gitlab|release-on-tag|publish:ghcr/);
  });
  it("its verify steps cannot fail open", () => {
    const checks = verifyStepsOf(load("countersign.yml").doc);
    assert.ok(checks.length > 0);
    for (const s of checks) {
      assert.ok(!s["continue-on-error"]);
      assert.equal(failOpen(runOf(s)), null);
    }
  });
});

describe("#1563: no oras, cosign copy or crane in any workflow", () => {
  for (const f of wfFiles()) {
    it(`${f} names none of them`, () => {
      const text = readFileSync(join(root, WF, f), "utf8");
      assert.doesNotMatch(text, /\boras\b/i);
      assert.doesNotMatch(text, /cosign copy\b/);
      assert.doesNotMatch(text, /\bcrane\b/);
    });
  }
});

describe("#1563: cosign-installer pinned to the version ensure-cosign.sh uses", () => {
  const ensure = readFileSync(join(root, "scripts/ensure-cosign.sh"), "utf8");
  const ver = ensure.match(/COSIGN_VERSION="([^"]+)"/)?.[1];
  it("ensure-cosign.sh's COSIGN_VERSION is v3.1.3", () => assert.equal(`v${ver}`, "v3.1.3"));
  for (const f of wfFiles()) {
    it(`${f}: every sigstore/cosign-installer step has cosign-release: v${ver}`, () => {
      const steps = stepsOf(load(f).doc).filter((s) => /^sigstore\/cosign-installer@/.test(s.uses ?? ""));
      for (const s of steps) assert.equal(s.with?.["cosign-release"], `v${ver}`);
    });
  }
});

const ciText = readFileSync(join(root, ".gitlab-ci.yml"), "utf8");
describe("#1563: retired GitLab-read pieces and CI wiring", () => {
  it("scripts/ci/gitlab-await-tested-image.sh and its test are gone, and CI does not run them", () => {
    assert.ok(!existsSync(join(root, "scripts/ci/gitlab-await-tested-image.sh")), "await script still present");
    assert.ok(!existsSync(join(root, "scripts/ci/gitlab-await-tested-image.test.sh")), "await test still present");
    assert.doesNotMatch(ciText, /gitlab-await-tested-image/);
  });
  it("scripts/verify-release-signature.sh is kept (reused on the GHCR digest)", () => {
    assert.ok(existsSync(join(root, "scripts/verify-release-signature.sh")));
  });
  it("github-copy.test.mjs is run by a CI job script line", () => {
    assert.match(ciText, /^\s*- .*(github-copy\.test\.mjs|tools\/supply-chain\/\*\.test\.mjs)/m);
  });
});

describe("#1563 docs: verify at GHCR, no copy tooling", () => {
  it("SECURITY.md's verify block checks the GHCR digest with the key and keyless, and provenance", () => {
    const sec = readRepo("SECURITY.md");
    const b = [...sec.matchAll(/```sh\n([\s\S]*?)```/g)].map((m) => m[1]).find((x) => /gh attestation verify/.test(x));
    assert.ok(b, "no sh block with gh attestation verify");
    assert.match(b, /gh attestation verify oci:\/\/ghcr\.io\/tomlawesome\/mikroview@/);
    assert.match(b, /cosign verify --key cosign\.pub ghcr\.io\/tomlawesome\/mikroview@/);
    assert.match(b, /cosign verify ghcr\.io\/tomlawesome\/mikroview@\S+ --certificate-identity-regexp/);
  });
  it("docs/release-signing.md explains how the digest reaches GHCR", () => {
    assert.match(readRepo("docs/release-signing.md"), /How the digest reaches GHCR/);
  });
  for (const p of ["docs/release-signing.md", "docs/releasing.md", "docs/decisions/release-versioning.md"]) {
    it(`${p} names neither cosign copy nor oras`, () => {
      const t = readRepo(p);
      assert.doesNotMatch(t, /cosign copy\b/);
      assert.doesNotMatch(t, /\boras\b/i);
    });
  }
});
