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
import { existsSync, readFileSync } from "node:fs";
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

  describe(`#1472 step 3 ${name}: verify before copy`, () => {
    it("runs await, then cosign verify --key cosign.pub, then the first GHCR write", () => {
      const { doc } = load(name);
      const steps = stepsOf(doc);
      const aw = indexOfStep(steps, /gitlab-await-tested-image\.sh/);
      const vf = steps.findIndex((s) => /cosign verify\b/.test(runOf(s)) && /cosign\.pub/.test(runOf(s)));
      const wr = steps.findIndex((s) => /cosign copy\b|imagetools create|docker push|crane (copy|push|tag)/.test(runOf(s)));
      assert.ok(aw >= 0, "no await step");
      assert.ok(vf >= 0, "no cosign verify --key cosign.pub step");
      assert.ok(wr >= 0, "no GHCR write step");
      assert.ok(aw < vf, "await must come before verify");
      assert.ok(vf < wr, "verify must come before any GHCR write");
    });
    it("verify cannot be skipped or ignored", () => {
      const { doc } = load(name);
      const s = stepsOf(doc).find((x) => /cosign verify\b/.test(runOf(x)) && /cosign\.pub/.test(runOf(x)));
      assert.ok(s, "no verify step");
      assert.ok(!s["continue-on-error"], "verify has continue-on-error");
      assert.doesNotMatch(runOf(s), /\|\|\s*(true|:)\b/);
    });
    it("copies with cosign copy by digest, never crane, to ghcr.io/tomlawesome/mikroview", () => {
      const { doc } = load(name);
      const run = allRun(doc);
      assert.match(run, /cosign copy\b/);
      assert.match(run, /ghcr\.io\/tomlawesome\/mikroview/);
      assert.doesNotMatch(run, /\bcrane\b/);
    });
    it("re-reads each GHCR tag after writing it and fails unless it is the GitLab digest", () => {
      const { doc } = load(name);
      const steps = stepsOf(doc);
      const c = steps.findIndex((s) => /cosign copy\b/.test(runOf(s)));
      assert.ok(c >= 0, "no copy step");
      const after = steps.slice(c).map(runOf).join("\n");
      const copyAt = after.search(/cosign copy\b/);
      const tail = after.slice(copyAt);
      assert.match(tail, /imagetools inspect|crane digest|manifest inspect|skopeo inspect|cosign triangulate/, "no re-read after copy");
      assert.match(tail, /!=|!==|\[\[ .* == .* \]\]|\bdiff\b|exit 1/, "no comparison/failure after re-read");
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
    const moved = steps.findIndex((s) => /mikroview:preview(["'\s]|$)/m.test(runOf(s)) && /cosign copy/.test(runOf(s)));
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
    assert.match(allRun(doc), /cosign copy[^\n]*mikroview:\$\{?(GITHUB_REF_NAME|[A-Za-z_]*TAG[A-Za-z_]*|[A-Za-z_]*VERSION[A-Za-z_]*)/i);
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
    const v = steps.findIndex((s) => /cosign verify\b/.test(runOf(s)) && /cosign\.pub/.test(runOf(s)));
    const sign = steps.findIndex((s) => /cosign sign\b/.test(runOf(s)));
    assert.ok(v >= 0 && sign > v, "verify must precede the keyless sign");
  });
  it("header says the key-based signature is now copied from GitLab by cosign copy", () => {
    const { header } = load("countersign.yml");
    assert.match(header, /cosign copy/);
    assert.match(header, /publish-from-gitlab|release-on-tag/);
  });
});
