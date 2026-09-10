'use strict';

// Decision logic for scripts/chr-watch/run.js (#929, moved to GitLab by
// #943), pulled out of the runner because a scheduled pipeline cannot
// dry-run its own trigger: this module is the only place these rules can
// be exercised before they go live, in chr-watch.test.js.
//
// The GitLab CHR job writes chr/last-run.json to the chr-reports branch on
// every run, pass or fail (#929's issue body has the schema). This module
// takes that file's raw text (or null if the branch/file doesn't exist yet)
// and the current time, and decides which of four states applies. It does
// no network I/O and no filesystem I/O -- run.js's job is to fetch the
// file and hand this module the bytes.

const EXPECTED_SCHEMA = 1;

// The CHR exercise runs weekly. Nine days is a week plus two days of
// slack, so a run that lands a little late (runner contention, a retried
// download -- see #929) doesn't get misread as a dead pipeline.
const STALE_AFTER_DAYS = 9;
const STALE_AFTER_MS = STALE_AFTER_DAYS * 24 * 60 * 60 * 1000;

/**
 * Parse and validate the raw report text. Returns the report with a real
 * Date attached, or null for anything not usable: absent, unparsable,
 * wrong schema, or missing/malformed fields. All of those collapse to the
 * same "missing" state -- the watcher can't tell absent from broken, and
 * shouldn't pretend it can.
 */
function parseReport(raw) {
  if (raw === null || raw === undefined || raw === '') return null;

  let report;
  try {
    report = typeof raw === 'string' ? JSON.parse(raw) : raw;
  } catch (e) {
    return null;
  }

  if (!report || typeof report !== 'object') return null;
  if (report.schema !== EXPECTED_SCHEMA) return null;
  if (report.result !== 'pass' && report.result !== 'fail') return null;
  if (typeof report.finished_at !== 'string') return null;

  const finishedAt = new Date(report.finished_at);
  if (Number.isNaN(finishedAt.getTime())) return null;

  return { ...report, finishedAt };
}

function formatFields(report) {
  const lines = [
    `- RouterOS version: ${report.routeros_version || '(not reported)'}`,
    `- Commit: ${report.commit || '(not reported)'}`,
    `- Finished: ${report.finished_at}`,
  ];
  if (report.summary) lines.push(`- Summary: ${report.summary}`);
  if (report.pipeline_url) lines.push(`- Pipeline: ${report.pipeline_url}`);
  if (report.job_url) lines.push(`- Job: ${report.job_url}`);
  return lines.join('\n');
}

function okBody(report) {
  return (
    `The latest CHR exercise run passed.\n\n${formatFields(report)}\n\n` +
    `Closing -- the passing run above is the evidence.`
  );
}

function failingBody(report) {
  return (
    `The latest CHR exercise run failed.\n\n${formatFields(report)}\n\n` +
    `This comment is added automatically on every failing run until the ` +
    `exercise passes again; see #929.`
  );
}

function staleBody(report, now) {
  const ageDays = Math.floor(
    (now.getTime() - report.finishedAt.getTime()) / (24 * 60 * 60 * 1000),
  );
  return (
    `The last CHR report is ${ageDays} days old -- more than the ` +
    `${STALE_AFTER_DAYS}-day staleness window, so this may mean the ` +
    `weekly job stopped running rather than that it is passing.\n\n` +
    `Last known report:\n${formatFields(report)}\n\n` +
    `See #929 for how this file is meant to arrive.`
  );
}

function missingBody() {
  return (
    `No usable CHR report was found at \`chr/last-run.json\` on the ` +
    `\`chr-reports\` branch -- the branch or file may not exist yet, or ` +
    `its contents didn't parse as the expected report shape (schema ${EXPECTED_SCHEMA}).\n\n` +
    `See #929 for how this file is meant to arrive.`
  );
}

/**
 * Decide what scripts/chr-watch/run.js should do.
 *
 * @param {string|object|null|undefined} raw - the raw text of
 *   chr/last-run.json (or an already-parsed object; either is accepted so
 *   tests can use either shape), or null/undefined if it could not be
 *   fetched at all.
 * @param {Date} now - the current time, injected so tests can pick exact
 *   boundaries instead of racing the clock.
 * @returns {{state: 'ok'|'failing'|'stale'|'missing', title: string, body: string}}
 */
function decideChrWatch(raw, now = new Date()) {
  const report = parseReport(raw);

  if (!report) {
    return {
      state: 'missing',
      title: 'CHR watch: no usable report',
      body: missingBody(),
    };
  }

  const ageMs = now.getTime() - report.finishedAt.getTime();
  if (ageMs > STALE_AFTER_MS) {
    return {
      state: 'stale',
      title: 'CHR watch: last report is stale',
      body: staleBody(report, now),
    };
  }

  if (report.result === 'fail') {
    return {
      state: 'failing',
      title: 'CHR watch: latest run failed',
      body: failingBody(report),
    };
  }

  return {
    state: 'ok',
    title: 'CHR watch: passing',
    body: okBody(report),
  };
}

module.exports = {
  decideChrWatch,
  EXPECTED_SCHEMA,
  STALE_AFTER_DAYS,
  STALE_AFTER_MS,
};
