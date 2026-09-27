// SPDX-License-Identifier: AGPL-3.0-only
'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const {
  globToRegExp,
  matchesGlob,
  parseTreeListing,
  hashEntries,
  jobInputHash,
  candidateJobs,
} = require('./ci-reuse-inputs');

test('candidateJobs names the jobs #1066, #1242 and #1350 cover', () => {
  assert.deepEqual(
    [...candidateJobs()].sort(),
    [
      'gate:image',
      'gate:scenarios 1/4',
      'gate:scenarios 2/4',
      'gate:scenarios 3/4',
      'gate:scenarios 4/4',
      'gate:scenarios:firefox 1/4',
      'gate:scenarios:firefox 2/4',
      'gate:scenarios:firefox 3/4',
      'gate:scenarios:firefox 4/4',
      'gate:scenarios:webkit 1/4',
      'gate:scenarios:webkit 2/4',
      'gate:scenarios:webkit 3/4',
      'gate:scenarios:webkit 4/4',
      'test:container',
      'test:install-line',
      'test:postgres',
    ].sort(),
  );
});

// #1350: the Firefox and WebKit scenario shards used to be absent from
// JOB_INPUTS entirely, so scripts/ci-reuse-gate.js's `candidateJobs().
// includes(job)` check always failed for them and every dev -> preview
// pipeline reran all eight non-Chromium shards regardless of what
// changed. These prove each engine now has its own entry, keyed by its
// own job name, with the same reuse behaviour Chromium's shards already
// had.
// No scenarios in these fixtures, so a no-op listScenarios (nothing
// listed, nothing to validate against the tree) keeps these tests about
// registration and plain content-hashing, not #1350's shard split --
// that gets its own tests below.
const NO_SCENARIOS = { listScenarios: () => [] };

test('gate:scenarios:firefox and :webkit shards are registered like Chromium\'s', () => {
  for (const engine of ['firefox', 'webkit']) {
    for (let i = 1; i <= 4; i += 1) {
      assert.doesNotThrow(() => jobInputHash(`gate:scenarios:${engine} ${i}/4`, [], NO_SCENARIOS));
    }
  }
});

test('an unchanged-inputs Firefox shard hashes the same as its earlier pass (reusable)', () => {
  const entries = [{ path: 'frontend/src/App.svelte', sha: 'x' }];
  const before = jobInputHash('gate:scenarios:firefox 2/4', entries, NO_SCENARIOS);
  const after = jobInputHash('gate:scenarios:firefox 2/4', entries, NO_SCENARIOS);
  assert.equal(before, after);
});

test('a changed input moves a Firefox shard\'s hash, so it reruns rather than reusing a stale pass', () => {
  const before = jobInputHash('gate:scenarios:firefox 2/4', [{ path: 'frontend/src/App.svelte', sha: 'x' }], NO_SCENARIOS);
  const after = jobInputHash('gate:scenarios:firefox 2/4', [{ path: 'frontend/src/App.svelte', sha: 'y' }], NO_SCENARIOS);
  assert.notEqual(before, after);
});

// Reuse across pipelines is matched in scripts/ci-reuse-gate.js by exact
// job-name equality (newestSuccess's `entry?.name !== job`), which this
// file feeds via JOB_INPUTS' keys and scripts/ci-reuse-gate.js's
// `candidateJobs()` gate. A Chromium pass named "gate:scenarios 2/4" can
// therefore never satisfy a lookup for "gate:scenarios:firefox 2/4" or
// "gate:scenarios:webkit 2/4", and vice versa -- proven here at the level
// this file controls: each engine's shard is its own distinct key, not
// an alias or a shared entry.
test('each engine\'s shard at a given index is a distinct JOB_INPUTS key (never cross-engine reusable)', () => {
  for (let i = 1; i <= 4; i += 1) {
    const keys = [`gate:scenarios ${i}/4`, `gate:scenarios:firefox ${i}/4`, `gate:scenarios:webkit ${i}/4`];
    assert.equal(new Set(keys).size, keys.length, keys.join(', '));
    for (const key of keys) assert.ok(candidateJobs().includes(key), key);
  }
});

test('a plain glob matches only its own path', () => {
  assert.ok(matchesGlob('go.mod', 'go.mod'));
  assert.ok(!matchesGlob('go.mod', 'internal/go.mod'));
});

test('** in the middle crosses directory boundaries', () => {
  const re = globToRegExp('frontend/**');
  assert.ok(re.test('frontend/x'));
  assert.ok(re.test('frontend/a/b/c.svelte'));
  assert.ok(!re.test('scripts/frontend/x'));
});

test('*.go at any depth needs **/*.go, not *.go alone', () => {
  assert.ok(!matchesGlob('*.go', 'internal/api/route.go'));
  assert.ok(matchesGlob('**/*.go', 'internal/api/route.go'));
  assert.ok(matchesGlob('**/*.go', 'main.go'));
});

test('parseTreeListing keeps only blob and commit entries, by path', () => {
  const listing = [
    '100644 blob aaaa111111111111111111111111111111aaaa\tmain.go',
    '040000 tree bbbb222222222222222222222222222222bbbb\tinternal',
    '100644 blob cccc333333333333333333333333333333cccc\tinternal/api/route.go',
  ].join('\0');
  const entries = parseTreeListing(listing);
  assert.deepEqual(
    entries.map((e) => e.path).sort(),
    ['internal/api/route.go', 'main.go'],
  );
});

test('hashEntries is stable under reordering', () => {
  const a = [
    { path: 'a', sha: '1' },
    { path: 'b', sha: '2' },
  ];
  const b = [
    { path: 'b', sha: '2' },
    { path: 'a', sha: '1' },
  ];
  assert.equal(hashEntries(a), hashEntries(b));
});

test('hashEntries changes when a selected blob sha changes', () => {
  const before = hashEntries([{ path: 'main.go', sha: '1' }]);
  const after = hashEntries([{ path: 'main.go', sha: '2' }]);
  assert.notEqual(before, after);
});

test('jobInputHash for test:postgres ignores a docs-only change', () => {
  const before = [
    { path: 'internal/persist/store.go', sha: 'aaa' },
    { path: 'README.md', sha: '111' },
  ];
  const after = [
    { path: 'internal/persist/store.go', sha: 'aaa' },
    { path: 'README.md', sha: '222' },
  ];
  assert.equal(
    jobInputHash('test:postgres', before),
    jobInputHash('test:postgres', after),
  );
});

test('jobInputHash for test:postgres reacts to a Go tree change', () => {
  const before = jobInputHash('test:postgres', [{ path: 'internal/persist/store.go', sha: 'aaa' }]);
  const after = jobInputHash('test:postgres', [{ path: 'internal/persist/store.go', sha: 'bbb' }]);
  assert.notEqual(before, after);
});

// #1350: superseded the "all four shards share one hash" invariant --
// see the shard-aware block below, which is what jobInputHash actually
// does now.

test('a change to .gitlab-ci.yml moves every job\'s hash (COMMON)', () => {
  const before = [{ path: '.gitlab-ci.yml', sha: 'a' }];
  const after = [{ path: '.gitlab-ci.yml', sha: 'b' }];
  for (const job of candidateJobs()) {
    assert.notEqual(jobInputHash(job, before, NO_SCENARIOS), jobInputHash(job, after, NO_SCENARIOS), job);
  }
});

// #1350: gate:scenarios' per-shard hash. A fake plan() -- four scripts,
// one per shard -- stands in for scripts/run-scenarios.sh --list so
// these tests are about jobInputHash's own logic, never a real plan()
// or a real subprocess.
const SHARD_SCRIPTS = {
  '1/4': ['frontend/scripts/live-alpha.mjs'],
  '2/4': ['frontend/scripts/live-bravo.mjs'],
  '3/4': ['frontend/scripts/live-charlie.mjs'],
  '4/4': ['frontend/scripts/live-delta.mjs'],
};
const ALL_SCENARIOS = Object.values(SHARD_SCRIPTS).flat();
const BASE_SCENARIO_ENTRIES = ALL_SCENARIOS.map((path) => ({ path, sha: `${path}-1` }));
const SHARDS = ['1/4', '2/4', '3/4', '4/4'];
const ENGINES = ['', ':firefox', ':webkit'];

function fakeListScenarios(shardMap = SHARD_SCRIPTS, all = ALL_SCENARIOS) {
  return (shard) => (shard ? shardMap[shard] : all);
}

test('editing a shard-2 script changes only that shard\'s hash, on every engine (#1350)', () => {
  const listScenarios = fakeListScenarios();
  const before = BASE_SCENARIO_ENTRIES;
  const after = before.map((entry) => (entry.path === 'frontend/scripts/live-bravo.mjs' ? { ...entry, sha: 'changed' } : entry));
  for (const engine of ENGINES) {
    for (const shard of SHARDS) {
      const job = `gate:scenarios${engine} ${shard}`;
      const beforeHash = jobInputHash(job, before, { listScenarios });
      const afterHash = jobInputHash(job, after, { listScenarios });
      if (shard === '2/4') assert.notEqual(beforeHash, afterHash, job);
      else assert.equal(beforeHash, afterHash, job);
    }
  }
});

test('a shared input (the browser helper, a .go file, frontend source, run-scenarios.sh, Makefile, the live-check image, the example config, or RouterOS export testdata) moves every shard\'s hash, on every engine (#1391)', () => {
  const listScenarios = fakeListScenarios();
  const sharedPaths = [
    'frontend/scripts/live-browser.mjs', // listed nowhere by --list, but still frontend/**
    'internal/routeros/parse.go',
    'frontend/src/App.svelte',
    'scripts/run-scenarios.sh',
    'Makefile',
    'live-check.Dockerfile',
    'deploy/config.example.yaml',
    'internal/routeros/export/testdata/hide-sensitive.rsc',
  ];
  for (const changedPath of sharedPaths) {
    const before = [...BASE_SCENARIO_ENTRIES, { path: changedPath, sha: 'a' }];
    const after = [...BASE_SCENARIO_ENTRIES, { path: changedPath, sha: 'b' }];
    for (const engine of ENGINES) {
      for (const shard of SHARDS) {
        const job = `gate:scenarios${engine} ${shard}`;
        assert.notEqual(
          jobInputHash(job, before, { listScenarios }),
          jobInputHash(job, after, { listScenarios }),
          `${job} / ${changedPath}`,
        );
      }
    }
  }
});

test('adding a scenario changes every shard\'s hash, on every engine', () => {
  const before = { listScenarios: fakeListScenarios() };
  const afterShardMap = { ...SHARD_SCRIPTS, '4/4': [...SHARD_SCRIPTS['4/4'], 'frontend/scripts/live-echo.mjs'] };
  const after = { listScenarios: fakeListScenarios(afterShardMap, [...ALL_SCENARIOS, 'frontend/scripts/live-echo.mjs']) };
  const beforeEntries = BASE_SCENARIO_ENTRIES;
  const afterEntries = [...BASE_SCENARIO_ENTRIES, { path: 'frontend/scripts/live-echo.mjs', sha: 'new' }];
  for (const engine of ENGINES) {
    for (const shard of SHARDS) {
      const job = `gate:scenarios${engine} ${shard}`;
      assert.notEqual(jobInputHash(job, beforeEntries, before), jobInputHash(job, afterEntries, after), job);
    }
  }
});

test('renaming a scenario changes every shard\'s hash, on every engine', () => {
  const before = { listScenarios: fakeListScenarios() };
  const afterShardMap = { ...SHARD_SCRIPTS, '1/4': ['frontend/scripts/live-alpha-renamed.mjs'] };
  const afterAll = ALL_SCENARIOS.map((p) => (p === 'frontend/scripts/live-alpha.mjs' ? 'frontend/scripts/live-alpha-renamed.mjs' : p));
  const after = { listScenarios: fakeListScenarios(afterShardMap, afterAll) };
  const beforeEntries = BASE_SCENARIO_ENTRIES;
  const afterEntries = BASE_SCENARIO_ENTRIES
    .filter((entry) => entry.path !== 'frontend/scripts/live-alpha.mjs')
    .concat([{ path: 'frontend/scripts/live-alpha-renamed.mjs', sha: 'frontend/scripts/live-alpha.mjs-1' }]);
  for (const engine of ENGINES) {
    for (const shard of SHARDS) {
      const job = `gate:scenarios${engine} ${shard}`;
      assert.notEqual(jobInputHash(job, beforeEntries, before), jobInputHash(job, afterEntries, after), job);
    }
  }
});

test('a scenario lister failure makes jobInputHash throw, not hash silently (#1350)', () => {
  const listScenarios = () => {
    throw new Error('boom');
  };
  assert.throws(() => jobInputHash('gate:scenarios 2/4', BASE_SCENARIO_ENTRIES, { listScenarios }), /boom/);
});

test('jobInputHash throws when --list names a path absent from the tree (#1350)', () => {
  const listScenarios = () => ['frontend/scripts/live-ghost.mjs'];
  assert.throws(
    () => jobInputHash('gate:scenarios 2/4', BASE_SCENARIO_ENTRIES, { listScenarios }),
    /live-ghost\.mjs/,
  );
});

test('install.sh moves test:install-line\'s hash but not test:container\'s (#1242)', () => {
  const before = [{ path: 'install.sh', sha: 'a' }];
  const after = [{ path: 'install.sh', sha: 'b' }];
  assert.notEqual(jobInputHash('test:install-line', before), jobInputHash('test:install-line', after));
  assert.equal(jobInputHash('test:container', before), jobInputHash('test:container', after));
});

test('jobInputHash throws on an unknown job rather than hashing nothing', () => {
  assert.throws(() => jobInputHash('no-such-job', []), /no job named no-such-job/);
});
