// SPDX-License-Identifier: AGPL-3.0-only
'use strict';

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
// gate:scenarios' four shards share one input set even though each only
// runs a slice of the scenario list: every shard builds and runs the
// same live-check image over the same binary, so a change anywhere in
// it can move any shard's result. Splitting the shards' inputs to match
// run-scenarios.sh's plan() (contiguous slices) is future work, not
// required by #1066.
//
// gate:image, test:container and test:postgres share one group too: the
// first builds live-check.Dockerfile, the second builds the product
// Dockerfile and runs it, and the third is Go-only against a real
// Postgres. One combined, deliberately over-broad list is simpler to
// keep correct than three narrow ones that would drift apart by hand.

const COMMON = ['.gitlab-ci.yml', 'scripts/ci-reuse-gate.js', 'scripts/ci-reuse-inputs.js'];

const GO_TREE = ['**/*.go', 'go.mod', 'go.sum'];

const SCENARIOS_PATHS = [...COMMON, ...GO_TREE, 'frontend/**', 'scripts/**'];

const IMAGE_PATHS = [...SCENARIOS_PATHS, 'Dockerfile', 'live-check.Dockerfile'];

const JOB_INPUTS = {
  'gate:scenarios 1/4': SCENARIOS_PATHS,
  'gate:scenarios 2/4': SCENARIOS_PATHS,
  'gate:scenarios 3/4': SCENARIOS_PATHS,
  'gate:scenarios 4/4': SCENARIOS_PATHS,
  'gate:image': IMAGE_PATHS,
  'test:container': IMAGE_PATHS,
  'test:postgres': IMAGE_PATHS,
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

function jobInputHash(job, entries) {
  const globs = globsFor(job);
  if (!globs) throw new Error(`ci-reuse-inputs: no job named ${job} in JOB_INPUTS`);
  return hashEntries(entries.filter((entry) => selects(globs, entry.path)));
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
};
