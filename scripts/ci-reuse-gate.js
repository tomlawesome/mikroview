#!/usr/bin/env node
// SPDX-License-Identifier: AGPL-3.0-only
'use strict';

// Stand on an earlier run of this same job rather than repeating it
// (#1066, ported from Orbit's reuse gate, orbit #898). The idea: a fix
// pushed after one red job used to rerun every job that had already
// passed on this same merge request, four scenario shards, the image
// build and the container/postgres tests included. This job hashes what
// scripts/ci-reuse-inputs.js says a job reads, looks through the merge
// request's earlier pipelines for a successful run of the same job on
// the same hash, and stops when it finds one.
//
// Two commands, both meant to sit at the ends of a candidate job's
// `script:` in .gitlab-ci.yml (see the `.reuse_gate` / `.reuse_evidence`
// anchors there):
//
//   node scripts/ci-reuse-gate.js gate <job name>
//     Exit 0: stand on an earlier run -- the caller's `if` then exits the
//     job successfully without doing the work.
//     Exit 1: nothing to stand on, or something went wrong -- run for
//     real. A missed reuse is never a failure: every fault here (no
//     token, no network, an expired artefact, an unknown job) falls
//     through to the real work, which is the only direction that can
//     never be wrong.
//
//   node scripts/ci-reuse-gate.js evidence <job name>
//     Writes ci-reuse-evidence/<job>.json, the proof this job really ran
//     and on which inputs. Put as the LAST line of the job's `script:`,
//     never in `after_script` (which runs whatever the job did) -- a job
//     that gated itself out or failed partway through leaves no
//     evidence, and "no evidence" must read as "did not really run",
//     not as "ran and passed". Never fails the job.
//
// Reads, all via environment:
//   CI_PIPELINE_SOURCE, CI_JOB_NAME, CI_API_V4_URL, CI_PROJECT_ID,
//   CI_MERGE_REQUEST_IID, CI_PIPELINE_ID, CI_JOB_ID  GitLab's own
//     predefined job variables.
//   CI_REUSE_TOKEN   a project access token the owner creates (`read_api`
//     scope, Reporter role, NOT protected -- an ordinary merge request
//     runs on an unprotected branch and would otherwise never see it).
//     Sent as the PRIVATE-TOKEN header, never printed. Unset simply
//     means every job runs, same as if this file did not exist.
//
// Plain Node, no npm dependencies, the same convention
// scripts/chr-watch/run.js uses and for the same reason: this only runs
// inside a handful of CI jobs, never as part of a build, so there is
// nothing to install for it beyond Node itself.

const { execFileSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

const { candidateJobs, jobInputHash, parseTreeListing } = require('./ci-reuse-inputs');

const EVIDENCE_DIR = 'ci-reuse-evidence';
const MAX_PIPELINES = 10;
const REQUEST_TIMEOUT_MS = 20_000;

function log(message) {
  console.log(`ci-reuse-gate: ${message}`);
}

// A job name as a filename: `gate:scenarios 1/4` -> `gate_scenarios_1_4`.
// Anything that is not a letter, a digit, a dot or a dash becomes `_`, so
// a job name naming a path GitLab would refuse cannot produce one.
function slug(job) {
  return String(job).replace(/[^A-Za-z0-9.-]+/gu, '_');
}

function evidencePath(job) {
  return path.join(EVIDENCE_DIR, `${slug(job)}.json`);
}

function readTreeListing() {
  const text = execFileSync('git', ['ls-tree', '-r', '-z', 'HEAD'], {
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
  });
  return parseTreeListing(text);
}

function computeHash(job) {
  return jobInputHash(job, readTreeListing());
}

// Everything a merge-request pipeline needs in order to even attempt a
// lookup. Pure and synchronous so the fall-through cases are trivial to
// unit test without touching git or the network.
function lookupPrerequisites(env) {
  if (env.CI_PIPELINE_SOURCE !== 'merge_request_event') {
    return { ok: false, reason: 'not a merge-request pipeline: every job runs (#1066)' };
  }
  if (!env.CI_REUSE_TOKEN) {
    return { ok: false, reason: 'CI_REUSE_TOKEN is not set: every job runs' };
  }
  const base = env.CI_API_V4_URL;
  if (!base || !env.CI_PROJECT_ID || !env.CI_MERGE_REQUEST_IID) {
    return { ok: false, reason: 'CI_API_V4_URL, CI_PROJECT_ID or CI_MERGE_REQUEST_IID is unset: every job runs' };
  }
  return { ok: true };
}

async function requestJson(url, { token, fetchImpl }) {
  const response = await fetchImpl(url, {
    headers: { 'PRIVATE-TOKEN': token },
    signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
  });
  if (!response.ok) {
    const error = new Error(`${url} answered ${response.status}`);
    error.status = response.status;
    throw error;
  }
  return response.json();
}

// The successful run of `job` in one pipeline's job list, or null. A
// retried job can appear more than once; the highest id is the run that
// stands.
function newestSuccess(jobs, job) {
  let found = null;
  for (const entry of Array.isArray(jobs) ? jobs : []) {
    if (entry?.name !== job || entry?.status !== 'success') continue;
    if (!found || Number(entry.id) > Number(found.id)) found = entry;
  }
  return found;
}

// Which earlier pipeline of this merge request (if any) has a successful
// run of `job` whose evidence names the same input hash. Nothing here
// may throw: every fault -- an unreachable API, a 404, a malformed
// evidence file -- is logged and read as "no match", which reruns the
// job. `fetchImpl` is injectable so a unit test can hand this a fake
// fetch and never touch a network.
async function findReusableRun({ base, token, projectId, mergeRequestIid, pipelineId, job, hash, fetchImpl, log: logFn }) {
  const options = { token, fetchImpl };

  let pipelines;
  try {
    pipelines = await requestJson(
      `${base}/projects/${encodeURIComponent(projectId)}/merge_requests/${encodeURIComponent(mergeRequestIid)}/pipelines`,
      options,
    );
  } catch (error) {
    logFn(`this merge request's earlier pipelines could not be listed (${error.message}): running`);
    return null;
  }

  const earlier = (Array.isArray(pipelines) ? pipelines : [])
    .filter((pipeline) => Number(pipeline?.id) !== Number(pipelineId))
    .sort((left, right) => Number(right.id) - Number(left.id))
    .slice(0, MAX_PIPELINES);

  if (earlier.length === 0) {
    logFn('this merge request has no earlier pipeline: running');
    return null;
  }

  for (const pipeline of earlier) {
    let jobs;
    try {
      jobs = await requestJson(
        `${base}/projects/${encodeURIComponent(projectId)}/pipelines/${pipeline.id}/jobs?per_page=100`,
        options,
      );
    } catch (error) {
      logFn(`pipeline ${pipeline.id}: its jobs could not be listed (${error.message})`);
      continue;
    }
    const candidate = newestSuccess(jobs, job);
    if (!candidate) continue;

    let evidence;
    try {
      evidence = await requestJson(
        `${base}/projects/${encodeURIComponent(projectId)}/jobs/${candidate.id}/artifacts/${evidencePath(job)}`,
        options,
      );
    } catch (error) {
      // A 404 is the ordinary answer for a job that stood on an even
      // earlier run and so wrote no evidence of its own, or one whose
      // artefact has since expired -- not worth logging as a failure.
      if (error.status !== 404) logFn(`job ${candidate.id} in pipeline ${pipeline.id}: evidence unreadable (${error.message})`);
      continue;
    }
    if (!evidence || evidence.inputs !== hash) {
      logFn(`job ${candidate.id} in pipeline ${pipeline.id} ran on different inputs`);
      continue;
    }
    return { jobId: Number(candidate.id), pipelineId: Number(pipeline.id) };
  }

  return null;
}

async function gate(job, env, fetchImpl) {
  if (!candidateJobs().includes(job)) {
    log(`${job} is not in ci-reuse-inputs.js's JOB_INPUTS: running`);
    return 1;
  }

  const prereq = lookupPrerequisites(env);
  if (!prereq.ok) {
    log(`${job}: ${prereq.reason}`);
    return 1;
  }

  let hash;
  try {
    hash = computeHash(job);
  } catch (error) {
    log(`${job}: could not compute the input hash (${error.message}): running`);
    return 1;
  }

  const match = await findReusableRun({
    base: env.CI_API_V4_URL.replace(/\/+$/u, ''),
    token: env.CI_REUSE_TOKEN,
    projectId: env.CI_PROJECT_ID,
    mergeRequestIid: env.CI_MERGE_REQUEST_IID,
    pipelineId: env.CI_PIPELINE_ID,
    job,
    hash,
    fetchImpl,
    log,
  });

  if (!match) {
    log(`${job}: no earlier run to stand on; running.`);
    return 1;
  }
  log(`${job}: standing on job ${match.jobId} from pipeline ${match.pipelineId} (inputs ${hash} unchanged).`);
  return 0;
}

function writeEvidence(job, env) {
  if (!candidateJobs().includes(job)) {
    log(`${job} is not in ci-reuse-inputs.js's JOB_INPUTS: no evidence to record`);
    return;
  }
  let hash;
  try {
    hash = computeHash(job);
  } catch (error) {
    log(`${job}: could not compute the input hash (${error.message}): no evidence recorded, so the next pipeline reruns it`);
    return;
  }
  fs.mkdirSync(EVIDENCE_DIR, { recursive: true });
  const record = {
    job,
    inputs: hash,
    pipeline: Number(env.CI_PIPELINE_ID) || null,
    job_id: Number(env.CI_JOB_ID) || null,
  };
  fs.writeFileSync(evidencePath(job), `${JSON.stringify(record, null, 2)}\n`);
  log(`recorded ${evidencePath(job)}: inputs ${hash}`);
}

async function main(argv, env, fetchImpl) {
  const [command, job] = argv;
  if (command === 'gate' && job) return gate(job, env, fetchImpl);
  if (command === 'evidence' && job) {
    writeEvidence(job, env);
    return 0;
  }
  if (command === 'hash' && job) {
    console.log(computeHash(job));
    return 0;
  }
  console.error('usage: ci-reuse-gate.js gate|evidence|hash <job name>');
  return 2;
}

if (require.main === module) {
  main(process.argv.slice(2), process.env, fetch)
    .then((code) => {
      process.exitCode = code;
    })
    .catch((error) => {
      // Never the reason a candidate job fails: a thrown error here means
      // "run for real", same as any other fault this file catches.
      log(`unexpected error (${error.stack || error.message}): running`);
      process.exitCode = 1;
    });
}

module.exports = {
  slug,
  evidencePath,
  lookupPrerequisites,
  newestSuccess,
  findReusableRun,
  gate,
  writeEvidence,
  main,
};
