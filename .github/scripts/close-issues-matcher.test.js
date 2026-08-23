'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const {
  extractClosedIssueNumbers,
  extractClosedIssueNumbersLegacy,
} = require('./close-issues-matcher');

function loadFixture(name) {
  const raw = fs.readFileSync(
    path.join(__dirname, 'fixtures', `${name}.json`),
    'utf8',
  );
  const { title, body } = JSON.parse(raw);
  return `${title}\n\n${body || ''}`;
}

// Real PR bodies from issue #503's audit. Fetched with:
//   gh pr view <n> --json title,body
// pr-496 -> falsely closed #371 ("Not fixed: #371" heading)
// pr-497 -> falsely closed #363 ("Does not close #363")
// pr-499 -> falsely closed #442 (a backtick-quoted "`Closes #442`")
// pr-493 -> a genuine closing PR ("Closes #474"), must keep working

test('legacy (pre-#503) pattern reproduces all three false closes', () => {
  // This pins the bug: proof that the fixtures actually exercise the
  // defect described in #503, not invented strings. If this test ever
  // fails, the fixtures no longer reproduce the reported bug.
  assert.ok(extractClosedIssueNumbersLegacy(loadFixture('pr-496')).has(371));
  assert.ok(extractClosedIssueNumbersLegacy(loadFixture('pr-497')).has(363));
  assert.ok(extractClosedIssueNumbersLegacy(loadFixture('pr-499')).has(442));
});

test('PR #496 does not close #371 ("Not fixed: #371" heading)', () => {
  const numbers = extractClosedIssueNumbers(loadFixture('pr-496'));
  assert.equal(numbers.has(371), false);
  // The PR's genuine "Closes #374" trailer must still fire.
  assert.ok(numbers.has(374));
});

test('PR #497 does not close #363 ("Does not close #363")', () => {
  const numbers = extractClosedIssueNumbers(loadFixture('pr-497'));
  assert.equal(numbers.has(363), false);
});

test('PR #499 does not close #442 (backtick-quoted keyword)', () => {
  const numbers = extractClosedIssueNumbers(loadFixture('pr-499'));
  assert.equal(numbers.has(442), false);
});

test('PR #493 still closes #474 (genuine "Closes #474" trailer)', () => {
  const numbers = extractClosedIssueNumbers(loadFixture('pr-493'));
  assert.ok(numbers.has(474));
});

test('negation immediately before the keyword is not a close', () => {
  const cases = [
    'Not fixed: #1',
    'not fixes #1',
    'Does not close #1',
    "doesn't resolve #1",
    "won't fix #1",
    "isn't fixed: #1",
    'never closes #1',
  ];
  for (const text of cases) {
    assert.deepEqual(
      [...extractClosedIssueNumbers(text)],
      [],
      `expected no match in: ${JSON.stringify(text)}`,
    );
  }
});

test('a keyword inside a fenced code block is not a close', () => {
  const text = '```\nCloses #123\n```';
  assert.deepEqual([...extractClosedIssueNumbers(text)], []);
});

test('a keyword inside an inline code span is not a close', () => {
  const text = 'The trailer `Closes #123` was removed from this PR.';
  assert.deepEqual([...extractClosedIssueNumbers(text)], []);
});

test('ordinary closing keywords outside code/negation still match', () => {
  const cases = [
    ['Closes #123', 123],
    ['Fixes #123', 123],
    ['fixed: #123', 123],
    ['Resolves #123', 123],
    ['Closes: #123', 123],
  ];
  for (const [text, expected] of cases) {
    assert.deepEqual([...extractClosedIssueNumbers(text)], [expected]);
  }
});
