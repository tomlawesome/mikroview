// Checks the licence of every package mikroview actually ships against
// supply-chain/licence-policy.yml -- the npm half of the licence check
// GitHub's actions/dependency-review-action used to run. That action stopped
// running when GitHub became a push mirror with no pull requests (#935), and
// its replacement here is scoped deliberately narrower than "everything
// installed": it checks what the binary distributes -- the Go modules
// statically linked into it and the frontend packages bundled into the
// embedded web assets -- not the build and test tooling that produces it. A
// GPL build tool does not affect the licence of what it produces, which is
// exactly the scope the old GitHub check got wrong (#945 item 3, #948): an
// earlier version of this script walked the whole installed node_modules
// tree, including devDependencies, and failed on twelve tooling-only
// packages (typescript's own `glob`/`jackspeak`/`path-scurry` chain,
// browserslist's `caniuse-lite` data) that never reach a shipped artefact.
//
// "What ships" is exactly the inventory tools/licenses/generate-notices.mjs
// already computes for THIRD-PARTY-NOTICES.md -- the Go module list from
// `go list -deps .` and the frontend package list read back off a real Vite
// build's sourcemaps -- so this script imports goModules() and npmPackages()
// from there rather than re-deriving the same set a second, possibly
// different way. In practice only the npm half is checked here: the Go half
// is `go-licenses check ./...`, run directly by the security:licence-policy
// job in .gitlab-ci.yml, fed this file's allow-licenses list via
// `--print-allowed` so both checks read the one list rather than two that
// can drift apart.
//
// SPDX licence expressions (`MIT OR Apache-2.0`, `A AND B`, parenthesised)
// are parsed and evaluated against the allow-list: OR passes if any branch is
// allowed, AND needs every part allowed. A missing, empty, `UNLICENSED`,
// `SEE LICENSE IN ...` or otherwise unparseable licence fails closed.

import { readFileSync } from "node:fs";
import { isAbsolute, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { npmPackages } from "./generate-notices.mjs";

const repositoryRoot = fileURLToPath(new URL("../../", import.meta.url));

// --- policy file -------------------------------------------------------

function stripComment(line) {
  const hashIndex = line.indexOf("#");
  return hashIndex === -1 ? line : line.slice(0, hashIndex);
}

/**
 * Parses an exact-version dependency exception, `pkg:npm/<name>@<version>`.
 * <name> may itself contain "@" (a scoped package), so the version is
 * whatever follows the *last* "@".
 */
export function parsePurlException(purl) {
  const prefix = "pkg:npm/";
  if (!purl.startsWith(prefix)) {
    throw new Error(`unsupported dependency exception PURL: ${purl}`);
  }
  const rest = purl.slice(prefix.length);
  const lastAt = rest.lastIndexOf("@");
  if (lastAt <= 0) {
    throw new Error(`dependency exception PURL is missing a version: ${purl}`);
  }
  return { name: rest.slice(0, lastAt), version: rest.slice(lastAt + 1) };
}

/**
 * Loads the two-key licence policy file: `allow-licenses` (a flat list of
 * SPDX ids) and `allow-dependencies-licenses` (a flat list of exact-version
 * PURL exceptions, or `[]`). This is not a general YAML parser -- it
 * understands only this file's shape (two top-level keys, each an indented
 * `- value` list or an inline `[]`, `#` comments anywhere) -- because the
 * repository has no YAML dependency to reach for and this file does not need
 * one.
 */
export function loadPolicy(policyPath) {
  const raw = readFileSync(policyPath, "utf8");
  const allowLicences = new Set();
  const allowExactExceptions = new Set();
  let currentKey = null;
  const lines = raw.split(/\r?\n/u);
  for (const rawLine of lines) {
    const line = stripComment(rawLine);
    if (line.trim() === "") continue;
    const emptyListMatch = /^([A-Za-z-]+):\s*\[\s*\]\s*$/u.exec(line);
    if (emptyListMatch) {
      currentKey = null;
      continue;
    }
    const topLevelMatch = /^([A-Za-z-]+):\s*$/u.exec(line);
    if (topLevelMatch) {
      currentKey = topLevelMatch[1];
      continue;
    }
    const itemMatch = /^\s+-\s+(\S+)\s*$/u.exec(line);
    if (itemMatch) {
      if (currentKey === "allow-licenses") {
        allowLicences.add(itemMatch[1]);
      } else if (currentKey === "allow-dependencies-licenses") {
        const { name, version } = parsePurlException(itemMatch[1]);
        allowExactExceptions.add(`${name}@${version}`);
      } else {
        throw new Error(`licence policy list item outside a known key: ${rawLine}`);
      }
      continue;
    }
    throw new Error(`unrecognised line in licence policy ${policyPath}: ${rawLine}`);
  }
  return { allowLicences, allowExactExceptions };
}

// --- SPDX expression parsing and evaluation -----------------------------

function tokenizeSpdxExpression(expression) {
  const tokens = [];
  let i = 0;
  while (i < expression.length) {
    const ch = expression[i];
    if (ch === " " || ch === "\t") {
      i += 1;
      continue;
    }
    if (ch === "(" || ch === ")") {
      tokens.push(ch);
      i += 1;
      continue;
    }
    let j = i;
    while (j < expression.length && !" \t()".includes(expression[j])) j += 1;
    tokens.push(expression.slice(i, j));
    i = j;
  }
  return tokens;
}

/** Parses an SPDX licence expression into a small AND/OR/id tree. */
export function parseSpdxExpression(expression) {
  const tokens = tokenizeSpdxExpression(expression);
  let pos = 0;
  const peek = () => tokens[pos];
  const next = () => tokens[pos++];

  function parseAtom() {
    const token = peek();
    if (token === "(") {
      next();
      const node = parseOr();
      if (next() !== ")") throw new Error(`unbalanced parentheses: ${expression}`);
      return node;
    }
    if (token === undefined || token === "AND" || token === "OR" || token === ")") {
      throw new Error(`unexpected token in licence expression: ${expression}`);
    }
    next();
    if (peek() === "WITH") {
      next();
      const exceptionId = next();
      if (exceptionId === undefined) throw new Error(`dangling WITH: ${expression}`);
      // Folded into one atom: an exception-qualified licence must be listed
      // in allow-licences verbatim (with its exception) to be accepted.
      return { type: "id", id: `${token} WITH ${exceptionId}` };
    }
    return { type: "id", id: token };
  }

  function parseAnd() {
    let node = parseAtom();
    while (peek() === "AND") {
      next();
      node = { type: "and", left: node, right: parseAtom() };
    }
    return node;
  }

  function parseOr() {
    let node = parseAnd();
    while (peek() === "OR") {
      next();
      node = { type: "or", left: node, right: parseAnd() };
    }
    return node;
  }

  if (tokens.length === 0) throw new Error("empty licence expression");
  const ast = parseOr();
  if (pos !== tokens.length) throw new Error(`trailing tokens in licence expression: ${expression}`);
  return ast;
}

function evaluateSpdxAst(node, allowLicences) {
  if (node.type === "id") return allowLicences.has(node.id);
  if (node.type === "and") return evaluateSpdxAst(node.left, allowLicences) && evaluateSpdxAst(node.right, allowLicences);
  if (node.type === "or") return evaluateSpdxAst(node.left, allowLicences) || evaluateSpdxAst(node.right, allowLicences);
  return false;
}

const UNRECOGNISED_PREFIXES = ["SEE LICENSE IN"];

/**
 * True only for a non-empty, parseable SPDX expression every required branch
 * of which is in `allowLicences`. Anything else -- missing, empty,
 * `UNLICENSED`, `SEE LICENSE IN ...`, or a string that does not parse as an
 * SPDX expression -- fails closed rather than throwing.
 */
export function isLicenceAllowed(licenceExpression, allowLicences) {
  const trimmed = String(licenceExpression ?? "").trim();
  if (trimmed === "") return false;
  const upper = trimmed.toUpperCase();
  if (upper === "UNLICENSED") return false;
  if (UNRECOGNISED_PREFIXES.some((prefix) => upper.startsWith(prefix))) return false;
  try {
    return evaluateSpdxAst(parseSpdxExpression(trimmed), allowLicences);
  } catch {
    return false;
  }
}

// --- shipped-package inventory -------------------------------------------

/**
 * Normalises a package.json `license` field (or the legacy `licenses`
 * array) into a single SPDX expression string. Handles the same three
 * shapes generate-notices.mjs's `npmPackages()` can hand back as `declared`:
 * a plain string, the legacy `{ type: "..." }` object, and the older still
 * `licenses: [{ type: "..." }, ...]` array (dual entries are OR'd, matching
 * how npm itself always treated more than one entry in that array).
 */
export function licenceExpressionFromDeclared(declared) {
  if (typeof declared === "string") return declared;
  if (declared && typeof declared === "object" && !Array.isArray(declared) && typeof declared.type === "string") {
    return declared.type;
  }
  if (Array.isArray(declared)) {
    const parts = declared
      .map((entry) => (entry && typeof entry.type === "string" ? entry.type : null))
      .filter((value) => value !== null);
    if (parts.length > 0) return parts.join(" OR ");
  }
  return "";
}

/**
 * Checks a list of `{ name, version, license }` packages -- the shipped npm
 * inventory, either the real one from `npmPackages()` or a fixture supplied
 * via `--packages-json` -- against the policy at `policyPath`. Returns the
 * total checked and the offending packages (empty when everything is
 * allowed).
 */
export function checkShippedPackages({ packages, policyPath }) {
  const policy = loadPolicy(policyPath);
  const offending = [];
  for (const pkg of packages) {
    const licence = licenceExpressionFromDeclared(pkg.license);
    if (policy.allowExactExceptions.has(`${pkg.name}@${pkg.version}`)) continue;
    if (!isLicenceAllowed(licence, policy.allowLicences)) {
      offending.push({ name: pkg.name, version: pkg.version, licence });
    }
  }
  return { checked: packages.length, offending };
}

// --- CLI -----------------------------------------------------------------

function parseCliArgs(argv) {
  let policy = "supply-chain/licence-policy.yml";
  let packagesJsonPath;
  let printAllowed = false;
  for (let i = 0; i < argv.length; i += 1) {
    if (argv[i] === "--policy" && argv[i + 1] !== undefined) policy = argv[++i];
    else if (argv[i] === "--packages-json" && argv[i + 1] !== undefined) packagesJsonPath = argv[++i];
    else if (argv[i] === "--print-allowed") printAllowed = true;
  }
  return {
    policy: isAbsolute(policy) ? policy : resolve(repositoryRoot, policy),
    packagesJsonPath:
      packagesJsonPath === undefined ? undefined : isAbsolute(packagesJsonPath) ? packagesJsonPath : resolve(process.cwd(), packagesJsonPath),
    printAllowed,
  };
}

function main() {
  const { policy: policyPath, packagesJsonPath, printAllowed } = parseCliArgs(process.argv.slice(2));

  if (printAllowed) {
    let policy;
    try {
      policy = loadPolicy(policyPath);
    } catch (error) {
      console.error(`licence policy could not be loaded: ${String(error?.message ?? error)}`);
      process.exitCode = 1;
      return;
    }
    // Comma-joined with no spaces: go-licenses' --allowed_licenses expects
    // exactly this shape, and this is the one place both checks read the
    // same list rather than two that can drift apart.
    console.log([...policy.allowLicences].sort().join(","));
    return;
  }

  let packages;
  try {
    if (packagesJsonPath) {
      // Bypasses the real Vite build behind npmPackages() -- for tests and
      // for the failing-proof, which need a fixture package list rather
      // than a full frontend build.
      packages = JSON.parse(readFileSync(packagesJsonPath, "utf8"));
    } else {
      packages = npmPackages().map((p) => ({ name: p.name, version: p.version, license: p.declared }));
    }
  } catch (error) {
    console.error(`licence policy check could not determine the shipped package list: ${String(error?.message ?? error)}`);
    process.exitCode = 1;
    return;
  }

  let result;
  try {
    result = checkShippedPackages({ packages, policyPath });
  } catch (error) {
    console.error(`licence policy check could not run: ${String(error?.message ?? error)}`);
    process.exitCode = 1;
    return;
  }

  if (result.offending.length > 0) {
    for (const pkg of result.offending) {
      console.log(`${pkg.name}@${pkg.version}  ${pkg.licence || "(no licence)"}  → not allowed`);
    }
    console.log(`${result.offending.length} of ${result.checked} package(s) failed the licence policy.`);
    process.exitCode = 1;
    return;
  }
  console.log(`${result.checked} package(s) checked against the licence policy; all allowed.`);
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) main();
