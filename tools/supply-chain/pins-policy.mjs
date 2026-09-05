// Checks supply-chain/pins-policy.json against what is actually pinned in
// .github/workflows/*.yml, the Dockerfile and .gitlab-ci.yml (#711).
//
// An immutable commit SHA only proves a reference cannot move on its own;
// it says nothing about whether a human ever read what that commit
// contains, or when. This script is the audit half of that record: it
// fails when a pin appears in the repository that the policy does not
// know about, when a pin the policy records has drifted (a different
// commit or a different `# vX.Y.Z` comment on a `uses:` line, or a
// container tag that no longer matches), or when the policy still lists
// a pin that has since been removed. It does not judge licences or
// vulnerabilities -- supply-chain/licence-policy.yml and the
// govulncheck/npm-audit/trivy-fs jobs already do that.
//
// Deliberately conservative about "what counts as a pin": a GitHub Action
// only counts if pinned to a full 40-character commit SHA (the form
// testing-and-ci's pinning rule requires); a container image counts
// whatever tag the Dockerfile or .gitlab-ci.yml actually names, since
// mikroview does not pin those to digests today and this script checks
// the review record against reality, not a stricter policy this repo
// does not implement.

import { readFileSync, readdirSync } from "node:fs";
import { isAbsolute, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repositoryRoot = fileURLToPath(new URL("../../", import.meta.url));

// --- policy -----------------------------------------------------------

export function loadPolicy(path) {
  const absolute = isAbsolute(path) ? path : resolve(repositoryRoot, path);
  let raw;
  try {
    raw = readFileSync(absolute, "utf8");
  } catch {
    throw new Error(`supply-chain pins policy is missing: ${path}`);
  }
  const policy = JSON.parse(raw);
  if (policy.schemaVersion !== 1) {
    throw new Error("supply-chain pins policy schemaVersion must be 1.");
  }
  if (!Array.isArray(policy.ciActions) || !Array.isArray(policy.containerImages)) {
    throw new Error("supply-chain pins policy must have ciActions and containerImages arrays.");
  }
  return policy;
}

// --- extraction from the actual pins --------------------------------

const ACTION_PIN = /uses:\s*([^\s@]+)@([0-9a-f]{40})(?:\s*#\s*(\S+))?/;

/**
 * Scans workflow YAML text for `uses: name@sha # version` pins. Returns a
 * map keyed by the action name (the exact string before `@`, so
 * `github/codeql-action/analyze` and `github/codeql-action/init` are
 * tracked separately -- they are different actions that happen to share a
 * commit).
 */
export function extractActionPins(fileTexts) {
  const pins = new Map();
  for (const [file, text] of Object.entries(fileTexts)) {
    for (const line of text.split("\n")) {
      const match = ACTION_PIN.exec(line);
      if (!match) continue;
      const [, name, commit, version] = match;
      if (!pins.has(name)) pins.set(name, { commit, version, locations: new Set() });
      pins.get(name).locations.add(file);
    }
  }
  return pins;
}

const DOCKERFILE_FROM = /^\s*FROM\s+(\S+)/i;
const IMAGE_INLINE = /^\s*image:\s*(\S+)\s*$/;
const IMAGE_BLOCK_START = /^\s*image:\s*$/;
const IMAGE_BLOCK_NAME = /^\s*name:\s*(\S+)\s*$/;
const SERVICE_NAME = /^\s*-\s*name:\s*(\S+)\s*$/;

/**
 * Scans a Dockerfile for FROM lines and a GitLab CI YAML file for `image:`
 * pins -- both the one-line form (`image: tag`) and the block form
 * (`image:` / `name: tag` / `entrypoint: [...]`) -- plus service
 * containers (`services: - name: tag`), matched directly on the `- name:`
 * shape rather than by tracking indentation: nothing else in
 * .gitlab-ci.yml uses that shape (a job's own top-level `name:` is never
 * list-prefixed with `-`). Returns a map keyed by the exact image
 * reference string, to the set of files it was found in.
 */
export function extractImagePins({ dockerfile, gitlabCi }) {
  const pins = new Map();
  const record = (reference, file) => {
    if (!pins.has(reference)) pins.set(reference, new Set());
    pins.get(reference).add(file);
  };

  if (dockerfile !== undefined) {
    for (const line of dockerfile.text.split("\n")) {
      const match = DOCKERFILE_FROM.exec(line);
      if (match) record(match[1], dockerfile.file);
    }
  }

  if (gitlabCi !== undefined) {
    const lines = gitlabCi.text.split("\n");
    for (let i = 0; i < lines.length; i++) {
      const inline = IMAGE_INLINE.exec(lines[i]);
      if (inline) {
        record(inline[1], gitlabCi.file);
        continue;
      }
      if (IMAGE_BLOCK_START.test(lines[i])) {
        const next = lines[i + 1] ?? "";
        const blockName = IMAGE_BLOCK_NAME.exec(next);
        if (blockName) record(blockName[1], gitlabCi.file);
        continue;
      }
      const service = SERVICE_NAME.exec(lines[i]);
      if (service) record(service[1], gitlabCi.file);
    }
  }

  return pins;
}

function readIfExists(path) {
  try {
    return readFileSync(path, "utf8");
  } catch {
    return undefined;
  }
}

export function collectRepositoryPins(root = repositoryRoot) {
  const workflowsDir = join(root, ".github", "workflows");
  const fileTexts = {};
  let entries = [];
  try {
    entries = readdirSync(workflowsDir);
  } catch {
    entries = [];
  }
  for (const entry of entries) {
    if (!entry.endsWith(".yml") && !entry.endsWith(".yaml")) continue;
    const relative = `.github/workflows/${entry}`;
    fileTexts[relative] = readFileSync(join(workflowsDir, entry), "utf8");
  }

  const dockerfileText = readIfExists(join(root, "Dockerfile"));
  const gitlabCiText = readIfExists(join(root, ".gitlab-ci.yml"));

  return {
    actionPins: extractActionPins(fileTexts),
    imagePins: extractImagePins({
      dockerfile: dockerfileText === undefined ? undefined : { file: "Dockerfile", text: dockerfileText },
      gitlabCi: gitlabCiText === undefined ? undefined : { file: ".gitlab-ci.yml", text: gitlabCiText },
    }),
  };
}

// --- comparison ---------------------------------------------------------

/**
 * Compares the pins actually found in the repository against the policy.
 * Returns a list of human-readable problem strings; an empty list means
 * the policy matches reality.
 */
export function diffPolicy(policy, { actionPins, imagePins }) {
  const problems = [];

  const policyActions = new Map(policy.ciActions.map((entry) => [entry.name, entry]));
  for (const [name, found] of actionPins) {
    const recorded = policyActions.get(name);
    if (!recorded) {
      problems.push(`ci action pinned but not recorded in the policy: ${name}@${found.commit}`);
      continue;
    }
    if (recorded.commit !== found.commit) {
      problems.push(
        `ci action ${name} is pinned to ${found.commit} but the policy records ${recorded.commit}`,
      );
    }
    if (found.version !== undefined && recorded.version !== found.version) {
      problems.push(
        `ci action ${name} pin comment says ${found.version} but the policy records ${recorded.version}`,
      );
    }
  }
  for (const name of policyActions.keys()) {
    if (!actionPins.has(name)) {
      problems.push(`policy records a ci action no longer pinned anywhere: ${name}`);
    }
  }

  const policyImages = new Map(policy.containerImages.map((entry) => [entry.reference, entry]));
  for (const reference of imagePins.keys()) {
    if (!policyImages.has(reference)) {
      problems.push(`container image pinned but not recorded in the policy: ${reference}`);
    }
  }
  for (const reference of policyImages.keys()) {
    if (!imagePins.has(reference)) {
      problems.push(`policy records a container image no longer pinned anywhere: ${reference}`);
    }
  }

  return problems;
}

function runCli() {
  const policyPath = process.argv[2] ?? "supply-chain/pins-policy.json";
  const policy = loadPolicy(policyPath);
  const { actionPins, imagePins } = collectRepositoryPins();
  const problems = diffPolicy(policy, { actionPins, imagePins });
  if (problems.length > 0) {
    process.stderr.write(
      `Supply-chain pins policy is out of date (${problems.length} problem(s)):\n` +
        problems.map((p) => `  - ${p}\n`).join(""),
    );
    process.exitCode = 1;
    return;
  }
  process.stdout.write(
    `Supply-chain pins policy matches the repository: ${policy.ciActions.length} ci action(s), ` +
      `${policy.containerImages.length} container image(s).\n`,
  );
}

const isMain = process.argv[1] !== undefined && resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) {
  try {
    runCli();
  } catch (error) {
    process.stderr.write(`${error instanceof Error ? error.message : "Supply-chain pins check failed."}\n`);
    process.exitCode = 1;
  }
}
