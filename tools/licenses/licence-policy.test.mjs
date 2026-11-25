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
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { after, describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  checkBundledContent,
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

// --- looking inside shipped packages (#1432) ------------------------------
//
// A package's declared licence covers its own code, not everything it
// carries. These drive the CLI with fixture inventories (--packages-json for
// npm, --go-packages-json for Go) rather than importing the new functions,
// so the same file runs unchanged against the gate before #1432 -- where
// every one of these failed, because that gate never looked inside a
// package at all.
const CLI = fileURLToPath(new URL("./licence-policy.mjs", import.meta.url));

function runGate(dir, { npm = [], go, policyLines }) {
  const policyPath = join(dir, "policy.yml");
  writeFileSync(policyPath, policyLines.join("\n") + "\n", "utf8");
  const npmPath = join(dir, "npm.json");
  writeFileSync(npmPath, JSON.stringify(npm), "utf8");
  const args = [CLI, "--policy", policyPath, "--packages-json", npmPath];
  if (go !== undefined) {
    const goPath = join(dir, "go.json");
    writeFileSync(goPath, JSON.stringify(go), "utf8");
    args.push("--go-packages-json", goPath);
  }
  const result = spawnSync(process.execPath, args, { encoding: "utf8" });
  return { status: result.status, output: `${result.stdout}\n${result.stderr}` };
}

function writeFiles(root, files) {
  for (const [rel, content] of Object.entries(files)) {
    const path = join(root, rel);
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, content, "utf8");
  }
}

const BASE_POLICY = ["allow-licenses:", "  - MIT", "allow-dependencies-licenses: []"];

describe("vendored npm packages (bundleDependencies)", () => {
  it("fails a bundled dependency whose own licence is not allowed, naming its carrier", () => {
    const dir = scratchDir();
    const carrier = join(dir, "node_modules", "carrier");
    writeFiles(carrier, {
      "package.json": JSON.stringify({ name: "carrier", version: "1.0.0", license: "MIT", bundleDependencies: ["inner"] }),
      "node_modules/inner/package.json": JSON.stringify({ name: "inner", version: "2.0.0", license: "GPL-3.0-only" }),
    });
    const { status, output } = runGate(dir, {
      npm: [{ name: "carrier", version: "1.0.0", license: "MIT", dir: carrier }],
      policyLines: BASE_POLICY,
    });
    assert.equal(status, 1, output);
    assert.match(output, /inner@2\.0\.0 \(vendored in carrier@1\.0\.0\)\s+GPL-3\.0-only/u);
  });

  it("passes a bundled dependency under an allowed licence", () => {
    const dir = scratchDir();
    const carrier = join(dir, "node_modules", "carrier");
    writeFiles(carrier, {
      "package.json": JSON.stringify({ name: "carrier", version: "1.0.0", license: "MIT", bundleDependencies: ["inner"] }),
      "node_modules/inner/package.json": JSON.stringify({ name: "inner", version: "2.0.0", license: "MIT" }),
    });
    const { status, output } = runGate(dir, {
      npm: [{ name: "carrier", version: "1.0.0", license: "MIT", dir: carrier }],
      policyLines: BASE_POLICY,
    });
    assert.equal(status, 0, output);
    assert.match(output, /1 vendored package\(s\) checked/u);
  });
});

describe("bundled assets in shipped npm packages", () => {
  function fontPackage(dir, version = "1.0.0") {
    const pkgDir = join(dir, "node_modules", "fontpkg");
    writeFiles(pkgDir, {
      "package.json": JSON.stringify({ name: "fontpkg", version, license: "MIT" }),
      "index.js": "export {};\n",
      "fonts/inter.woff2": "not really a font",
      "fonts/inter.css": "@font-face { font-family: Inter; }",
    });
    return [{ name: "fontpkg", version, license: "MIT", dir: pkgDir }];
  }

  it("fails a font or stylesheet with no recorded review", () => {
    const dir = scratchDir();
    const { status, output } = runGate(dir, { npm: fontPackage(dir), policyLines: BASE_POLICY });
    assert.equal(status, 1, output);
    assert.match(output, /no bundled-assets review/u);
    assert.match(output, /pkg:npm\/fontpkg@1\.0\.0 fonts\/ \(2 file\(s\)/u);
  });

  it("passes when a bundled-assets review covers them, and says so", () => {
    const dir = scratchDir();
    const { status, output } = runGate(dir, {
      npm: fontPackage(dir),
      policyLines: [...BASE_POLICY, "bundled-assets:", "  - pkg:npm/fontpkg@1.0.0 fonts/ MIT"],
    });
    assert.equal(status, 0, output);
    assert.match(output, /2 bundled file\(s\) passed by recorded review/u);
  });

  it("re-triggers review when the carrying package's version moves on", () => {
    const dir = scratchDir();
    const { status, output } = runGate(dir, {
      npm: fontPackage(dir, "1.1.0"),
      policyLines: [...BASE_POLICY, "bundled-assets:", "  - pkg:npm/fontpkg@1.0.0 fonts/ MIT"],
    });
    assert.equal(status, 1, output);
    assert.match(output, /pkg:npm\/fontpkg@1\.1\.0 fonts\/ \(2 file\(s\)/u);
  });

  it("fails a review entry that matches no shipped file as stale", () => {
    const dir = scratchDir();
    const { status, output } = runGate(dir, {
      npm: fontPackage(dir),
      policyLines: [
        ...BASE_POLICY,
        "bundled-assets:",
        "  - pkg:npm/fontpkg@1.0.0 fonts/ MIT",
        "  - pkg:npm/fontpkg@1.0.0 images/ MIT",
      ],
    });
    assert.equal(status, 1, output);
    assert.match(output, /stale bundled-assets review/u);
    assert.match(output, /pkg:npm\/fontpkg@1\.0\.0 images\/ MIT/u);
  });
});

describe("other projects' code inlined into a shipped npm package", () => {
  function bannerPackage(dir, banner) {
    const pkgDir = join(dir, "node_modules", "bundler");
    writeFiles(pkgDir, {
      "package.json": JSON.stringify({ name: "bundler", version: "3.0.0", license: "MIT" }),
      "LICENSE": "MIT License\n\nCopyright (c) 2020 Jane Maintainer\n",
      "dist/bundle.js": `${banner}\nexport const x = 1;\n`,
    });
    return [{ name: "bundler", version: "3.0.0", license: "MIT", dir: pkgDir, shippedFiles: [{ path: "dist/bundle.js" }] }];
  }

  it("fails a shipped file carrying another project's licence banner", () => {
    const dir = scratchDir();
    const npm = bannerPackage(dir, "/*! otherlib v4.2 | (c) 2015 Other Corp | Apache-2.0 */");
    const { status, output } = runGate(dir, { npm, policyLines: BASE_POLICY });
    assert.equal(status, 1, output);
    assert.match(output, /pkg:npm\/bundler@3\.0\.0 dist\/ \(1 file\(s\): inlined code with another holder's copyright: other corp/u);
  });

  it("ignores the package's own copyright banner", () => {
    const dir = scratchDir();
    const npm = bannerPackage(dir, "/**\n * Copyright (c) 2021 Jane Maintainer\n * Licensed under the MIT license\n */");
    const { status, output } = runGate(dir, { npm, policyLines: BASE_POLICY });
    assert.equal(status, 0, output);
  });

  it("passes the inlined file once it is reviewed by exact path", () => {
    const dir = scratchDir();
    const npm = bannerPackage(dir, "/*! otherlib v4.2 | (c) 2015 Other Corp | Apache-2.0 */");
    const { status, output } = runGate(dir, {
      npm,
      policyLines: [...BASE_POLICY, "bundled-assets:", "  - pkg:npm/bundler@3.0.0 dist/bundle.js MIT"],
    });
    assert.equal(status, 0, output);
  });
});

describe("Go modules: vendored code and embedded assets", () => {
  function goModule(dir, files) {
    const moduleDir = join(dir, "gomod", "example.com", "lib@v1.2.3");
    writeFiles(moduleDir, { LICENSE: "MIT License\n", "go.mod": "module example.com/lib\n", ...files });
    return moduleDir;
  }
  const goPkg = (moduleDir, rel, embedFiles = []) => ({
    importPath: `example.com/lib${rel ? `/${rel}` : ""}`,
    dir: rel ? join(moduleDir, rel) : moduleDir,
    module: "example.com/lib",
    version: "v1.2.3",
    moduleDir,
    embedFiles,
  });

  it("fails vendored code under third_party/ with no licence file of its own", () => {
    const dir = scratchDir();
    const moduleDir = goModule(dir, { "third_party/fastmath/fast.go": "package fastmath\n" });
    const { status, output } = runGate(dir, {
      go: [goPkg(moduleDir, ""), goPkg(moduleDir, "third_party/fastmath")],
      policyLines: BASE_POLICY,
    });
    assert.equal(status, 1, output);
    assert.match(output, /pkg:golang\/example\.com\/lib@v1\.2\.3 third_party\/fastmath\/ \(1 file\(s\): vendored code with no licence file of its own/u);
  });

  it("leaves vendored code that carries its own licence file to go-licenses", () => {
    const dir = scratchDir();
    const moduleDir = goModule(dir, {
      "third_party/fastmath/fast.go": "package fastmath\n",
      "third_party/fastmath/LICENSE": "BSD 3-Clause\n",
    });
    const { status, output } = runGate(dir, {
      go: [goPkg(moduleDir, "third_party/fastmath")],
      policyLines: BASE_POLICY,
    });
    assert.equal(status, 0, output);
    assert.match(output, /example\.com\/lib\/third_party\/fastmath .*third_party\/fastmath\/LICENSE/u);
  });

  it("fails an embedded web asset with no recorded review, and passes it once reviewed", () => {
    const dir = scratchDir();
    const moduleDir = goModule(dir, { "ui/static/logo.png": "png", "ui/static/app.css": "body{}", "ui/ui.go": "package ui\n" });
    const pkgs = [goPkg(moduleDir, "ui", ["static/logo.png", "static/app.css"])];
    const failing = runGate(dir, { go: pkgs, policyLines: BASE_POLICY });
    assert.equal(failing.status, 1, failing.output);
    assert.match(failing.output, /pkg:golang\/example\.com\/lib@v1\.2\.3 ui\/static\/ \(2 file\(s\): embedded asset/u);

    const passing = runGate(dir, {
      go: pkgs,
      policyLines: [...BASE_POLICY, "bundled-assets:", "  - pkg:golang/example.com/lib@v1.2.3 ui/static/ MIT"],
    });
    assert.equal(passing.status, 0, passing.output);
  });
});

describe("loadPolicy bundled-assets licence check", () => {
  function loadText(lines) {
    const dir = scratchDir();
    const policyPath = join(dir, "policy.yml");
    writeFileSync(policyPath, lines.join("\n") + "\n", "utf8");
    return loadPolicy(policyPath);
  }
  const ENTRY = "  - pkg:npm/qrcode@1.5.4 lib/core/qrcode.js";

  it("throws, naming the entry, when a bundled-assets licence is not allowed", () => {
    assert.throws(
      () => loadText(["allow-licenses:", "  - MIT", "bundled-assets:", `${ENTRY} GPL-3.0`]),
      (err) => err instanceof Error && /pkg:npm\/qrcode@1\.5\.4/u.test(err.message) && /lib\/core\/qrcode\.js/u.test(err.message),
    );
  });

  it("throws when an AND expression has a disallowed part", () => {
    assert.throws(
      () => loadText(["allow-licenses:", "  - MIT", "bundled-assets:", `${ENTRY} MIT AND GPL-3.0`]),
      /qrcode/u,
    );
  });

  it("loads when an OR expression has one allowed part", () => {
    const policy = loadText(["allow-licenses:", "  - MIT", "bundled-assets:", `${ENTRY} MIT OR GPL-3.0`]);
    assert.equal(policy.assetReviews.length, 1);
  });

  it("loads an allowed entry when allow-licenses comes after bundled-assets", () => {
    const policy = loadText(["bundled-assets:", `${ENTRY} MIT`, "allow-licenses:", "  - MIT"]);
    assert.equal(policy.assetReviews.length, 1);
  });

  it("throws for a disallowed entry even when allow-licenses comes after bundled-assets", () => {
    assert.throws(
      () => loadText(["bundled-assets:", `${ENTRY} GPL-3.0`, "allow-licenses:", "  - MIT"]),
      /qrcode/u,
    );
  });
});

// --- #1466: licence gate gaps ---------------------------------------------

function bundledPolicy(dir) {
  return writePolicy(dir, { allowLicenses: ["MIT"] });
}

function goFixture(dir, embedFiles) {
  const moduleDir = join(dir, "mod");
  mkdirSync(moduleDir, { recursive: true });
  for (const f of embedFiles) writeFileSync(join(moduleDir, f), "x", "utf8");
  return { importPath: "example.com/mod", dir: moduleDir, module: "example.com/mod", version: "v1.0.0", moduleDir, embedFiles };
}

function npmFixture(dir, files) {
  const pkgDir = join(dir, "pkg");
  mkdirSync(pkgDir, { recursive: true });
  writeFileSync(join(pkgDir, "package.json"), JSON.stringify({ name: "pkg", version: "1.0.0", license: "MIT" }), "utf8");
  for (const f of files) writeFileSync(join(pkgDir, f), "x", "utf8");
  return { name: "pkg", version: "1.0.0", dir: pkgDir };
}

describe("Go //go:embed findings (#1466)", () => {
  for (const name of ["data.json", "notes.txt", "blob.bin", "NOEXT"]) {
    it(`flags an embedded ${name} whatever its extension`, () => {
      const dir = scratchDir();
      const result = checkBundledContent({ go: [goFixture(dir, [name])], policyPath: bundledPolicy(dir) });
      assert.deepEqual(result.unreviewed.map((f) => f.path), [name]);
    });
  }
});

describe("npm bundled-asset extensions (#1466)", () => {
  for (const ext of [".wasm", ".bin", ".dat", ".data", ".db", ".mmdb", ".xml", ".gz", ".zip", ".tar", ".tgz"]) {
    it(`flags a shipped ${ext} file`, () => {
      const dir = scratchDir();
      const result = checkBundledContent({ npm: [npmFixture(dir, [`blob${ext}`])], policyPath: bundledPolicy(dir) });
      assert.deepEqual(result.unreviewed.map((f) => f.path), [`blob${ext}`]);
    });
  }
  it("still does not flag .json, .txt, .md or .map files", () => {
    const dir = scratchDir();
    const files = ["a.json", "b.txt", "c.md", "d.map"];
    const result = checkBundledContent({ npm: [npmFixture(dir, files)], policyPath: bundledPolicy(dir) });
    assert.deepEqual(result.unreviewed, []);
  });
});
