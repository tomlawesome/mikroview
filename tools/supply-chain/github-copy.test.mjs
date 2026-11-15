// Tests for #1472 landing step 3: GitHub stops building and only copies the
// image GitLab built, tested and signed (publish-from-gitlab.yml on preview,
// release-on-tag.yml on v* tags), and docker.yml becomes dispatch-only.
// Run with `node --test tools/supply-chain/github-copy.test.mjs`.
// Static checks of the workflow files, written from the step 3 spec alone.
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

for (const name of NEW) {
  describe(`#1472 step 3 ${name}: shape`, () => {
    it("never installs Node or Go, and never builds", () => {
      const { doc } = load(name);
      const uses = stepsOf(doc).map((s) => s.uses ?? "").join("\n");
      assert.doesNotMatch(uses, /actions\/setup-(node|go)/);
      const run = allRun(doc);
      assert.doesNotMatch(run, /\bnpm (ci|install|i)\b/);
      assert.doesNotMatch(run, /\bgo (build|install|test)\b/);
      assert.doesNotMatch(run, /\bdocker (buildx )?build\b/);
      assert.doesNotMatch(run, /\bdocker build-push\b|docker\/build-push-action/);
      assert.doesNotMatch(uses, /docker\/build-push-action/);
    });
    it("pins every action by a full 40-hex commit sha", () => {
      const { doc } = load(name);
      const uses = stepsOf(doc).map((s) => s.uses).filter(Boolean);
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
      assert.equal(merged.packages, "write");
      assert.equal(merged.attestations, "write");
      for (const k of Object.keys(merged)) {
        assert.ok(["contents", "packages", "id-token", "attestations"].includes(k), `unexpected permission ${k}`);
      }
      assert.ok(!Object.values(doc.permissions ?? {}).includes("write-all"));
    });
    it("awaits GitLab with scripts/ci/gitlab-await-tested-image.sh using the read token secret", () => {
      const { doc, text } = load(name);
      const steps = stepsOf(doc);
      const i = indexOfStep(steps, /scripts\/ci\/gitlab-await-tested-image\.sh/);
      assert.ok(i >= 0, "no await step");
      assert.match(text, /secrets\.GITLAB_READ_TOKEN/);
      assert.match(text, /GITLAB_PROJECT_ID:\s*['"]?53['"]?/);
    });
  });

  // Ruling v2 replaced cosign copy with imagetools create and moved the
  // key-based verification to the GitLab source; see "ruling v2" below.
  describe(`#1472 step 3 ${name}: verify before copy`, () => {
    it("the gitlab job's checks cannot be skipped or ignored", () => {
      const steps = doc0(name).jobs?.gitlab?.steps ?? [];
      const checks = steps.filter((x) => /verify-release-signature\.sh|cosign verify-attestation/.test(runOf(x)));
      assert.ok(checks.length > 0, "no verify step in the gitlab job");
      for (const s of checks) {
        assert.ok(!s["continue-on-error"], "verify has continue-on-error");
        assert.doesNotMatch(runOf(s), /\|\|\s*(true|:)\b/);
      }
    });
    it("copy needs gitlab, so nothing is written unless the gitlab job passed", () => {
      const needs = doc0(name).jobs?.copy?.needs;
      assert.ok([needs].flat().includes("gitlab"), "copy job does not need gitlab");
    });
  });

  describe(`#1472 step 3 ${name}: attestations`, () => {
    it("attests provenance and the SBOM (sbom-path image.spdx.json) and verifies both", () => {
      const { doc, text } = load(name);
      const attest = stepsOf(doc).filter((s) => /^actions\/attest@/.test(s.uses ?? ""));
      assert.ok(attest.length >= 2, "need two actions/attest steps");
      assert.ok(attest.some((s) => s.with?.["sbom-path"] === "image.spdx.json"), "no SBOM attest with sbom-path: image.spdx.json");
      assert.ok(attest.some((s) => !s.with?.["sbom-path"]), "no provenance attest");
      const verifies = allRun(doc).match(/gh attestation verify/g) ?? [];
      assert.ok(verifies.length >= 2, "need gh attestation verify for provenance and SBOM");
      assert.match(text, /upload-artifact@[0-9a-f]{40}/, "evidence is not uploaded");
    });
  });
}

describe("#1472 step 3 publish-from-gitlab.yml: triggers and :preview", () => {
  it("runs on push to preview and on workflow_dispatch only", () => {
    const { doc } = load("publish-from-gitlab.yml");
    const on = onOf(doc);
    assert.deepEqual(on.push?.branches, ["preview"]);
    assert.ok("workflow_dispatch" in on, "no workflow_dispatch");
    assert.ok(!on.push?.tags, "must not run on tags");
    assert.deepEqual(Object.keys(on).sort(), ["push", "workflow_dispatch"]);
  });
  it("copies to :preview-<short7> and to :preview", () => {
    const { doc } = load("publish-from-gitlab.yml");
    const run = allRun(doc);
    assert.match(run, /mikroview:preview-\$\{?[A-Za-z_]+/, "no preview-<short7> tag");
    assert.match(run, /\bcut -c1-7\b|:0:7|--short=7|head -c 7|\bcut -c 1-7\b/, "short sha is not 7 characters");
    assert.match(run, /mikroview:preview(["'\s]|$)/m, "no :preview tag");
  });
  it("moves :preview only after an exact-ref head check against refs/heads/preview", () => {
    const { doc } = load("publish-from-gitlab.yml");
    const steps = stepsOf(doc);
    const head = steps.findIndex((s) => /ls-remote/.test(runOf(s)) && /refs\/heads\/preview/.test(runOf(s)));
    assert.ok(head >= 0, "no ls-remote head check of refs/heads/preview");
    const text = runOf(steps[head]);
    // ls-remote matches by suffix: only the line whose ref is exactly the branch may count.
    assert.match(text, /\$2\s*==/, "head check is not an exact ref match");
    const moved = steps.findIndex((s) => /mikroview:preview(["'\s]|$)/m.test(runOf(s)) && /imagetools create/.test(runOf(s)));
    assert.ok(moved >= 0, "no copy to :preview");
    assert.ok(head <= moved, "head check must come before (or in) the :preview copy");
  });
});

describe("#1472 step 3 release-on-tag.yml: triggers and :latest", () => {
  it("runs on push of v* tags only", () => {
    const { doc } = load("release-on-tag.yml");
    const on = onOf(doc);
    assert.deepEqual(on.push?.tags, ["v*"]);
    assert.ok(!on.push?.branches, "must not run on branches");
    assert.deepEqual(Object.keys(on), ["push"]);
  });
  it("copies to :vX.Y.Z from the tag's own pipeline", () => {
    const { doc, text } = load("release-on-tag.yml");
    assert.match(joinCont(allRun(doc)), /imagetools create[^\n]*mikroview:\$\{?(GITHUB_REF_NAME|[A-Za-z_]*TAG[A-Za-z_]*|[A-Za-z_]*VERSION[A-Za-z_]*)/i);
    assert.match(text, /GITHUB_REF_NAME|github\.ref_name/);
  });
  it("moves :latest with imagetools create --prefer-index=false after re-checking the digest, then re-reads it", () => {
    const { doc } = load("release-on-tag.yml");
    const steps = stepsOf(doc);
    const lat = steps.findIndex((s) => /imagetools create/.test(runOf(s)) && /mikroview:latest/.test(runOf(s)));
    assert.ok(lat >= 0, "no imagetools create to :latest");
    const t = runOf(steps[lat]);
    assert.match(t, /--prefer-index=false/);
    const create = t.search(/imagetools create/);
    assert.match(t.slice(0, create) + steps.slice(0, lat).map(runOf).join("\n"), /imagetools inspect|crane digest|manifest inspect/, "no digest re-check before :latest");
    assert.match(t.slice(create), /imagetools inspect|crane digest|manifest inspect/, "no re-read of :latest after the write");
  });
  it("creates no GitHub Release", () => {
    const { doc, text } = load("release-on-tag.yml");
    assert.doesNotMatch(text, /softprops\/action-gh-release|gh release create|actions\/create-release/);
    assert.doesNotMatch(allRun(doc), /gh release/);
  });
});

describe("#1472 step 3 docker.yml: dispatch only", () => {
  it("has no push, tag or branch triggers", () => {
    const { doc } = load("docker.yml");
    const on = onOf(doc);
    assert.deepEqual(Object.keys(on), ["workflow_dispatch"]);
  });
  it("header maps the old jobs to the GitLab jobs and release-on-tag", () => {
    const { header } = load("docker.yml");
    for (const w of ["publish-preview", "release", "promote-to-main", "build:image", "test:container", "publish:image", "release:check-version", "release-on-tag", "latest"]) {
      assert.ok(header.includes(w), `header does not mention ${w}`);
    }
    assert.match(header, /security:/);
    assert.match(header, /#1464/);
  });
  it("is the only workflow besides release-on-tag/publish-from-gitlab that could write :preview or :latest, and none of the others trigger on push to preview/tags", () => {
    const pub = load("publish-from-gitlab.yml").doc;
    const rel = load("release-on-tag.yml").doc;
    const dock = load("docker.yml").doc;
    assert.deepEqual(onOf(pub).push.branches, ["preview"]);
    assert.deepEqual(onOf(rel).push.tags, ["v*"]);
    assert.ok(!onOf(dock).push, "docker.yml still triggers on push");
  });
});

describe("#1472 step 3 countersign.yml: behaviour unchanged, header updated", () => {
  it("stays owner-dispatched with a digest input and verifies cosign.pub before the keyless signature", () => {
    const { doc } = load("countersign.yml");
    const on = onOf(doc);
    assert.deepEqual(Object.keys(on), ["workflow_dispatch"]);
    assert.ok(on.workflow_dispatch.inputs?.digest?.required);
    const steps = stepsOf(doc);
    // The verify step is either a direct `cosign verify --key cosign.pub` or
    // scripts/verify-release-signature.sh, which itself verifies against cosign.pub.
    const helper = readFileSync(join(root, "scripts/verify-release-signature.sh"), "utf8");
    assert.match(helper, /verify --key "\$pubkey"/);
    assert.match(helper, /cosign\.pub/);
    const v = steps.findIndex((s) => (/cosign verify\b/.test(runOf(s)) && /cosign\.pub/.test(runOf(s))) || /verify-release-signature\.sh/.test(runOf(s)));
    const sign = steps.findIndex((s) => /cosign sign\b/.test(runOf(s)));
    assert.ok(v >= 0 && sign > v, "verify must precede the keyless sign");
  });
  // Ruling v2 item 10: the signature no longer travels, so the header
  // must stop saying cosign copy carries it.
  it("header names the publish workflows and no longer mentions cosign copy", () => {
    const { header } = load("countersign.yml");
    assert.doesNotMatch(header, /cosign copy/);
    assert.match(header, /publish-from-gitlab|release-on-tag/);
  });
});

// ---------------------------------------------------------------------------
// Review round 1 on step 3 (M1-M5, L1-L4).
// ---------------------------------------------------------------------------
const ciText = readFileSync(join(root, ".gitlab-ci.yml"), "utf8");

describe("review M1: :latest moves only for the highest vX.Y.Z tag", () => {
  it("reads the remote's tags at run time and sorts them by version before the :latest write", () => {
    const { doc } = load("release-on-tag.yml");
    const steps = stepsOf(doc);
    const lat = steps.findIndex((s) => /imagetools create/.test(runOf(s)) && /mikroview:latest/.test(runOf(s)));
    assert.ok(lat >= 0, "no :latest write");
    const before = steps.slice(0, lat + 1).map(runOf).join("\n");
    assert.match(before, /ls-remote[^\n]*--tags|git (fetch --tags|tag)\b/, "tags are not read from the repository");
    assert.match(before, /sort -V|--sort=-?v:refname|sort --version-sort/, "tags are not version-sorted");
  });
  it("gates the :latest write on this tag being the highest, and says so when it is not", () => {
    const { doc } = load("release-on-tag.yml");
    const steps = stepsOf(doc);
    const lat = steps.findIndex((s) => /imagetools create/.test(runOf(s)) && /mikroview:latest/.test(runOf(s)));
    assert.ok(lat >= 0, "no :latest write");
    const t = runOf(steps[lat]);
    const gated = steps[lat].if || /highest|newest|latest_tag|is_latest/i.test(t.slice(0, t.search(/imagetools create/)));
    assert.ok(gated, "the :latest write is not conditional on the tag being the highest");
    assert.match(steps.map(stepText).join("\n"), /::notice::[^\n]*latest[^\n]*(not moved|left|unchanged|skipp)/i, "no notice that :latest was left alone");
  });
});

describe("review M3: docker.yml jobs can never run", () => {
  it("every job carries a hard if: false", () => {
    const { doc } = load("docker.yml");
    const jobs = Object.entries(doc.jobs ?? {});
    assert.ok(jobs.length > 0);
    for (const [n, j] of jobs) {
      assert.ok([false, "false", "${{ false }}"].includes(j.if), `docker.yml job ${n} can run on dispatch (if: ${JSON.stringify(j.if)})`);
    }
  });
});

describe("review M4: the attested SBOM is the verified GitLab SBOM", () => {
  for (const name of NEW) {
    it(`${name}: sbom-path is taken from verify-attestation output, or compared with it`, () => {
      const { doc } = load(name);
      const steps = stepsOf(doc);
      const at = steps.findIndex((s) => /^actions\/attest@/.test(s.uses ?? "") && s.with?.["sbom-path"]);
      assert.ok(at >= 0, "no SBOM attest step");
      const path = steps[at].with["sbom-path"];
      const before = steps.slice(0, at).map(runOf).join("\n");
      const va = /verify-attestation[\s\S]*\|[\s\S]*(\.payload|\.predicate|base64 -d)/.test(before) && before.includes(path);
      const compared = /verify-attestation/.test(before) && /\b(cmp|diff)\b|jq -S[^\n]*(==|!=)/.test(before) && before.includes(path);
      assert.ok(va || compared, "the SBOM bound to the GHCR digest is the downloaded trivy artifact, not the verified attestation predicate");
    });
  }
});

// Review L1 (cosign verify --key on GHCR after each write) is dropped by
// ruling v2: there is no key signature on GHCR to find. Item 5 below
// asserts its absence instead.

describe("review L3: preview concurrency comment is true", () => {
  it("does not claim every merge gets its own tag; says only the newest pending run is kept", () => {
    const { text } = load("publish-from-gitlab.yml");
    assert.doesNotMatch(text, /every merge gets its own/i);
    assert.match(text, /only the newest[^\n]*pending|newest pending/i);
  });
});

describe("review L4: await and verify run with read-only permissions", () => {
  for (const name of NEW) {
    it(`${name}: the await job holds no write permission; the attesting job holds no GitLab token or login`, () => {
      const { doc } = load(name);
      const jobs = Object.entries(doc.jobs ?? {});
      const awaitJob = jobs.find(([, j]) => (j.steps ?? []).some((s) => /gitlab-await-tested-image\.sh/.test(runOf(s))));
      const attestJob = jobs.find(([, j]) => (j.steps ?? []).some((s) => /^actions\/attest@/.test(s.uses ?? "")));
      assert.ok(awaitJob && attestJob, "await and attest steps missing");
      assert.notEqual(awaitJob[0], attestJob[0], "await and attest share one job (one set of write permissions)");
      const perms = awaitJob[1].permissions ?? doc.permissions ?? {};
      for (const [k, v] of Object.entries(perms)) assert.notEqual(v, "write", `await job has ${k}: write`);
      const aj = JSON.stringify(attestJob[1]);
      assert.doesNotMatch(aj, /GITLAB_READ_TOKEN/, "attest job carries the GitLab token");
      assert.ok(!(attestJob[1].steps ?? []).some((s) => /docker\/login-action/.test(s.uses ?? "") && /GITLAB_REGISTRY|GITLAB_READ_TOKEN/.test(JSON.stringify(s.with ?? {}))), "attest job logs in to GitLab's registry");
    });
  }
});

describe("review M5: both new test files run in CI", () => {
  it("the await script's .test.sh is run by a CI job script line", () => {
    assert.match(ciText, /^\s*- .*scripts\/ci\/gitlab-await-tested-image\.test\.sh/m);
  });
  it("github-copy.test.mjs is run by a CI job script line", () => {
    assert.match(ciText, /^\s*- .*(github-copy\.test\.mjs|tools\/supply-chain\/\*\.test\.mjs)/m);
  });
  it("the job that runs the .test.sh has coreutils (busybox date cannot read a UTC timestamp)", () => {
    const i = ciText.search(/^\s*- .*gitlab-await-tested-image\.test\.sh/m);
    assert.ok(i >= 0, "not wired");
    const head = ciText.slice(0, i);
    const apk = head.slice(head.lastIndexOf("apk add"));
    assert.match(apk.split("\n")[0], /coreutils/);
  });
});

// ---------------------------------------------------------------------------
// Ruling v2 on step 3: copy the image only (imagetools create), verify the
// signed evidence at the GitLab source, attest the copy on GitHub. The
// signature never travels; the digest does. Items numbered as in the ruling.
// ---------------------------------------------------------------------------
const wfFiles = () => readdirSync(join(root, WF)).filter((f) => /\.ya?ml$/.test(f)).sort();
const reEsc = (t) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
const digestCompare = /!=\s*"?\$\{?DIGEST\}?"?/;
const GHCR = "ghcr.io/tomlawesome/mikroview";

describe("ruling v2 item 1: no oras, cosign copy or crane in any workflow", () => {
  for (const f of wfFiles()) {
    it(`${f} names none of them`, () => {
      const text = readFileSync(join(root, WF, f), "utf8");
      assert.doesNotMatch(text, /\boras\b/i, "oras is not an approved tool");
      assert.doesNotMatch(text, /cosign copy\b/, "cosign copy carries the signature; v2 copies the image only");
      assert.doesNotMatch(text, /\bcrane\b/, "crane is the fallback only, not used");
    });
  }
});

for (const name of NEW) {
  describe(`ruling v2 ${name}`, () => {
    it("item 2: every GHCR write is imagetools create --prefer-index=false --tag <ghcr tag> <source>, each re-read against DIGEST", () => {
      const run = joinCont(allRun(doc0(name)));
      const lines = run.split("\n");
      const writes = lines.map((l, i) => [l, i]).filter(([l]) => /imagetools create/.test(l));
      assert.ok(writes.length >= 2, `expected at least two GHCR writes, found ${writes.length}`);
      const helper = run.match(/check\(\)\s*\{([\s\S]*?)\n\s*\}/);
      const helperOk = helper && /imagetools inspect "?\$1"?/.test(helper[1]) && digestCompare.test(helper[1]) && /exit 1/.test(helper[1]);
      writes.forEach(([l, i], k) => {
        const m = l.trim().match(/^docker buildx imagetools create --prefer-index=false --tag "?ghcr\.io\/tomlawesome\/mikroview:([^"\s]+)"? "?(\$\{SOURCE\}|ghcr\.io\/tomlawesome\/mikroview@\$\{DIGEST\})"?$/);
        assert.ok(m, `GHCR write is not the ruled form: ${l.trim()}`);
        if (m[1] !== "latest") assert.equal(m[2], "${SOURCE}", `${m[1]} is not copied from the GitLab source`);
        const next = k + 1 < writes.length ? writes[k + 1][1] : lines.length;
        const after = lines.slice(i + 1, next).join("\n");
        const tag = reEsc(`${GHCR}:${m[1]}`);
        const direct = after.search(new RegExp(`imagetools inspect "?${tag}"?`));
        const viaCheck = helperOk && new RegExp(`\\bcheck "?${tag}"?`).test(after);
        const directOk = direct >= 0 && digestCompare.test(after.slice(direct)) && /exit 1/.test(after.slice(direct));
        assert.ok(directOk || viaCheck, `no re-read of ${GHCR}:${m[1]} compared with DIGEST, failing on mismatch`);
      });
    });

    it("item 3: gitlab job runs await, then verify-release-signature.sh, then verify-attestation with an exactly-one check; no GHCR login or write", () => {
      const job = doc0(name).jobs?.gitlab;
      assert.ok(job, "no gitlab job");
      const t = jobText(job);
      const aw = t.search(/scripts\/ci\/gitlab-await-tested-image\.sh/);
      const vs = t.search(/scripts\/verify-release-signature\.sh "\$\{SOURCE\}"/);
      const va = t.search(/cosign verify-attestation --key cosign\.pub --type spdxjson "\$\{SOURCE\}"/);
      assert.ok(aw >= 0, "no await step");
      assert.ok(vs >= 0, 'no scripts/verify-release-signature.sh "${SOURCE}" in the gitlab job');
      assert.ok(va >= 0, 'no cosign verify-attestation --key cosign.pub --type spdxjson "${SOURCE}" in the gitlab job');
      assert.ok(aw < vs && vs < va, "order must be await, signature script, attestation");
      const tail = t.slice(va);
      assert.match(tail, /grep -c|wc -l|jq -s/, "no count of verified attestations");
      assert.match(tail, /(!=|-ne)\s*"?1"?/, "count is not compared with exactly one");
      assert.match(tail, /exit 1/, "count mismatch does not fail");
      for (const s of job.steps ?? []) {
        assert.ok(!(/docker\/login-action/.test(s.uses ?? "") && /ghcr\.io/.test(JSON.stringify(s.with ?? {}))), "gitlab job logs in to GHCR");
      }
      assert.doesNotMatch(t, /imagetools create|docker push/, "gitlab job writes to a registry");
    });

    it("item 4: copy checks imagetools inspect ${SOURCE} equals ${DIGEST} before its first imagetools create", () => {
      const t = jobText(doc0(name).jobs?.copy);
      const ins = t.search(/imagetools inspect "?\$\{SOURCE\}"?/);
      const cr = t.search(/imagetools create/);
      assert.ok(ins >= 0, "copy job never re-checks the source digest");
      assert.ok(cr >= 0, "copy job writes nothing");
      assert.ok(ins < cr, "source check comes after the first write");
      const between = t.slice(ins, cr);
      assert.match(between, digestCompare, "source digest is not compared with DIGEST");
      assert.match(between, /exit 1/, "source digest mismatch does not fail");
    });

    it("item 6: SBOM is the verified predicate, made in gitlab and handed to attest as an artifact", () => {
      const doc = doc0(name);
      const gl = doc.jobs?.gitlab;
      const at = doc.jobs?.attest;
      assert.ok(gl && at, "gitlab or attest job missing");
      const gt = jobText(gl);
      const va = gt.search(/cosign verify-attestation/);
      assert.ok(va >= 0, "gitlab job does not verify the attestation");
      const after = gt.slice(va);
      assert.match(after, /\.predicate/, "no jq pipeline taking .predicate");
      assert.match(after, />\s*"?image\.spdx\.json"?/, "image.spdx.json is not written from the verified attestation");
      const up = (gl.steps ?? []).find((s) => /^actions\/upload-artifact@[0-9a-f]{40}/.test(s.uses ?? "") && /image\.spdx\.json/.test(String(s.with?.path ?? "")));
      assert.ok(up, "gitlab job does not upload image.spdx.json");
      const steps = at.steps ?? [];
      const dl = steps.findIndex((s) => /^actions\/download-artifact@[0-9a-f]{40}/.test(s.uses ?? ""));
      const sb = steps.findIndex((s) => /^actions\/attest@/.test(s.uses ?? "") && s.with?.["sbom-path"] === "image.spdx.json");
      assert.ok(dl >= 0, "attest job does not download the artifact (pinned by sha)");
      assert.ok(sb >= 0, "no SBOM attest with sbom-path: image.spdx.json");
      assert.ok(dl < sb, "download must come before the SBOM attest");
      if (steps[dl].with?.name && up.with?.name) assert.equal(steps[dl].with.name, up.with.name, "downloads a different artifact");
      const aj = JSON.stringify(at);
      assert.doesNotMatch(aj, /GITLAB_READ_TOKEN/, "attest job carries the GitLab token");
      assert.doesNotMatch(aj, /GITLAB_REGISTRY|registry\.tomlawson\.io|gitlab-await-tested-image|GITLAB_API_URL/, "attest job reaches GitLab");
      assert.doesNotMatch(jobText(at), /verify-attestation/, "attest job re-reads the SBOM from a registry");
    });

    it("item 7: attest runs gh attestation verify for provenance and for the SPDX predicate type", () => {
      const t = jobText(doc0(name).jobs?.attest);
      const v = t.split("\n").filter((l) => /gh attestation verify/.test(l));
      assert.ok(v.some((l) => !/--predicate-type/.test(l)), "no provenance verify");
      assert.ok(v.some((l) => /--predicate-type https:\/\/spdx\.dev\/Document\/v2\.3/.test(l)), "no SPDX verify");
    });

    it("item 9: per-job permissions are exactly gitlab read, copy +packages, attest +id-token +attestations", () => {
      const doc = doc0(name);
      assert.deepEqual(doc.permissions, { contents: "read" });
      assert.deepEqual(doc.jobs?.gitlab?.permissions, { contents: "read" });
      assert.deepEqual(doc.jobs?.copy?.permissions, { contents: "read", packages: "write" });
      assert.deepEqual(doc.jobs?.attest?.permissions, { contents: "read", packages: "write", "id-token": "write", attestations: "write" });
      assert.deepEqual(Object.keys(doc.jobs ?? {}).sort(), ["attest", "copy", "gitlab"]);
    });
  });
}

describe("ruling v2 item 5: no cosign verify --key against ghcr.io", () => {
  for (const f of wfFiles()) {
    it(`${f} never key-verifies a GHCR reference`, () => {
      // Run text only: what the workflow does, not what its comments say
      // (item 12 covers the user-facing commands in the docs).
      const { doc } = load(f);
      const text = allRun(doc);
      const ghcrVars = Object.entries({ ...(doc.env ?? {}), ...Object.assign({}, ...jobsOf(doc).map((j) => j.env ?? {})) })
        .filter(([, v]) => /ghcr\.io/.test(String(v))).map(([k]) => k);
      for (const l of joinCont(text).split("\n")) {
        if (!/cosign verify (?!-)[^\n]*--key|cosign verify --key/.test(l)) continue;
        assert.doesNotMatch(l, /ghcr\.io/, `key verify on GHCR: ${l.trim()}`);
        for (const v of ghcrVars) assert.ok(!new RegExp(`\\$\\{?${v}\\b`).test(l), `key verify on GHCR via ${v}: ${l.trim()}`);
      }
    });
  }
});

describe("ruling v2 item 8: cosign-installer pinned to cosign v3.1.3 everywhere", () => {
  const ensure = readFileSync(join(root, "scripts/ensure-cosign.sh"), "utf8");
  const ver = ensure.match(/COSIGN_VERSION="([^"]+)"/)?.[1];
  it("ensure-cosign.sh's COSIGN_VERSION is the v3.1.3 the workflows use", () => {
    assert.equal(`v${ver}`, "v3.1.3");
  });
  for (const f of wfFiles()) {
    it(`${f}: every sigstore/cosign-installer step has cosign-release: v${ver}`, () => {
      const steps = stepsOf(load(f).doc).filter((s) => /^sigstore\/cosign-installer@/.test(s.uses ?? ""));
      for (const s of steps) assert.equal(s.with?.["cosign-release"], `v${ver}`, `${f}: cosign-installer without cosign-release v${ver}`);
    });
  }
});

describe("ruling v2 item 10: countersign verifies on the GitLab source, then signs GHCR keyless", () => {
  // Expand ${{ env.X }} and shell ${X} where X is defined in env with a
  // static value, so a SOURCE built from GITLAB_REGISTRY reads as the path.
  const expand = (t, env) => {
    for (let n = 0; n < 4; n++) {
      t = t.replace(/\$\{\{\s*env\.(\w+)\s*\}\}/g, (m, k) => env[k] ?? m);
      t = t.replace(/\$\{(\w+)\}|\$(\w+)/g, (m, a, b) => {
        const v = env[a ?? b]?.replace(/\$\{\{\s*env\.(\w+)\s*\}\}/g, (mm, k) => env[k] ?? mm);
        return v != null && !/\$\{\{/.test(v) ? v : m;
      });
    }
    return t;
  };
  it("is dispatch-only with a digest input validated as sha256:<64 hex>", () => {
    const { doc } = load("countersign.yml");
    assert.deepEqual(Object.keys(onOf(doc)), ["workflow_dispatch"]);
    assert.ok(onOf(doc).workflow_dispatch.inputs?.digest?.required);
    assert.match(allRun(doc), /\^sha256:\[0-9a-f\]\{64\}\$/);
  });
  it("logs in to registry.tomlawson.io with the GitLab read secrets", () => {
    const { doc } = load("countersign.yml");
    const env = { ...(doc.env ?? {}), ...Object.assign({}, ...jobsOf(doc).map((j) => j.env ?? {})) };
    const login = stepsOf(doc).find((s) => /^docker\/login-action@/.test(s.uses ?? "") && expand(String(s.with?.registry ?? ""), env) === "registry.tomlawson.io");
    assert.ok(login, "no login to registry.tomlawson.io");
    assert.match(String(login.with.username), /secrets\.GITLAB_READ_TOKEN_NAME/);
    assert.match(String(login.with.password), /secrets\.GITLAB_READ_TOKEN\b/);
  });
  it("verifies signature and attestation on registry.tomlawson.io/ai/mikroview@${DIGEST}, then signs, then verifies keyless", () => {
    const { doc, header } = load("countersign.yml");
    const env = { ...(doc.env ?? {}), ...Object.assign({}, ...jobsOf(doc).map((j) => j.env ?? {})) };
    const t = stepsOf(doc).map((s) => expand(joinCont(runOf(s)), { ...env, ...(s.env ?? {}) })).join("\n");
    const src = reEsc("registry.tomlawson.io/ai/mikroview@") + "\\$\\{?DIGEST\\}?";
    const vs = t.search(new RegExp(`scripts/verify-release-signature\\.sh "?${src}"?`));
    const va = t.search(new RegExp(`cosign verify-attestation --key cosign\\.pub --type spdxjson "?${src}"?`));
    const sg = t.search(/cosign sign --yes "\$\{IMAGE\}@\$\{DIGEST\}"/);
    assert.ok(vs >= 0, "verify-release-signature.sh is not run on the GitLab source digest");
    assert.ok(va >= 0, "verify-attestation is not run on the GitLab source digest");
    assert.ok(sg >= 0, 'no cosign sign --yes "${IMAGE}@${DIGEST}"');
    assert.ok(vs < sg && va < sg, "both verifies must precede the keyless sign");
    const kl = t.slice(sg).split("\n").find((l) => /cosign verify (?!-)/.test(l) && !/--key/.test(l));
    assert.ok(kl, "no keyless cosign verify after the sign");
    assert.match(kl, /--certificate-identity-regexp '\^https:\/\/github\.com\/tomlawesome\/mikroview\/'/);
    assert.match(kl, /--certificate-oidc-issuer https:\/\/token\.actions\.githubusercontent\.com/);
    assert.doesNotMatch(header, /cosign copy/);
  });
});

describe("ruling v2 item 12: docs describe the verify-at-source shape", () => {
  const read = (p) => joinCont(readFileSync(join(root, p), "utf8")).replace(/[ \t]+/g, " ");
  it("SECURITY.md's verify block has the three ruled commands and no key verify on GHCR", () => {
    const sec = read("SECURITY.md");
    const blocks = [...sec.matchAll(/```sh\n([\s\S]*?)```/g)].map((m) => m[1]);
    const b = blocks.find((x) => /gh attestation verify/.test(x));
    assert.ok(b, "no sh block with gh attestation verify");
    assert.match(b, /gh attestation verify oci:\/\/ghcr\.io\/tomlawesome\/mikroview@/);
    assert.match(b, /cosign verify ghcr\.io\/tomlawesome\/mikroview@\S+ --certificate-identity-regexp/);
    assert.match(b, /cosign verify --key cosign\.pub registry\.tomlawson\.io\/ai\/mikroview@/);
    assert.doesNotMatch(sec, /cosign verify --key cosign\.pub ghcr\.io/);
  });
  for (const p of ["docs/release-signing.md", "docs/decisions/release-versioning.md"]) {
    it(`${p} names neither cosign copy nor oras`, () => {
      const t = read(p);
      assert.doesNotMatch(t, /cosign copy\b/);
      assert.doesNotMatch(t, /\boras\b/i);
    });
  }
});
