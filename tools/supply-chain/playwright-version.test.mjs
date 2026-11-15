// #1473: the live gate's Playwright browsers must match the lockfile's
// Playwright. live-check.Dockerfile installs browsers for one pinned
// version; frontend/package-lock.json decides which Playwright the tests
// actually run, and a mismatch means the browsers are not the ones that
// version expects. Run with
// `node --test tools/supply-chain/playwright-version.test.mjs`.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";
import assert from "node:assert/strict";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const DOCKERFILE = "live-check.Dockerfile";
const LOCKFILE = "frontend/package-lock.json";

function dockerfileVersion() {
  const text = readFileSync(join(root, DOCKERFILE), "utf8");
  const m = text.match(/^ARG PLAYWRIGHT_VERSION=(\S+)\s*$/m);
  return m ? m[1] : undefined;
}

function lockfileVersion(pkg) {
  const lock = JSON.parse(readFileSync(join(root, LOCKFILE), "utf8"));
  return lock.packages?.[`node_modules/${pkg}`]?.version;
}

describe("live gate Playwright browsers match the lockfile (#1473)", () => {
  const docker = dockerfileVersion();
  for (const pkg of ["playwright-core", "playwright"]) {
    it(`${DOCKERFILE} matches ${LOCKFILE} node_modules/${pkg}`, () => {
      const locked = lockfileVersion(pkg);
      assert.ok(docker, `no "ARG PLAYWRIGHT_VERSION=<x.y.z>" found in ${DOCKERFILE}`);
      assert.ok(locked, `no version for node_modules/${pkg} found in ${LOCKFILE}`);
      assert.equal(
        docker,
        locked,
        `${DOCKERFILE} ARG PLAYWRIGHT_VERSION is ${docker} but ${LOCKFILE} ` +
          `resolves node_modules/${pkg} to ${locked}; change one so they agree`,
      );
    });
  }
});
