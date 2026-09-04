'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const { decideChrWatch, STALE_AFTER_MS } = require('./chr-watch');

function loadFixture(name) {
  return fs.readFileSync(
    path.join(__dirname, 'fixtures', `${name}.json`),
    'utf8',
  );
}

const FINISHED_AT = new Date('2026-09-04T05:31:00Z');

test('fresh pass is ok', () => {
  const result = decideChrWatch(loadFixture('chr-report-pass'), FINISHED_AT);
  assert.equal(result.state, 'ok');
  assert.match(result.body, /passed/);
});

test('fresh fail is failing', () => {
  const result = decideChrWatch(loadFixture('chr-report-fail'), FINISHED_AT);
  assert.equal(result.state, 'failing');
  assert.match(result.body, /failed/);
});

test('stale-but-passing report reads as stale, not ok', () => {
  const now = new Date(FINISHED_AT.getTime() + STALE_AFTER_MS + 60_000);
  const result = decideChrWatch(loadFixture('chr-report-pass'), now);
  assert.equal(result.state, 'stale');
  assert.match(result.body, /days old/);
});

test('missing file (null) is missing', () => {
  const result = decideChrWatch(null, FINISHED_AT);
  assert.equal(result.state, 'missing');
});

test('absent file (undefined) is missing', () => {
  const result = decideChrWatch(undefined, FINISHED_AT);
  assert.equal(result.state, 'missing');
});

test('unparsable JSON is missing, not a crash', () => {
  const result = decideChrWatch('{ this is not json', FINISHED_AT);
  assert.equal(result.state, 'missing');
});

test('a schema other than 1 is missing', () => {
  const raw = JSON.stringify({
    schema: 2,
    result: 'pass',
    finished_at: FINISHED_AT.toISOString(),
  });
  const result = decideChrWatch(raw, FINISHED_AT);
  assert.equal(result.state, 'missing');
});

test('a report missing required fields is missing', () => {
  const raw = JSON.stringify({ schema: 1, result: 'pass' }); // no finished_at
  const result = decideChrWatch(raw, FINISHED_AT);
  assert.equal(result.state, 'missing');
});

test('boundary: exactly 9 days old is not yet stale', () => {
  const now = new Date(FINISHED_AT.getTime() + STALE_AFTER_MS);
  const result = decideChrWatch(loadFixture('chr-report-pass'), now);
  assert.equal(result.state, 'ok');
});

test('boundary: one millisecond past 9 days is stale', () => {
  const now = new Date(FINISHED_AT.getTime() + STALE_AFTER_MS + 1);
  const result = decideChrWatch(loadFixture('chr-report-pass'), now);
  assert.equal(result.state, 'stale');
});

test('a stale failing report still reports as stale, since silence outranks a known failure', () => {
  const now = new Date(FINISHED_AT.getTime() + STALE_AFTER_MS + 1);
  const result = decideChrWatch(loadFixture('chr-report-fail'), now);
  assert.equal(result.state, 'stale');
});

test('title and body are always non-empty strings, for every state', () => {
  const cases = [
    decideChrWatch(loadFixture('chr-report-pass'), FINISHED_AT),
    decideChrWatch(loadFixture('chr-report-fail'), FINISHED_AT),
    decideChrWatch(null, FINISHED_AT),
  ];
  for (const result of cases) {
    assert.equal(typeof result.title, 'string');
    assert.ok(result.title.length > 0);
    assert.equal(typeof result.body, 'string');
    assert.ok(result.body.length > 0);
  }
});
