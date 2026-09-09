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

test('candidateJobs names the seven jobs #1066 covers', () => {
  assert.deepEqual(
    [...candidateJobs()].sort(),
    [
      'gate:image',
      'gate:scenarios 1/4',
      'gate:scenarios 2/4',
      'gate:scenarios 3/4',
      'gate:scenarios 4/4',
      'test:container',
      'test:postgres',
    ].sort(),
  );
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

test('gate:scenarios shards share the same hash for the same tree', () => {
  const entries = [{ path: 'frontend/src/App.svelte', sha: 'x' }];
  const first = jobInputHash('gate:scenarios 1/4', entries);
  const second = jobInputHash('gate:scenarios 4/4', entries);
  assert.equal(first, second);
});

test('a change to .gitlab-ci.yml moves every job\'s hash (COMMON)', () => {
  const before = [{ path: '.gitlab-ci.yml', sha: 'a' }];
  const after = [{ path: '.gitlab-ci.yml', sha: 'b' }];
  for (const job of candidateJobs()) {
    assert.notEqual(jobInputHash(job, before), jobInputHash(job, after), job);
  }
});

test('jobInputHash throws on an unknown job rather than hashing nothing', () => {
  assert.throws(() => jobInputHash('no-such-job', []), /no job named no-such-job/);
});
