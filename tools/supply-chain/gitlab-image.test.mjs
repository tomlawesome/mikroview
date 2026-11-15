// Tests for #1472 landing step 1: GitLab builds, signs and publishes the
// product image by digest (jobs 1-8 of the design); GitHub is untouched.
// Run with `node --test tools/supply-chain/gitlab-image.test.mjs`. They read
// the real .gitlab-ci.yml (and the two scripts it calls) as text, in the same
// node:test style as trivy-scans.test.mjs.
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";
import assert from "node:assert/strict";

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
    assert.match(b, /needs:\s*\[\s*\]/);
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
  it("is the only docker build/buildx build in the pipeline", () => {
    const n = ci.split("\n").filter((l) => !/^\s*#/.test(l) && /docker (buildx )?build\b/.test(l)).length;
    assert.equal(n, 1, `expected one build, found ${n} docker build lines`);
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
  it("test:container's reuse gate is skipped on .publish_ref", () => {
    assert.match(job("test:container"), /publish_ref/);
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

describe("#1472 landing step 1 leaves GitHub workflows alone", () => {
  it("no publish-from-gitlab.yml or release-on-tag.yml yet", () => {
    for (const f of ["publish-from-gitlab.yml", "release-on-tag.yml"]) {
      assert.throws(() => read(`.github/workflows/${f}`), /ENOENT/);
    }
  });
});
