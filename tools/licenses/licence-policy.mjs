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
// different way. The declared licences checked here are the npm half: the
// Go half is `go-licenses check ./...`, run directly by the
// security:licence-policy job in .gitlab-ci.yml, fed this file's
// allow-licenses list via `--print-allowed` so both checks read the one list
// rather than two that can drift apart. Looking inside packages (below)
// covers both ecosystems here.
//
// SPDX licence expressions (`MIT OR Apache-2.0`, `A AND B`, parenthesised)
// are parsed and evaluated against the allow-list: OR passes if any branch is
// allowed, AND needs every part allowed. A missing, empty, `UNLICENSED`,
// `SEE LICENSE IN ...` or otherwise unparseable licence fails closed.
//
// A package's declared licence covers its own code, not everything it
// carries (#1432), so the gate also looks inside what ships:
//
//   - Vendored packages. An npm package's bundleDependencies ship inside
//     it; each is checked by exactly the licence rules above, under its own
//     name@version. A Go package under third_party/, vendor/ (and the like)
//     inside a module is vendored code: if it carries a licence file of its
//     own, go-licenses already classifies it against the same allow-list
//     (it attributes each package to its nearest licence file), so it is
//     listed here, not re-judged; if it carries none, the module's LICENSE
//     is the only thing claiming to cover it, and that needs a review.
//   - Bundled content with no licence field. Images, stylesheets, web
//     pages, fonts and media a shipped npm package carries, files a Go
//     module embeds with //go:embed, and shipped npm source files carrying
//     another copyright holder's licence banner (a library inlined into a
//     bundle) all need a recorded review in the policy's bundled-assets:
//     list. That list is the pattern birdcage's licence gate uses for the
//     files pip packages carry: reviews keyed by exact package@version (a
//     version bump stops an entry matching, so the new version is read
//     again), and an entry matching nothing shipped fails as stale, so the
//     list cannot quietly outlive what it reviewed.

import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, extname, isAbsolute, join, relative, resolve, sep } from "node:path";
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
 * Parses an exact-version package URL, `pkg:npm/<name>@<version>` or
 * `pkg:golang/<module path>@<version>`, as bundled-assets entries use.
 */
export function parsePurl(purl) {
  const match = /^pkg:(npm|golang)\/(.+)$/u.exec(purl);
  if (!match) throw new Error(`unsupported PURL (want pkg:npm/... or pkg:golang/...): ${purl}`);
  const rest = match[2];
  const lastAt = rest.lastIndexOf("@");
  if (lastAt <= 0 || lastAt === rest.length - 1) throw new Error(`PURL is missing a version: ${purl}`);
  return { ecosystem: match[1], name: rest.slice(0, lastAt), version: rest.slice(lastAt + 1) };
}

/**
 * Loads the licence policy file: `allow-licenses` (a flat list of SPDX
 * ids), `allow-dependencies-licenses` (a flat list of exact-version PURL
 * exceptions, or `[]`) and `bundled-assets` (reviews of bundled content,
 * each `<purl> <path-prefix> <licence or finding>`, or `[]`). This is not a
 * general YAML parser -- it understands only this file's shape (top-level
 * keys, each an indented `- value` list or an inline `[]`, `#` comments
 * anywhere) -- because the repository has no YAML dependency to reach for
 * and this file does not need one.
 */
export function loadPolicy(policyPath) {
  const raw = readFileSync(policyPath, "utf8");
  const allowLicences = new Set();
  const allowExactExceptions = new Set();
  const assetReviews = [];
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
    const itemMatch = /^\s+-\s+(\S.*?)\s*$/u.exec(line);
    if (itemMatch) {
      const item = itemMatch[1];
      if (currentKey === "bundled-assets") {
        const parts = /^(\S+)\s+(\S+)\s+(\S.*)$/u.exec(item);
        if (!parts) throw new Error(`bundled-assets entry needs "<purl> <path-prefix> <licence>": ${rawLine}`);
        const { ecosystem, name, version } = parsePurl(parts[1]);
        assetReviews.push({ ecosystem, name, version, prefix: parts[2], licence: parts[3], purl: parts[1] });
      } else if (/\s/u.test(item)) {
        throw new Error(`unrecognised line in licence policy ${policyPath}: ${rawLine}`);
      } else if (currentKey === "allow-licenses") {
        allowLicences.add(item);
      } else if (currentKey === "allow-dependencies-licenses") {
        const { name, version } = parsePurlException(item);
        allowExactExceptions.add(`${name}@${version}`);
      } else {
        throw new Error(`licence policy list item outside a known key: ${rawLine}`);
      }
      continue;
    }
    throw new Error(`unrecognised line in licence policy ${policyPath}: ${rawLine}`);
  }
  return { allowLicences, allowExactExceptions, assetReviews };
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

// --- looking inside shipped packages (#1432) -----------------------------

// File types that carry no licence field of their own and are the usual
// vehicle for someone else's work. JavaScript is not on the npm list: it is
// an npm package's own code, and other projects' code inlined into it is
// found by its licence banner instead (below). It is on the Go list, where
// an embedded .js file is never the module's own Go code.
const ASSET_EXTENSIONS = new Set([
  ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".bmp", ".webp", ".avif", ".tif", ".tiff",
  ".css", ".scss", ".sass", ".less", ".html", ".htm", ".xhtml",
  ".woff", ".woff2", ".ttf", ".otf", ".eot",
  ".mp3", ".wav", ".ogg", ".mp4", ".webm", ".swf",
]);
const GO_EMBED_EXTENSIONS = new Set([...ASSET_EXTENSIONS, ".js", ".mjs", ".cjs"]);

// Directory names Go code is vendored under inside a module.
const GO_VENDOR_SEGMENTS = new Set(["third_party", "third-party", "thirdparty", "vendor", "_vendor"]);

const LICENCE_FILE = /^(LICEN[CS]E|COPYING)([-._][A-Za-z0-9.-]+)?$/iu;

const toPosix = (path) => path.split(sep).join("/");

function licenceFilesIn(dir) {
  if (!existsSync(dir)) return [];
  return readdirSync(dir).filter((name) => LICENCE_FILE.test(name)).sort();
}

function readJson(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

/** Every file under `root`, as root-relative POSIX paths. */
function walkFiles(root, { skipNodeModules }) {
  const out = [];
  const walk = (dir) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.isDirectory()) {
        if (skipNodeModules && entry.name === "node_modules") continue;
        walk(join(dir, entry.name));
      } else if (entry.isFile()) {
        out.push(toPosix(relative(root, join(dir, entry.name))));
      }
    }
  };
  if (existsSync(root)) walk(root);
  return out.sort();
}

// -- licence banners ---------------------------------------------------------

const COMMENT = /\/\*[\s\S]*?\*\/|(?:^[ \t]*\/\/[^\n]*(?:\n|$))+/gmu;
const COPYRIGHT_MARK = /copyright|©|(?:^|[\s*/])\([cC]\)\s/u;
// Licence-text boilerplate that says "copyright" without naming anyone.
const COPYRIGHT_BOILERPLATE = /copyright\s+(notice|holder|owner|law|statement)|above copyright/iu;

function normaliseWords(text) {
  return String(text)
    .toLowerCase()
    .replace(/[^a-z0-9]+/gu, " ")
    .trim();
}

function holderOf(line) {
  // One-line banners pack several fields together ("lib v4 | (c) 2015
  // Corp | MIT"): only the field holding the copyright mark, from the mark
  // onwards, names the holder.
  const field = line.split(/\s\|\s|;/u).find((part) => COPYRIGHT_MARK.test(part)) ?? line;
  const mark = field.search(/copyright|©|\([cC]\)/iu);
  return normaliseWords(
    field
      .slice(Math.max(mark, 0))
      .replace(/<[^>]*>/gu, " ")
      .replace(/\S+@\S+/gu, " ")
      .replace(/https?:\/\/\S+/gu, " ")
      .replace(/copyright|©|\([cC]\)|all rights reserved|\bby\b/giu, " ")
      .replace(/\b\d{4}\b|\bpresent\b/giu, " "),
  );
}

function commentLines(comment) {
  return comment
    .split("\n")
    .map((line) => line.replace(/^\s*(\/\*+!?|\*\/|\*|\/\/+)?/u, "").replace(/\*+\/\s*$/u, "").trim());
}

/**
 * The licence banners in a source file, each with the copyright holders it
 * names. A banner is a comment that is `/*!`-preserved, carries @license
 * or @preserve, or states a copyright. A copyright line naming no one
 * ("Copyright (C) 2008") takes its holder from the next non-empty line,
 * which is where such banners put it.
 */
export function licenceBanners(source) {
  const banners = [];
  for (const match of String(source).matchAll(COMMENT)) {
    const comment = match[0];
    const preserved = /^\/\*!/u.test(comment) || /@license|@preserve/iu.test(comment);
    if (!preserved && !COPYRIGHT_MARK.test(comment)) continue;
    const lines = commentLines(comment);
    const holders = [];
    lines.forEach((line, i) => {
      if (!COPYRIGHT_MARK.test(line) || COPYRIGHT_BOILERPLATE.test(line)) return;
      let holder = holderOf(line);
      if (holder === "") holder = holderOf(lines.slice(i + 1).find((next) => next !== "") ?? "");
      holders.push(holder);
    });
    banners.push({ text: comment, holders });
  }
  return banners;
}

/**
 * The copyright holders a shipped file names that are not the package's
 * own -- the signature of another project's code inlined into it. "Own"
 * means named in the package's licence file or package.json (name, author,
 * contributors, maintainers). A preserved banner naming no holder at all
 * counts as foreign unless it names the package. Deliberately biased
 * towards flagging: a false positive costs one review entry, a false
 * negative ships someone's code unread.
 */
export function foreignBannerHolders(source, { packageName, ownText }) {
  const own = ` ${normaliseWords(ownText)} `;
  const ownName = normaliseWords(packageName.replace(/^@[^/]+\//u, ""));
  const foreign = new Set();
  for (const banner of licenceBanners(source)) {
    if (banner.holders.length === 0) {
      if (!` ${normaliseWords(banner.text)} `.includes(` ${ownName} `)) foreign.add("(no holder named)");
      continue;
    }
    for (const holder of banner.holders) {
      if (holder === "") {
        if (!` ${normaliseWords(banner.text)} `.includes(` ${ownName} `)) foreign.add("(no holder named)");
      } else if (!own.includes(` ${holder} `)) {
        foreign.add(holder);
      }
    }
  }
  return [...foreign].sort();
}

function ownTextOf(pkgDir, manifest) {
  const parts = [manifest.name ?? ""];
  for (const key of ["author", "contributors", "maintainers"]) {
    if (manifest[key] !== undefined) parts.push(JSON.stringify(manifest[key]));
  }
  for (const file of licenceFilesIn(pkgDir)) parts.push(readFileSync(join(pkgDir, file), "utf8"));
  return parts.join("\n");
}

// -- inventories ---------------------------------------------------------------

/**
 * The Go packages linked into the binary from modules other than
 * mikroview's own -- the same `go list -deps .` scope goModules() in
 * generate-notices.mjs uses, at package rather than module granularity,
 * since vendoring and //go:embed are properties of a package.
 */
export function goPackages() {
  const format = [
    "{{if and .Module (not .Module.Main)}}{{.ImportPath}}",
    "{{.Dir}}",
    "{{.Module.Path}}",
    "{{.Module.Version}}",
    "{{.Module.Dir}}",
    '{{join .EmbedFiles "|"}}{{end}}',
  ].join("\t");
  const out = execFileSync("go", ["list", "-buildvcs=false", "-deps", "-f", format, "."], {
    cwd: repositoryRoot,
    encoding: "utf8",
    maxBuffer: 32 * 1024 * 1024,
  });
  const packages = [];
  for (const line of out.split("\n")) {
    if (!line.trim()) continue;
    const [importPath, dir, module, version, moduleDir, embeds] = line.split("\t");
    packages.push({ importPath, dir, module, version, moduleDir, embedFiles: embeds ? embeds.split("|") : [] });
  }
  return packages;
}

// -- the checks ----------------------------------------------------------------

function npmVendored(pkg, policy) {
  const manifestPath = join(pkg.dir, "package.json");
  if (!existsSync(manifestPath)) return { bundles: false, checked: [], offending: [] };
  const manifest = readJson(manifestPath);
  const declared = manifest.bundleDependencies ?? manifest.bundledDependencies;
  const bundles = declared === true || (Array.isArray(declared) && declared.length > 0);
  const checked = [];
  const offending = [];
  if (!bundles) return { bundles, checked, offending };
  // Everything under the carrier's node_modules ships inside its tarball,
  // the bundled packages' own dependencies included, so every nested
  // package.json is one vendored package.
  for (const file of walkFiles(join(pkg.dir, "node_modules"), { skipNodeModules: false })) {
    if (!file.endsWith("/package.json")) continue;
    // Only a package's own manifest -- <name>/package.json or
    // @scope/<name>/package.json directly under some node_modules/ -- not a
    // fixture or nested build manifest further inside a package.
    const isPackageRoot = /(^|\/)node_modules\/(@[^/]+\/)?[^/@][^/]*\/package\.json$/u.test(`node_modules/${file}`);
    if (!isPackageRoot) continue;
    const inner = readJson(join(pkg.dir, "node_modules", file));
    if (!inner.name || !inner.version) continue;
    const licence = licenceExpressionFromDeclared(inner.license ?? inner.licenses);
    checked.push(`${inner.name}@${inner.version}`);
    if (policy.allowExactExceptions.has(`${inner.name}@${inner.version}`)) continue;
    if (!isLicenceAllowed(licence, policy.allowLicences)) {
      offending.push({ name: inner.name, version: inner.version, licence, vendoredIn: `${pkg.name}@${pkg.version}` });
    }
  }
  return { bundles, checked, offending };
}

function npmFindings(pkg, bundles) {
  const findings = [];
  const base = { ecosystem: "npm", name: pkg.name, version: pkg.version };
  for (const file of walkFiles(pkg.dir, { skipNodeModules: !bundles })) {
    if (ASSET_EXTENSIONS.has(extname(file).toLowerCase())) {
      findings.push({ ...base, path: file, kind: "bundled asset (no licence field)" });
    }
  }
  // Files the build loaded from the package but the walk above could not
  // see (a package whose directory is not where the inventory says) are
  // still the package's shipped assets.
  for (const file of pkg.shippedAssets ?? []) {
    if (!findings.some((f) => f.path === file)) findings.push({ ...base, path: file, kind: "bundled asset (no licence field)" });
  }
  const manifestPath = join(pkg.dir, "package.json");
  const manifest = existsSync(manifestPath) ? readJson(manifestPath) : { name: pkg.name };
  const ownText = ownTextOf(pkg.dir, manifest);
  for (const shipped of pkg.shippedFiles ?? []) {
    const source = shipped.content ?? readFileSync(join(pkg.dir, shipped.path), "utf8");
    const holders = foreignBannerHolders(source, { packageName: pkg.name, ownText });
    if (holders.length > 0) {
      findings.push({ ...base, path: shipped.path, kind: `inlined code with another holder's copyright: ${holders.join(", ")}` });
    }
  }
  return findings;
}

function goFindings(goPkgs) {
  const findings = [];
  const ownLicensed = new Map();
  for (const pkg of goPkgs) {
    const base = { ecosystem: "golang", name: pkg.module, version: pkg.version };
    const rel = toPosix(relative(pkg.moduleDir, pkg.dir));
    const segments = rel === "" ? [] : rel.split("/");
    const marker = segments.findIndex((segment) => GO_VENDOR_SEGMENTS.has(segment));
    if (marker !== -1) {
      const vendoredRoot = segments.slice(0, Math.min(marker + 2, segments.length));
      // A licence file anywhere from the package up to the vendored
      // project's own root counts as the vendored code's own licence.
      let found = null;
      for (let depth = segments.length; depth >= vendoredRoot.length && !found; depth -= 1) {
        const candidate = segments.slice(0, depth).join("/");
        const files = licenceFilesIn(join(pkg.moduleDir, candidate));
        if (files.length > 0) found = `${candidate}/${files[0]}`;
      }
      if (found) {
        ownLicensed.set(pkg.importPath, `${pkg.module}@${pkg.version} ${found}`);
      } else {
        findings.push({ ...base, path: `${vendoredRoot.join("/")}/`, kind: "vendored code with no licence file of its own" });
      }
    } else {
      // Not under a vendoring directory, but carrying a licence file below
      // the module root (go-jose's fork of encoding/json is the shape):
      // vendored code in all but name. Listed, since go-licenses already
      // judges it by that file.
      for (let depth = segments.length; depth >= 1; depth -= 1) {
        const candidate = segments.slice(0, depth).join("/");
        const files = licenceFilesIn(join(pkg.moduleDir, candidate));
        if (files.length > 0) {
          ownLicensed.set(pkg.importPath, `${pkg.module}@${pkg.version} ${candidate}/${files[0]}`);
          break;
        }
      }
    }
    for (const embedded of pkg.embedFiles ?? []) {
      if (!GO_EMBED_EXTENSIONS.has(extname(embedded).toLowerCase())) continue;
      const path = toPosix(relative(pkg.moduleDir, join(pkg.dir, embedded)));
      findings.push({ ...base, path, kind: "embedded asset (no licence field)" });
    }
  }
  return { findings, ownLicensed };
}

/**
 * Looks inside the shipped npm packages and Go packages (#1432): checks
 * npm bundleDependencies by the licence rules, and requires a bundled-assets
 * review for every bundled asset, embedded file, inlined other-project
 * source file and licence-less vendored Go directory. Returns the vendored
 * packages checked and failing, the findings with and without a review,
 * the stale reviews, and the vendored Go packages left to go-licenses.
 */
export function checkBundledContent({ npm = [], go = [], policyPath }) {
  const policy = loadPolicy(policyPath);
  const vendoredChecked = [];
  const vendoredOffending = [];
  const findings = [];
  for (const pkg of npm) {
    if (!pkg.dir) throw new Error(`${pkg.name}@${pkg.version} has no directory to look inside`);
    const vendored = npmVendored(pkg, policy);
    vendoredChecked.push(...vendored.checked);
    vendoredOffending.push(...vendored.offending);
    findings.push(...npmFindings(pkg, vendored.bundles));
  }
  const goResult = goFindings(go);
  findings.push(...goResult.findings);

  const seen = new Set();
  const unreviewed = [];
  const reviewed = [];
  const usedReviews = new Set();
  for (const finding of findings) {
    const key = `${finding.ecosystem}:${finding.name}@${finding.version}:${finding.path}`;
    if (seen.has(key)) continue;
    seen.add(key);
    const review = policy.assetReviews.find(
      (r) =>
        r.ecosystem === finding.ecosystem &&
        r.name === finding.name &&
        r.version === finding.version &&
        finding.path.startsWith(r.prefix),
    );
    if (review) {
      usedReviews.add(review);
      reviewed.push({ ...finding, review });
    } else {
      unreviewed.push(finding);
    }
  }
  const stale = policy.assetReviews.filter((r) => !usedReviews.has(r));
  return {
    vendoredChecked,
    vendoredOffending,
    unreviewed,
    reviewed,
    stale,
    goOwnLicensed: [...goResult.ownLicensed].sort(([a], [b]) => a.localeCompare(b)),
  };
}

function purlOf(finding) {
  return `pkg:${finding.ecosystem}/${finding.name}@${finding.version}`;
}

/** Groups unreviewed findings by package and directory, for output. */
function groupUnreviewed(unreviewed) {
  const groups = new Map();
  for (const finding of unreviewed) {
    const directory = finding.path.endsWith("/") ? finding.path : dirname(finding.path) === "." ? "" : `${dirname(finding.path)}/`;
    const key = `${purlOf(finding)} ${directory}\t${finding.kind}`;
    groups.set(key, (groups.get(key) ?? 0) + 1);
  }
  return [...groups].sort(([a], [b]) => a.localeCompare(b));
}

// --- CLI -----------------------------------------------------------------

function parseCliArgs(argv) {
  let policy = "supply-chain/licence-policy.yml";
  let packagesJsonPath;
  let goPackagesJsonPath;
  let printAllowed = false;
  const fromCwd = (path) => (isAbsolute(path) ? path : resolve(process.cwd(), path));
  for (let i = 0; i < argv.length; i += 1) {
    if (argv[i] === "--policy" && argv[i + 1] !== undefined) policy = argv[++i];
    else if (argv[i] === "--packages-json" && argv[i + 1] !== undefined) packagesJsonPath = fromCwd(argv[++i]);
    else if (argv[i] === "--go-packages-json" && argv[i + 1] !== undefined) goPackagesJsonPath = fromCwd(argv[++i]);
    else if (argv[i] === "--print-allowed") printAllowed = true;
  }
  return {
    policy: isAbsolute(policy) ? policy : resolve(repositoryRoot, policy),
    packagesJsonPath,
    goPackagesJsonPath,
    printAllowed,
  };
}

function main() {
  const { policy: policyPath, packagesJsonPath, goPackagesJsonPath, printAllowed } = parseCliArgs(process.argv.slice(2));

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
  let goPkgs;
  try {
    if (packagesJsonPath || goPackagesJsonPath) {
      // Bypasses the real Vite build behind npmPackages() and the real
      // `go list` behind goPackages() -- for tests and for the
      // failing-proof, which need fixture inventories rather than a full
      // frontend build. Either fixture alone means the other is empty.
      packages = packagesJsonPath ? JSON.parse(readFileSync(packagesJsonPath, "utf8")) : [];
      goPkgs = goPackagesJsonPath ? JSON.parse(readFileSync(goPackagesJsonPath, "utf8")) : [];
    } else {
      packages = npmPackages().map((p) => ({
        name: p.name,
        version: p.version,
        license: p.declared,
        dir: p.dir,
        shippedFiles: p.shippedFiles,
        shippedAssets: p.shippedAssets,
      }));
      goPkgs = goPackages();
    }
  } catch (error) {
    console.error(`licence policy check could not determine the shipped package list: ${String(error?.message ?? error)}`);
    process.exitCode = 1;
    return;
  }

  let result;
  let inside;
  try {
    result = checkShippedPackages({ packages, policyPath });
    inside = checkBundledContent({ npm: packages.filter((p) => p.dir), go: goPkgs, policyPath });
  } catch (error) {
    console.error(`licence policy check could not run: ${String(error?.message ?? error)}`);
    process.exitCode = 1;
    return;
  }

  let failed = false;
  const offending = [...result.offending, ...inside.vendoredOffending];
  if (offending.length > 0) {
    for (const pkg of offending) {
      const label = pkg.vendoredIn ? `${pkg.name}@${pkg.version} (vendored in ${pkg.vendoredIn})` : `${pkg.name}@${pkg.version}`;
      console.log(`${label}  ${pkg.licence || "(no licence)"}  → not allowed`);
    }
    console.log(`${offending.length} of ${result.checked + inside.vendoredChecked.length} package(s) failed the licence policy.`);
    failed = true;
  }

  if (inside.unreviewed.length > 0) {
    console.log(
      `${inside.unreviewed.length} bundled file(s) with no bundled-assets review in ${policyPath} -- ` +
        "images, pages, stylesheets, fonts, embedded files and other projects' inlined code carry no licence " +
        "field of their own, so each needs reading and recording:",
    );
    for (const [key, count] of groupUnreviewed(inside.unreviewed)) {
      const [where, kind] = key.split("\t");
      console.log(`  ${where} (${count} file(s): ${kind})`);
    }
    failed = true;
  }

  if (inside.stale.length > 0) {
    console.log(`${inside.stale.length} stale bundled-assets review(s) in ${policyPath} matched no shipped file -- delete them:`);
    for (const review of inside.stale) console.log(`  ${review.purl} ${review.prefix} ${review.licence}`);
    failed = true;
  }

  if (inside.goOwnLicensed.length > 0) {
    console.log(
      `${inside.goOwnLicensed.length} vendored Go package(s) carry their own licence file; go-licenses classifies each against the same allow-list:`,
    );
    for (const [importPath, where] of inside.goOwnLicensed) console.log(`  ${importPath}  (${where})`);
  }

  // A green gate must not hide that some files passed only because someone
  // reviewed them rather than because a licence field said so.
  if (inside.reviewed.length > 0) {
    const byReview = new Map();
    for (const finding of inside.reviewed) byReview.set(finding.review, (byReview.get(finding.review) ?? 0) + 1);
    console.log(`${inside.reviewed.length} bundled file(s) passed by recorded review (bundled-assets), not by a licence field:`);
    for (const [review, count] of byReview) console.log(`  ${review.purl} ${review.prefix} (${count}) — ${review.licence}`);
  }

  if (failed) {
    process.exitCode = 1;
    return;
  }
  console.log(
    `${result.checked} package(s) checked against the licence policy; all allowed. ` +
      `${inside.vendoredChecked.length} vendored package(s) checked inside them; all allowed.`,
  );
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) main();
