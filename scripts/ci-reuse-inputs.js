// SPDX-License-Identifier: AGPL-3.0-only
'use strict';

const { execFileSync } = require('node:child_process');

// What each candidate job reads (#1066, ported from Orbit's reuse gate,
// orbit #898). scripts/ci-reuse-gate.js hashes the HEAD tree, filtered by
// these globs, and a job whose hash matches an earlier merge request
// pipeline's successful run of the same job stands on that run instead
// of doing the work again. See docs/quality-strategy.md, "What stands on
// what", for the job-by-job reasoning.
//
// Too broad only costs a rerun; too narrow reuses a stale result, which
// is a correctness bug -- widen when in doubt, the same rule Orbit's
// job-inputs.json states. COMMON is in every job's list, so a change to
// this file or to ci-reuse-gate.js reruns everything rather than trust a
// hash whose meaning a bug in these files might just have changed.
//
// #1350: each gate:scenarios shard's hash is now name-aware rather than
// one shared hash for all four. jobInputHash special-cases a job name
// matching /^gate:scenarios(?::(firefox|webkit))? (\d+)\/(\d+)$/ and
// asks scripts/run-scenarios.sh --list (via the injected `listScenarios`,
// scenarioShardHash below) what plan() assigns: the shard's own scripts
// are content-hashed, every other listed scenario is hashed by name
// only, and everything else in SCENARIOS_PATHS (including scripts never
// listed, like live-browser.mjs) stays content-hashed as shared input.
// Safe because plan() is a pure function of the sorted name list: if no
// name changed, membership did not either, so only an edit to a shard's
// own scripts moves its hash, while any add, remove or rename changes
// the name set and moves every shard's hash -- plan()'s slice boundaries
// move with the total scenario count (`k * NR / n`), which is exactly
// what makes a script's shard membership only stable while the name set
// itself is unchanged. See #1350's build notes for why this shells out
// to run-scenarios.sh rather than reimplementing plan() here.
//
// #1306/#1350: the same shard script also runs under Firefox and WebKit
// (`gate:scenarios:firefox N/4`, `gate:scenarios:webkit N/4`), on
// dev -> preview merge requests. Each engine gets its own JOB_INPUTS
// entry, keyed by its own job name alongside gate:scenarios' Chromium
// entries -- the same SCENARIOS_PATHS, since a code change can move any
// engine's result the same way. Reuse across pipelines is matched by
// this exact job-name string (scripts/ci-reuse-gate.js's
// `entry?.name !== job` in newestSuccess), so a Firefox job can only
// ever stand on a Firefox pass and never on a Chromium or WebKit one --
// distinct keys are what makes that true, not anything in the hash
// itself.
//
// gate:image, test:container and test:postgres share one group too: the
// first builds live-check.Dockerfile, the second builds the product
// Dockerfile and runs it, and the third is Go-only against a real
// Postgres. One combined, deliberately over-broad list is simpler to
// keep correct than three narrow ones that would drift apart by hand.

const COMMON = ['.gitlab-ci.yml', 'scripts/ci-reuse-gate.js', 'scripts/ci-reuse-inputs.js'];

const GO_TREE = ['**/*.go', 'go.mod', 'go.sum'];

// #1391: widened past the Go tree and frontend/scripts/** to the other
// paths a gate:scenarios shard actually depends on -- live-check.Dockerfile
// is the image the shards run in, Makefile is what invokes them,
// deploy/** is embedded by exampleconfig.go, internal/** catches non-.go
// data GO_TREE's **/*.go misses (frontend/scripts/live-log-every-rule.mjs
// reads internal/routeros/export/testdata/hide-sensitive.rsc), and
// THIRD-PARTY-NOTICES.md is checked by a scenario too. Before this, a
// change to only those files let a shard stand on a pass that never ran
// against them.
const SCENARIOS_PATHS = [
  ...COMMON,
  ...GO_TREE,
  'frontend/**',
  'scripts/**',
  'Makefile',
  'live-check.Dockerfile',
  'deploy/**',
  'internal/**',
  'THIRD-PARTY-NOTICES.md',
];

// live-check.Dockerfile is already in SCENARIOS_PATHS (#1391); only
// Dockerfile (the product image, not the live-check one) is IMAGE_PATHS'
// own addition.
const IMAGE_PATHS = [...SCENARIOS_PATHS, 'Dockerfile'];

// test:install-line builds the same image as test:container (so it needs
// IMAGE_PATHS) and then runs install.sh, which lives at the repo root and
// so matches none of IMAGE_PATHS' globs on its own (#1242) -- named here
// explicitly rather than folded into scripts/**, which install.sh isn't
// under.
const INSTALL_LINE_PATHS = [...IMAGE_PATHS, 'install.sh'];

const JOB_INPUTS = {
  'gate:scenarios 1/4': SCENARIOS_PATHS,
  'gate:scenarios 2/4': SCENARIOS_PATHS,
  'gate:scenarios 3/4': SCENARIOS_PATHS,
  'gate:scenarios 4/4': SCENARIOS_PATHS,
  'gate:scenarios:firefox 1/4': SCENARIOS_PATHS,
  'gate:scenarios:firefox 2/4': SCENARIOS_PATHS,
  'gate:scenarios:firefox 3/4': SCENARIOS_PATHS,
  'gate:scenarios:firefox 4/4': SCENARIOS_PATHS,
  'gate:scenarios:webkit 1/4': SCENARIOS_PATHS,
  'gate:scenarios:webkit 2/4': SCENARIOS_PATHS,
  'gate:scenarios:webkit 3/4': SCENARIOS_PATHS,
  'gate:scenarios:webkit 4/4': SCENARIOS_PATHS,
  'gate:image': IMAGE_PATHS,
  'test:container': IMAGE_PATHS,
  'test:postgres': IMAGE_PATHS,
  'test:install-line': INSTALL_LINE_PATHS,
};

function candidateJobs() {
  return Object.keys(JOB_INPUTS);
}

function globsFor(job) {
  return JOB_INPUTS[job];
}

function escapeLiteral(ch) {
  return /[.+^${}()|[\]\\]/.test(ch) ? `\\${ch}` : ch;
}

// One glob, as an anchored regular expression over a repository-relative
// path. Segment by segment, because `**` is about path segments and
// nothing else -- `frontend/**` has to match `frontend/x` as well as
// `frontend/a/b`, which a naive `**` -> `.*` gets wrong for the first
// case if `**` is required to cross at least one `/`. Mirrors
// scripts/ci-changed-paths-guard.py's glob_re, in the language this file
// already runs in rather than shelling out to Python for one function.
function globToRegExp(glob) {
  const segments = String(glob).split('/');
  let pattern = '^';
  segments.forEach((segment, index) => {
    const last = index === segments.length - 1;
    if (segment === '**') {
      pattern += last ? '(?:[^/]+/)*[^/]+' : '(?:[^/]+/)*';
      return;
    }
    for (const ch of segment) {
      if (ch === '*') pattern += '[^/]*';
      else if (ch === '?') pattern += '[^/]';
      else pattern += escapeLiteral(ch);
    }
    if (!last) pattern += '/';
  });
  return new RegExp(`${pattern}$`);
}

function matchesGlob(glob, path) {
  return globToRegExp(glob).test(path);
}

function selects(globs, path) {
  return globs.some((glob) => matchesGlob(glob, path));
}

// `git ls-tree -r -z HEAD` output as `{ path, sha }` entries. Records are
// `<mode> <type> <object>\t<path>`, NUL-separated under `-z`, which is
// what the CLI asks for so a path with an unusual byte in it is not
// quoted -- the quoted form would not be the real path.
function parseTreeListing(text) {
  return String(text)
    .split(/[\0\n]/u)
    .filter((record) => record.length > 0)
    .map((record) => {
      const tab = record.indexOf('\t');
      if (tab < 0) throw new Error(`unparsable git ls-tree record: ${record}`);
      const fields = record.slice(0, tab).split(' ');
      return { type: fields[1], sha: fields[2], path: record.slice(tab + 1) };
    })
    .filter((entry) => entry.type === 'blob' || entry.type === 'commit');
}

// sha256 over sorted `path<TAB>sha` lines -- sorted by raw code unit, so
// two machines agree, and never over file contents directly: git has
// already hashed every blob, and a rename or mode-only change still
// moves the line it sits on.
function hashEntries(entries) {
  const crypto = require('node:crypto');
  const lines = [...entries]
    .sort((left, right) => (left.path < right.path ? -1 : left.path > right.path ? 1 : 0))
    .map((entry) => `${entry.path}\t${entry.sha}\n`);
  return crypto.createHash('sha256').update(lines.join(''), 'utf8').digest('hex');
}

const SCENARIO_SHARD_RE = /^gate:scenarios(?::(?:firefox|webkit))? (\d+\/\d+)$/;

// The MV_SHARD value (e.g. '2/4') a scenario-shard job name carries, or
// null for a job this reuse split does not apply to.
function matchScenarioShard(job) {
  const match = SCENARIO_SHARD_RE.exec(String(job));
  return match ? match[1] : null;
}

// scripts/run-scenarios.sh --list is the single source of truth for
// plan()'s assignment (#1350's build notes: shelled out to, never
// reimplemented here). `shard` is an MV_SHARD value, or null for the
// full unsharded list. Exported so scripts/ci-reuse-gate.js can log a
// shard's own scripts with the same lister jobInputHash uses.
function defaultListScenarios(shard) {
  const env = { ...process.env };
  if (shard) env.MV_SHARD = shard;
  else delete env.MV_SHARD;
  const output = execFileSync('scripts/run-scenarios.sh', ['--list'], { encoding: 'utf8', env });
  return output.split('\n').filter((line) => line.length > 0);
}

// A scenario shard's hash: its shared inputs (`selected`, already
// filtered to SCENARIOS_PATHS) minus every listed scenario script, plus
// that shard's own scripts content-hashed, plus every other listed
// scenario hashed by name only (a constant, empty sha -- only the path
// contributes). `listScenarios` is injected so tests never shell out.
//
// Throws rather than hashing something silent if the lister fails, or
// names a path absent from `entries` -- scripts/ci-reuse-gate.js's gate()
// and writeEvidence() both already treat a thrown jobInputHash as "run
// for real, write no evidence" (#1350's build notes).
function scenarioShardHash(job, shard, entries, selected, listScenarios) {
  let ownScripts;
  let allScripts;
  try {
    ownScripts = listScenarios(shard);
    allScripts = listScenarios(null);
  } catch (error) {
    throw new Error(`ci-reuse-inputs: ${job}: scripts/run-scenarios.sh --list failed (${error.message})`);
  }

  const treePaths = new Set(entries.map((entry) => entry.path));
  for (const scriptPath of new Set([...ownScripts, ...allScripts])) {
    if (!treePaths.has(scriptPath)) {
      throw new Error(`ci-reuse-inputs: ${job}: --list named ${scriptPath}, which is not in the tree`);
    }
  }

  const ownSet = new Set(ownScripts);
  const allSet = new Set(allScripts);
  const shared = selected.filter((entry) => !allSet.has(entry.path));
  const own = selected.filter((entry) => ownSet.has(entry.path));
  const others = allScripts
    .filter((scriptPath) => !ownSet.has(scriptPath))
    .map((scriptPath) => ({ path: scriptPath, sha: '' }));

  return hashEntries([...shared, ...own, ...others]);
}

function jobInputHash(job, entries, { listScenarios = defaultListScenarios } = {}) {
  const globs = globsFor(job);
  if (!globs) throw new Error(`ci-reuse-inputs: no job named ${job} in JOB_INPUTS`);
  const selected = entries.filter((entry) => selects(globs, entry.path));
  const shard = matchScenarioShard(job);
  if (!shard) return hashEntries(selected);
  return scenarioShardHash(job, shard, entries, selected, listScenarios);
}

module.exports = {
  JOB_INPUTS,
  candidateJobs,
  globsFor,
  globToRegExp,
  matchesGlob,
  selects,
  parseTreeListing,
  hashEntries,
  jobInputHash,
  matchScenarioShard,
  defaultListScenarios,
};
