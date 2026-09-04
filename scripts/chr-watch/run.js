#!/usr/bin/env node
'use strict';

// I/O half of the CHR watch (#929, moved to GitLab by #943). The decision
// logic (chr-watch.js, tested in chr-watch.test.js) is pure; this file does
// the network calls a GitLab scheduled pipeline needs to act on that
// decision, mirroring exactly what the old GitHub Actions workflow
// (chr-watch.yml, now deleted) did inline in its script step.
//
// Reads, all via environment -- GitLab's own predefined job variables plus
// one CI/CD variable the owner creates:
//   CI_API_V4_URL     GitLab's own predefined job variable.
//   CI_PROJECT_ID     GitLab's own predefined job variable.
//   GITLAB_MR_TOKEN   the same project access token (`api` +
//                     `write_repository`) scripts/routeros-chr-open-mr.sh
//                     and scripts/chr-report.sh already use, sent as the
//                     PRIVATE-TOKEN header.
//
// Plain Node 22, no npm dependencies: this only ever runs inside the
// chr-watch:run CI job (node:22-alpine), never as part of the frontend
// build, so there is nothing to install for it.
//
// Exits non-zero for every state except 'ok', so the scheduled pipeline
// itself shows red -- the same signal core.setFailed gave on GitHub.

const LABEL = 'chr-watch';
const REPORT_PATH = 'chr/last-run.json';
const REPORT_REF = 'chr-reports';

const { decideChrWatch } = require('./chr-watch');

function apiBase() {
  const url = process.env.CI_API_V4_URL;
  const projectId = process.env.CI_PROJECT_ID;
  if (!url) throw new Error('CI_API_V4_URL is not set -- this must run inside a GitLab CI job');
  if (!projectId) throw new Error('CI_PROJECT_ID is not set -- this must run inside a GitLab CI job');
  return `${url}/projects/${encodeURIComponent(projectId)}`;
}

function authHeaders() {
  const token = process.env.GITLAB_MR_TOKEN;
  if (!token) throw new Error('GITLAB_MR_TOKEN is not set -- cannot talk to the GitLab API');
  return { 'PRIVATE-TOKEN': token };
}

// Fetches chr/last-run.json from the chr-reports branch. Returns the raw
// text, or null if the branch or file does not exist yet -- the same
// "missing" case decideChrWatch already has a name for.
async function fetchReport() {
  const path = encodeURIComponent(REPORT_PATH);
  const res = await fetch(
    `${apiBase()}/repository/files/${path}/raw?ref=${encodeURIComponent(REPORT_REF)}`,
    { headers: authHeaders() },
  );
  if (res.status === 404) return null;
  if (!res.ok) {
    throw new Error(`fetching ${REPORT_PATH}@${REPORT_REF} failed: ${res.status} ${await res.text()}`);
  }
  return res.text();
}

// Never more than one open chr-watch issue, by construction of planActions
// below -- a new one is only opened when none is already open.
async function listOpenIssues() {
  const res = await fetch(
    `${apiBase()}/issues?labels=${encodeURIComponent(LABEL)}&state=opened&per_page=100`,
    { headers: authHeaders() },
  );
  if (!res.ok) {
    throw new Error(`listing open ${LABEL} issues failed: ${res.status} ${await res.text()}`);
  }
  return res.json();
}

async function createIssue(title, description) {
  const res = await fetch(`${apiBase()}/issues`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, description, labels: LABEL }),
  });
  if (!res.ok) {
    throw new Error(`creating issue failed: ${res.status} ${await res.text()}`);
  }
  return res.json();
}

async function commentOnIssue(iid, body) {
  const res = await fetch(`${apiBase()}/issues/${iid}/notes`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ body }),
  });
  if (!res.ok) {
    throw new Error(`commenting on issue ${iid} failed: ${res.status} ${await res.text()}`);
  }
  return res.json();
}

async function closeIssue(iid, body) {
  await commentOnIssue(iid, body);
  const res = await fetch(`${apiBase()}/issues/${iid}?state_event=close`, {
    method: 'PUT',
    headers: authHeaders(),
  });
  if (!res.ok) {
    throw new Error(`closing issue ${iid} failed: ${res.status} ${await res.text()}`);
  }
  return res.json();
}

/**
 * Pure decision -> action mapping, unit-tested in run.test.js with no
 * network involved. Mirrors chr-watch.yml's old rules exactly:
 *
 *   - 'ok' with an open issue: comment (the passing report is the
 *     evidence) and close it.
 *   - 'ok' with no open issue: nothing to do.
 *   - anything else (failing, stale, missing) with an open issue: comment
 *     on it, so several bad runs in a row read as one issue with several
 *     comments rather than a new issue each time.
 *   - anything else with no open issue: open one.
 *
 * @param {{state: string, title: string, body: string}} decision
 * @param {Array<{iid: number}>} openIssues
 * @returns {{actions: Array<object>, exitNonZero: boolean}}
 */
function planActions(decision, openIssues) {
  const openIssue = openIssues[0]; // Never more than one, by construction.
  const actions = [];

  if (decision.state === 'ok') {
    if (openIssue) {
      actions.push({ type: 'close', iid: openIssue.iid, body: decision.body });
    }
    return { actions, exitNonZero: false };
  }

  if (openIssue) {
    actions.push({ type: 'comment', iid: openIssue.iid, body: decision.body });
  } else {
    actions.push({ type: 'create', title: decision.title, description: decision.body });
  }
  return { actions, exitNonZero: true };
}

async function runAction(action) {
  switch (action.type) {
    case 'close': {
      const issue = await closeIssue(action.iid, action.body);
      console.log(`Closed #${issue.iid}: latest run passed.`);
      return;
    }
    case 'comment': {
      await commentOnIssue(action.iid, action.body);
      console.log(`Commented on already-open #${action.iid}.`);
      return;
    }
    case 'create': {
      const issue = await createIssue(action.title, action.description);
      console.log(`Opened #${issue.iid}.`);
      return;
    }
    default:
      throw new Error(`unknown action type: ${action.type}`);
  }
}

async function main() {
  const raw = await fetchReport();
  const decision = decideChrWatch(raw, new Date());
  console.log(`CHR watch decision: ${decision.state}`);

  const openIssues = await listOpenIssues();
  const { actions, exitNonZero } = planActions(decision, openIssues);

  if (actions.length === 0) {
    console.log('Latest run passed and no issue is open -- nothing to do.');
  } else {
    for (const action of actions) {
      await runAction(action);
    }
  }

  if (exitNonZero) {
    console.log(`CHR watch: ${decision.state}`);
    process.exitCode = 1;
  }
}

if (require.main === module) {
  main().catch((err) => {
    console.error(err.stack || String(err));
    process.exitCode = 1;
  });
}

module.exports = { planActions };
