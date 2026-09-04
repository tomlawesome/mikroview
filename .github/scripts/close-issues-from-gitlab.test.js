'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { closingText } = require('./close-issues-from-gitlab.js');
const { extractClosedIssueNumbers } = require('./close-issues-matcher.js');

const sha = 'abc123';

test('reads the merged MR title and description, not the merge commit', () => {
  const { source, text } = closingText({
    commitSha: sha,
    commitMessage: "Merge branch 'fix/1-thing' into 'dev'\n\nFix the thing\n\nSee merge request ai/mikroview!7",
    mergeRequests: [
      { iid: 7, state: 'merged', merge_commit_sha: sha, title: 'Fix the thing', description: 'Closes #1' },
    ],
  });
  assert.equal(source, '!7');
  assert.deepEqual([...extractClosedIssueNumbers(text)], [1]);
});

test('ignores MRs that merged as some other commit, and unmerged ones', () => {
  const { source, text } = closingText({
    commitSha: sha,
    commitMessage: 'Direct push\n\nCloses #9',
    mergeRequests: [
      { iid: 3, state: 'merged', merge_commit_sha: 'other', title: 'Old', description: 'Closes #2' },
      { iid: 4, state: 'opened', merge_commit_sha: null, title: 'Open', description: 'Closes #3' },
    ],
  });
  assert.equal(source, 'commit');
  assert.deepEqual([...extractClosedIssueNumbers(text)], [9]);
});

test('a squash merge is matched by its squash commit', () => {
  const { source } = closingText({
    commitSha: sha,
    commitMessage: '',
    mergeRequests: [{ iid: 5, state: 'merged', merge_commit_sha: null, squash_commit_sha: sha, title: 't', description: '' }],
  });
  assert.equal(source, '!5');
});

test('a description that only quotes the keyword closes nothing (#503 rules apply)', () => {
  const { text } = closingText({
    commitSha: sha,
    commitMessage: '',
    mergeRequests: [{ iid: 6, state: 'merged', merge_commit_sha: sha, title: 't', description: 'Does not close #4; see `Closes #5` for the old form.' }],
  });
  assert.equal(extractClosedIssueNumbers(text).size, 0);
});
