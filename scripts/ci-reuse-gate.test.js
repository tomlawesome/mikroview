// SPDX-License-Identifier: AGPL-3.0-only
'use strict';

// Unit tests for the network-shaped half of the reuse gate. No real
// network and no real GitLab: `findReusableRun` and `gate` take a
// `fetchImpl`, so a fake here stands in for the API exactly the way
// Orbit's reuse-lookup.mjs tests do. The git-and-hashing half is covered
// by ci-reuse-inputs.test.js; the end-to-end shell behaviour (a real git
// tree, the CLI's exit codes) is covered by ci-reuse-gate.test.sh.

const test = require('node:test');
const assert = require('node:assert/strict');

const { lookupPrerequisites, newestSuccess, findReusableRun, gate, slug } = require('./ci-reuse-gate');

const BASE = 'https://gitlab.example/api/v4';
const JOB = 'test:postgres';
const HASH = 'deadbeef';

function jsonResponse(body, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  };
}

test('lookupPrerequisites refuses everything but a merge-request pipeline', () => {
  const mrEnv = {
    CI_PIPELINE_SOURCE: 'merge_request_event',
    CI_REUSE_TOKEN: 't',
    CI_API_V4_URL: BASE,
    CI_PROJECT_ID: '1',
    CI_MERGE_REQUEST_IID: '2',
  };
  assert.equal(lookupPrerequisites(mrEnv).ok, true);
  assert.equal(lookupPrerequisites({ ...mrEnv, CI_PIPELINE_SOURCE: 'push' }).ok, false);
  assert.equal(lookupPrerequisites({ ...mrEnv, CI_REUSE_TOKEN: '' }).ok, false);
  assert.equal(lookupPrerequisites({ ...mrEnv, CI_PROJECT_ID: '' }).ok, false);
});

test('slug turns a parallel job name into a safe filename', () => {
  assert.equal(slug('gate:scenarios 1/4'), 'gate_scenarios_1_4');
  assert.equal(slug('test:postgres'), 'test_postgres');
});

test('newestSuccess picks the highest id among retries, ignores other jobs and failures', () => {
  const jobs = [
    { id: 10, name: JOB, status: 'failed' },
    { id: 11, name: JOB, status: 'success' },
    { id: 12, name: JOB, status: 'success' },
    { id: 13, name: 'other', status: 'success' },
  ];
  assert.equal(newestSuccess(jobs, JOB).id, 12);
});

test('findReusableRun: no earlier pipeline means no match', async () => {
  const fetchImpl = async () => jsonResponse([]);
  const result = await findReusableRun({
    base: BASE, token: 't', projectId: '1', mergeRequestIid: '2', pipelineId: 99,
    job: JOB, hash: HASH, fetchImpl, log: () => {},
  });
  assert.equal(result, null);
});

test('findReusableRun: matching evidence in an earlier pipeline is reused', async () => {
  const fetchImpl = async (url) => {
    if (url.includes('/merge_requests/2/pipelines')) return jsonResponse([{ id: 50 }, { id: 99 }]);
    if (url.includes('/pipelines/50/jobs')) return jsonResponse([{ id: 500, name: JOB, status: 'success' }]);
    if (url.includes('/jobs/500/artifacts/')) return jsonResponse({ job: JOB, inputs: HASH });
    throw new Error(`unexpected request: ${url}`);
  };
  const result = await findReusableRun({
    base: BASE, token: 't', projectId: '1', mergeRequestIid: '2', pipelineId: 99,
    job: JOB, hash: HASH, fetchImpl, log: () => {},
  });
  assert.deepEqual(result, { jobId: 500, pipelineId: 50 });
});

test('findReusableRun: a different input hash is not reused', async () => {
  const fetchImpl = async (url) => {
    if (url.includes('/pipelines')) return jsonResponse([{ id: 50 }]);
    if (url.includes('/jobs?')) return jsonResponse([{ id: 500, name: JOB, status: 'success' }]);
    if (url.includes('/artifacts/')) return jsonResponse({ job: JOB, inputs: 'something-else' });
    throw new Error(`unexpected request: ${url}`);
  };
  const result = await findReusableRun({
    base: BASE, token: 't', projectId: '1', mergeRequestIid: '2', pipelineId: 99,
    job: JOB, hash: HASH, fetchImpl, log: () => {},
  });
  assert.equal(result, null);
});

test('findReusableRun: no evidence artefact (404) falls through to the next pipeline', async () => {
  const fetchImpl = async (url) => {
    if (url.includes('/pipelines') && !url.includes('/jobs')) return jsonResponse([{ id: 60 }, { id: 50 }]);
    if (url.includes('/pipelines/60/jobs')) return jsonResponse([{ id: 600, name: JOB, status: 'success' }]);
    if (url.includes('/jobs/600/artifacts/')) return jsonResponse({}, 404);
    if (url.includes('/pipelines/50/jobs')) return jsonResponse([{ id: 500, name: JOB, status: 'success' }]);
    if (url.includes('/jobs/500/artifacts/')) return jsonResponse({ job: JOB, inputs: HASH });
    throw new Error(`unexpected request: ${url}`);
  };
  const result = await findReusableRun({
    base: BASE, token: 't', projectId: '1', mergeRequestIid: '2', pipelineId: 99,
    job: JOB, hash: HASH, fetchImpl, log: () => {},
  });
  assert.deepEqual(result, { jobId: 500, pipelineId: 50 });
});

test('findReusableRun: the current pipeline itself is never a candidate', async () => {
  const fetchImpl = async (url) => {
    if (url.includes('/pipelines') && !url.includes('/jobs')) return jsonResponse([{ id: 99 }]);
    throw new Error(`unexpected request: ${url}`);
  };
  const result = await findReusableRun({
    base: BASE, token: 't', projectId: '1', mergeRequestIid: '2', pipelineId: 99,
    job: JOB, hash: HASH, fetchImpl, log: () => {},
  });
  assert.equal(result, null);
});

test('findReusableRun: a network error is swallowed as "no match", never thrown', async () => {
  const fetchImpl = async () => {
    throw new Error('network is down');
  };
  const result = await findReusableRun({
    base: BASE, token: 't', projectId: '1', mergeRequestIid: '2', pipelineId: 99,
    job: JOB, hash: HASH, fetchImpl, log: () => {},
  });
  assert.equal(result, null);
});

test('gate() returns 1 (run for real) for a job outside JOB_INPUTS', async () => {
  const code = await gate('no-such-job', { CI_PIPELINE_SOURCE: 'merge_request_event' }, async () => jsonResponse([]));
  assert.equal(code, 1);
});

test('gate() returns 1 on a non-merge-request pipeline without calling fetch', async () => {
  let called = false;
  const fetchImpl = async () => {
    called = true;
    return jsonResponse([]);
  };
  const code = await gate('test:postgres', { CI_PIPELINE_SOURCE: 'push' }, fetchImpl);
  assert.equal(code, 1);
  assert.equal(called, false);
});
