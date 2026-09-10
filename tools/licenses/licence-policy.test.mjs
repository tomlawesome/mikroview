// Tests for licence-policy.mjs. Run with `node --test
// tools/licenses/licence-policy.test.mjs`: this tree has no test runner that
// reaches tools/ (frontend/'s vitest only looks under frontend/, and
// tools/screenshots/package.json has no test runner at all), so this uses
// node:test, which needs nothing installed.
//
// These tests exercise the pure functions against fixture package lists --
// never the real npmPackages(), which shells out to a Vite build. That
// build is exercised for real by the failing-proof and the real run
// recorded on the issue, not by this file.
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  checkShippedPackages,
  isLicenceAllowed,
  licenceExpressionFromDeclared,
  loadPolicy,
  parsePurlException,
} from "./licence-policy.mjs";

// Each test writing a policy file builds its own tree under a fresh temp
// directory; all of them are removed when this file's run ends.
const scratchDirs = [];
function scratchDir() {
  const dir = mkdtempSync(join(tmpdir(), "mikroview-licence-policy-"));
  scratchDirs.push(dir);
  return dir;
}
after(() => {
  for (const dir of scratchDirs) rmSync(dir, { recursive: true, force: true });
});

function writePolicy(dir, { allowLicenses, allowDependenciesLicenses = [] }) {
  const path = join(dir, "licence-policy.yml");
  const lines = [
    "allow-licenses:",
    ...allowLicenses.map((id) => `  - ${id}`),
    "allow-dependencies-licenses:",
    ...allowDependenciesLicenses.map((purl) => `  - ${purl}`),
  ];
  writeFileSync(path, lines.join("\n") + "\n", "utf8");
  return path;
}

describe("loadPolicy", () => {
  it("reads the allow-licenses and allow-dependencies-licenses lists, ignoring comments", () => {
    const dir = scratchDir();
    const policyPath = join(dir, "policy.yml");
    writeFileSync(
      policyPath,
      [
        "# header comment",
        "allow-licenses:",
        "  - MIT",
        "  - ISC # inline comment",
        "# a comment above the exceptions",
        "allow-dependencies-licenses:",
        "  - pkg:npm/@scope/pkg@1.3.0",
        "",
      ].join("\n"),
      "utf8",
    );
    const policy = loadPolicy(policyPath);
    assert.deepEqual(policy.allowLicences, new Set(["MIT", "ISC"]));
    assert.deepEqual(policy.allowExactExceptions, new Set(["@scope/pkg@1.3.0"]));
  });

  it("reads an empty allow-dependencies-licenses: [] list", () => {
    const dir = scratchDir();
    const policyPath = join(dir, "policy.yml");
    writeFileSync(policyPath, ["allow-licenses:", "  - MIT", "allow-dependencies-licenses: []", ""].join("\n"), "utf8");
    const policy = loadPolicy(policyPath);
    assert.deepEqual(policy.allowLicences, new Set(["MIT"]));
    assert.deepEqual(policy.allowExactExceptions, new Set());
  });
});

describe("parsePurlException", () => {
  it("splits a scoped package name from its exact version", () => {
    assert.deepEqual(parsePurlException("pkg:npm/@scope/pkg@1.3.0"), {
      name: "@scope/pkg",
      version: "1.3.0",
    });
    assert.deepEqual(parsePurlException("pkg:npm/left-pad@1.0.0"), {
      name: "left-pad",
      version: "1.0.0",
    });
  });

  it("rejects a PURL with no version", () => {
    assert.throws(() => parsePurlException("pkg:npm/left-pad"));
  });
});

describe("isLicenceAllowed", () => {
  const allow = new Set(["MIT", "ISC", "Apache-2.0"]);

  it("passes a single allowed licence", () => {
    assert.equal(isLicenceAllowed("MIT", allow), true);
  });

  it("passes an OR expression when at least one branch is allowed", () => {
    assert.equal(isLicenceAllowed("ISC OR GPL-3.0-only", allow), true);
    assert.equal(isLicenceAllowed("GPL-3.0-only OR ISC", allow), true);
  });

  it("fails an AND expression when any part is disallowed", () => {
    assert.equal(isLicenceAllowed("MIT AND GPL-3.0-only", allow), false);
  });

  it("passes an AND expression only when every part is allowed", () => {
    assert.equal(isLicenceAllowed("MIT AND ISC", allow), true);
  });

  it("honours parentheses", () => {
    assert.equal(isLicenceAllowed("(MIT OR GPL-3.0-only) AND ISC", allow), true);
    assert.equal(isLicenceAllowed("MIT OR (GPL-3.0-only AND ISC)", allow), true);
    assert.equal(isLicenceAllowed("(MIT AND GPL-3.0-only) OR BSD-3-Clause", allow), false);
  });

  it("fails closed on missing, empty, UNLICENSED and SEE LICENSE IN", () => {
    assert.equal(isLicenceAllowed(undefined, allow), false);
    assert.equal(isLicenceAllowed("", allow), false);
    assert.equal(isLicenceAllowed("   ", allow), false);
    assert.equal(isLicenceAllowed("UNLICENSED", allow), false);
    assert.equal(isLicenceAllowed("SEE LICENSE IN LICENSE.txt", allow), false);
  });

  it("fails closed on an unparseable expression", () => {
    assert.equal(isLicenceAllowed("(MIT", allow), false);
    assert.equal(isLicenceAllowed("MIT AND", allow), false);
  });
});

describe("licenceExpressionFromDeclared", () => {
  it("passes through a plain string", () => {
    assert.equal(licenceExpressionFromDeclared("MIT"), "MIT");
  });

  it("reads the legacy { type } object", () => {
    assert.equal(licenceExpressionFromDeclared({ type: "ISC", url: "https://example.com" }), "ISC");
  });

  it("ORs the legacy licenses array", () => {
    assert.equal(
      licenceExpressionFromDeclared([{ type: "MIT" }, { type: "Apache-2.0" }]),
      "MIT OR Apache-2.0",
    );
  });

  it("returns an empty string for anything else", () => {
    assert.equal(licenceExpressionFromDeclared(undefined), "");
    assert.equal(licenceExpressionFromDeclared(null), "");
    assert.equal(licenceExpressionFromDeclared([]), "");
  });
});

describe("checkShippedPackages", () => {
  it("passes an allowed package list", () => {
    const policyPath = writePolicy(scratchDir(), { allowLicenses: ["MIT", "ISC"] });
    const packages = [
      { name: "mit-pkg", version: "1.0.0", license: "MIT" },
      { name: "isc-pkg", version: "2.0.0", license: "ISC" },
    ];
    const result = checkShippedPackages({ packages, policyPath });
    assert.equal(result.checked, 2);
    assert.deepEqual(result.offending, []);
  });

  it("fails a GPL-3.0-only package", () => {
    const policyPath = writePolicy(scratchDir(), { allowLicenses: ["MIT"] });
    const packages = [{ name: "gpl-pkg", version: "2.0.0", license: "GPL-3.0-only" }];
    const result = checkShippedPackages({ packages, policyPath });
    assert.equal(result.checked, 1);
    assert.deepEqual(result.offending, [{ name: "gpl-pkg", version: "2.0.0", licence: "GPL-3.0-only" }]);
  });

  it("fails a package with no licence field", () => {
    const policyPath = writePolicy(scratchDir(), { allowLicenses: ["MIT"] });
    const packages = [{ name: "no-licence-pkg", version: "1.0.0", license: undefined }];
    const result = checkShippedPackages({ packages, policyPath });
    assert.deepEqual(result.offending, [{ name: "no-licence-pkg", version: "1.0.0", licence: "" }]);
  });

  it("passes an exact-version dependency exception", () => {
    const policyPath = writePolicy(scratchDir(), {
      allowLicenses: ["MIT"],
      allowDependenciesLicenses: ["pkg:npm/@scope/pkg@1.3.0"],
    });
    const packages = [{ name: "@scope/pkg", version: "1.3.0", license: "LGPL-3.0-or-later" }];
    const result = checkShippedPackages({ packages, policyPath });
    assert.deepEqual(result.offending, []);
  });

  it("fails the same exception name at a different version", () => {
    const policyPath = writePolicy(scratchDir(), {
      allowLicenses: ["MIT"],
      allowDependenciesLicenses: ["pkg:npm/@scope/pkg@1.3.0"],
    });
    const packages = [{ name: "@scope/pkg", version: "1.4.0", license: "LGPL-3.0-or-later" }];
    const result = checkShippedPackages({ packages, policyPath });
    assert.deepEqual(result.offending, [
      { name: "@scope/pkg", version: "1.4.0", licence: "LGPL-3.0-or-later" },
    ]);
  });

  it("passes an OR expression with one allowed branch", () => {
    const policyPath = writePolicy(scratchDir(), { allowLicenses: ["ISC"] });
    const packages = [{ name: "or-pkg", version: "1.0.0", license: "ISC OR GPL-3.0-only" }];
    const result = checkShippedPackages({ packages, policyPath });
    assert.deepEqual(result.offending, []);
  });

  it("fails an AND expression with one disallowed part", () => {
    const policyPath = writePolicy(scratchDir(), { allowLicenses: ["MIT"] });
    const packages = [{ name: "and-pkg", version: "1.0.0", license: "MIT AND GPL-3.0-only" }];
    const result = checkShippedPackages({ packages, policyPath });
    assert.deepEqual(result.offending, [
      { name: "and-pkg", version: "1.0.0", licence: "MIT AND GPL-3.0-only" },
    ]);
  });

  it("checks a package declared with the legacy licenses array", () => {
    const policyPath = writePolicy(scratchDir(), { allowLicenses: ["MIT"] });
    const packages = [{ name: "legacy-pkg", version: "1.0.0", license: [{ type: "GPL-3.0-only" }] }];
    const result = checkShippedPackages({ packages, policyPath });
    assert.deepEqual(result.offending, [
      { name: "legacy-pkg", version: "1.0.0", licence: "GPL-3.0-only" },
    ]);
  });
});
