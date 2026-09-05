// Tests for pins-policy.mjs. Run with `node --test
// tools/supply-chain/pins-policy.test.mjs` -- same reasoning as
// tools/licenses/licence-policy.test.mjs: nothing in this tree runs tests
// under tools/, so this uses node:test, which needs nothing installed.
//
// These exercise the pure extraction and comparison functions against
// fixture text, not the real repository tree -- collectRepositoryPins()
// (which reads .github/workflows, Dockerfile and .gitlab-ci.yml for real)
// is exercised for real by running the script directly against this repo,
// which is how #711's policy file was built and checked in the first
// place.
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, describe, it } from "node:test";
import assert from "node:assert/strict";

import { diffPolicy, extractActionPins, extractImagePins, loadPolicy } from "./pins-policy.mjs";

const scratchDirs = [];
function scratchDir() {
  const dir = mkdtempSync(join(tmpdir(), "mikroview-pins-policy-"));
  scratchDirs.push(dir);
  return dir;
}
after(() => {
  for (const dir of scratchDirs) rmSync(dir, { recursive: true, force: true });
});

describe("extractActionPins", () => {
  it("reads a uses: pin's name, commit and version comment", () => {
    const pins = extractActionPins({
      "workflow.yml": "      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1\n",
    });
    assert.deepEqual(pins.get("actions/checkout"), {
      commit: "3d3c42e5aac5ba805825da76410c181273ba90b1",
      version: "v7.0.1",
      locations: new Set(["workflow.yml"]),
    });
  });

  it("tracks the same action name across files as one pin, unioning locations", () => {
    const pins = extractActionPins({
      "a.yml": "uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1\n",
      "b.yml": "uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1\n",
    });
    assert.equal(pins.size, 1);
    assert.deepEqual(pins.get("actions/checkout").locations, new Set(["a.yml", "b.yml"]));
  });

  it("keeps two actions from the same repo separate when their paths differ", () => {
    const pins = extractActionPins({
      "codeql.yml": [
        "uses: github/codeql-action/init@cdf488f595d80d6e07e03d4674febd5ab45fa938 # v4.37.9",
        "uses: github/codeql-action/analyze@cdf488f595d80d6e07e03d4674febd5ab45fa938 # v4.37.9",
      ].join("\n"),
    });
    assert.equal(pins.size, 2);
    assert.ok(pins.has("github/codeql-action/init"));
    assert.ok(pins.has("github/codeql-action/analyze"));
  });

  it("ignores a uses: line that is not pinned to a full commit SHA", () => {
    const pins = extractActionPins({ "workflow.yml": "uses: actions/checkout@v7\n" });
    assert.equal(pins.size, 0);
  });
});

describe("extractImagePins", () => {
  it("reads a Dockerfile FROM line, dropping any AS stage alias", () => {
    const pins = extractImagePins({
      dockerfile: { file: "Dockerfile", text: "FROM node:26-alpine AS frontend\n" },
    });
    assert.ok(pins.has("node:26-alpine"));
    assert.deepEqual(pins.get("node:26-alpine"), new Set(["Dockerfile"]));
  });

  it("reads a one-line image: pin", () => {
    const pins = extractImagePins({
      gitlabCi: { file: ".gitlab-ci.yml", text: "test:go:\n  image: golang:1.27\n" },
    });
    assert.ok(pins.has("golang:1.27"));
  });

  it("reads a block-form image: pin (image: / name: / entrypoint:)", () => {
    const pins = extractImagePins({
      gitlabCi: {
        file: ".gitlab-ci.yml",
        text: ["security:gosec:", "  image:", "    name: ghcr.io/securego/gosec:2.29.0", "    entrypoint: [\"\"]"].join(
          "\n",
        ),
      },
    });
    assert.ok(pins.has("ghcr.io/securego/gosec:2.29.0"));
  });

  it("reads a services: - name: service-container pin", () => {
    const pins = extractImagePins({
      gitlabCi: {
        file: ".gitlab-ci.yml",
        text: ["test:postgres:", "  services:", "    - name: postgres:18-alpine", "      alias: postgres"].join("\n"),
      },
    });
    assert.ok(pins.has("postgres:18-alpine"));
  });
});

describe("diffPolicy", () => {
  const basePolicy = () => ({
    ciActions: [
      {
        name: "actions/checkout",
        commit: "3d3c42e5aac5ba805825da76410c181273ba90b1",
        version: "v7.0.1",
      },
    ],
    containerImages: [{ reference: "golang:1.27" }],
  });

  it("reports nothing when the policy matches reality", () => {
    const actionPins = new Map([
      ["actions/checkout", { commit: "3d3c42e5aac5ba805825da76410c181273ba90b1", version: "v7.0.1" }],
    ]);
    const imagePins = new Map([["golang:1.27", new Set([".gitlab-ci.yml"])]]);
    assert.deepEqual(diffPolicy(basePolicy(), { actionPins, imagePins }), []);
  });

  it("flags a pin present in the repository but not recorded in the policy", () => {
    const actionPins = new Map([
      ["actions/checkout", { commit: "3d3c42e5aac5ba805825da76410c181273ba90b1", version: "v7.0.1" }],
      ["actions/setup-go", { commit: "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e", version: "v7.0.0" }],
    ]);
    const imagePins = new Map([["golang:1.27", new Set([".gitlab-ci.yml"])]]);
    const problems = diffPolicy(basePolicy(), { actionPins, imagePins });
    assert.ok(problems.some((p) => p.includes("actions/setup-go") && p.includes("not recorded")));
  });

  it("flags a commit that has moved since the policy was written", () => {
    const actionPins = new Map([
      ["actions/checkout", { commit: "0000000000000000000000000000000000000000", version: "v7.0.1" }],
    ]);
    const imagePins = new Map([["golang:1.27", new Set([".gitlab-ci.yml"])]]);
    const problems = diffPolicy(basePolicy(), { actionPins, imagePins });
    assert.ok(problems.some((p) => p.includes("actions/checkout") && p.includes("0000000000000000000000000000000000000000")));
  });

  it("flags a version comment that disagrees with the policy's recorded version", () => {
    const actionPins = new Map([
      ["actions/checkout", { commit: "3d3c42e5aac5ba805825da76410c181273ba90b1", version: "v8.0.0" }],
    ]);
    const imagePins = new Map([["golang:1.27", new Set([".gitlab-ci.yml"])]]);
    const problems = diffPolicy(basePolicy(), { actionPins, imagePins });
    assert.ok(problems.some((p) => p.includes("v8.0.0")));
  });

  it("flags a policy entry for a pin that has since been removed", () => {
    const actionPins = new Map();
    const imagePins = new Map();
    const problems = diffPolicy(basePolicy(), { actionPins, imagePins });
    assert.ok(problems.some((p) => p.includes("actions/checkout") && p.includes("no longer pinned")));
    assert.ok(problems.some((p) => p.includes("golang:1.27") && p.includes("no longer pinned")));
  });

  it("flags a container image pinned but not recorded", () => {
    const actionPins = new Map([
      ["actions/checkout", { commit: "3d3c42e5aac5ba805825da76410c181273ba90b1", version: "v7.0.1" }],
    ]);
    const imagePins = new Map([
      ["golang:1.27", new Set([".gitlab-ci.yml"])],
      ["node:22-alpine", new Set([".gitlab-ci.yml"])],
    ]);
    const problems = diffPolicy(basePolicy(), { actionPins, imagePins });
    assert.ok(problems.some((p) => p.includes("node:22-alpine") && p.includes("not recorded")));
  });
});

describe("loadPolicy", () => {
  it("loads the real policy file and it matches this repository's actual pins", async () => {
    // The one place this file touches the real tree: it is the assertion
    // that ships with #711 -- a policy that has never been checked
    // against reality proves nothing (testing-and-ci's rule on absence
    // tests applies just as much to a "policy" file as to a directory).
    const { collectRepositoryPins } = await import("./pins-policy.mjs");
    const policy = loadPolicy("supply-chain/pins-policy.json");
    const problems = diffPolicy(policy, collectRepositoryPins());
    assert.deepEqual(problems, []);
  });

  it("rejects a policy with an unsupported schema version", () => {
    const dir = scratchDir();
    const path = join(dir, "policy.json");
    writeFileSync(path, JSON.stringify({ schemaVersion: 2, ciActions: [], containerImages: [] }), "utf8");
    assert.throws(() => loadPolicy(path), /schemaVersion must be 1/);
  });

  it("rejects a policy missing the ciActions or containerImages arrays", () => {
    const dir = scratchDir();
    const path = join(dir, "policy.json");
    writeFileSync(path, JSON.stringify({ schemaVersion: 1, ciActions: [] }), "utf8");
    assert.throws(() => loadPolicy(path), /ciActions and containerImages/);
  });

  it("reports a missing file rather than an unhandled read error", () => {
    assert.throws(() => loadPolicy("/nonexistent/pins-policy.json"), /is missing/);
  });
});
