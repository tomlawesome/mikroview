'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { planActions } = require('./run');

const OK = { state: 'ok', title: 'CHR watch: passing', body: 'ok body' };
const FAILING = { state: 'failing', title: 'CHR watch: latest run failed', body: 'failing body' };
const STALE = { state: 'stale', title: 'CHR watch: last report is stale', body: 'stale body' };
const MISSING = { state: 'missing', title: 'CHR watch: no usable report', body: 'missing body' };

test('ok with an open issue: close it, exit zero', () => {
  const { actions, exitNonZero } = planActions(OK, [{ iid: 7 }]);
  assert.deepEqual(actions, [{ type: 'close', iid: 7, body: OK.body }]);
  assert.equal(exitNonZero, false);
});

test('ok with no open issue: nothing to do, exit zero', () => {
  const { actions, exitNonZero } = planActions(OK, []);
  assert.deepEqual(actions, []);
  assert.equal(exitNonZero, false);
});

test('failing with an open issue: comment, exit non-zero', () => {
  const { actions, exitNonZero } = planActions(FAILING, [{ iid: 3 }]);
  assert.deepEqual(actions, [{ type: 'comment', iid: 3, body: FAILING.body }]);
  assert.equal(exitNonZero, true);
});

test('failing with no open issue: create one, exit non-zero', () => {
  const { actions, exitNonZero } = planActions(FAILING, []);
  assert.deepEqual(actions, [
    { type: 'create', title: FAILING.title, description: FAILING.body },
  ]);
  assert.equal(exitNonZero, true);
});

test('stale with an open issue: comment, exit non-zero', () => {
  const { actions, exitNonZero } = planActions(STALE, [{ iid: 9 }]);
  assert.deepEqual(actions, [{ type: 'comment', iid: 9, body: STALE.body }]);
  assert.equal(exitNonZero, true);
});

test('missing with no open issue: create one, exit non-zero', () => {
  const { actions, exitNonZero } = planActions(MISSING, []);
  assert.deepEqual(actions, [
    { type: 'create', title: MISSING.title, description: MISSING.body },
  ]);
  assert.equal(exitNonZero, true);
});

test('never more than one open issue is ever acted on', () => {
  const { actions } = planActions(FAILING, [{ iid: 1 }, { iid: 2 }]);
  assert.equal(actions.length, 1);
  assert.equal(actions[0].iid, 1);
});
