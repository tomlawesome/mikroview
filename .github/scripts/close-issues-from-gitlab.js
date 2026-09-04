'use strict';

// Close the GitHub issues a merge into GitLab `dev` resolved. Run by the
// sync:close-github-issues job in .gitlab-ci.yml after the mirror push, so
// the issue closes only once GitHub actually has the commit.
//
// Successor to close-issues-on-dev.yml (removed in #935): that workflow
// fired on a GitHub pull request merging into `dev`, which no longer
// happens now that merges are GitLab merge requests. The matching rules
// are unchanged and shared -- close-issues-matcher.js, with the #503
// corrections for negation and quoted keywords -- and the reason it exists
// is unchanged too: GitHub itself closes an issue only on a merge into the
// default branch, and `dev` is not it.
//
// What text is read: the title and description of every merged merge
// request whose merge commit is this pipeline's commit. GitLab's default
// merge commit carries only the MR title, so reading the commit would miss
// every `Closes #N` written where people actually write it. A commit with
// no merge request behind it (a direct push, such as a reconciliation
// merge) falls back to the commit message.
//
// Environment, all supplied by GitLab CI except the two tokens:
//   CI_API_V4_URL, CI_PROJECT_ID, CI_COMMIT_SHA, CI_COMMIT_MESSAGE
//   GITLAB_MR_TOKEN      read access to this project's merge requests
//   GITHUB_ISSUES_TOKEN  fine-grained PAT: this repository, issues read/write
//   GITHUB_REPO          optional, default tomlawesome/mikroview

const path = require('node:path');
const { extractClosedIssueNumbers } = require(path.join(__dirname, 'close-issues-matcher.js'));

/**
 * The text the closing keywords are read from: every MR merged as this
 * commit, or the commit message when there is none. Pure, for the tests.
 */
function closingText({ mergeRequests, commitSha, commitMessage }) {
  const merged = (mergeRequests || []).filter(
    (mr) => mr.state === 'merged'
      && (mr.merge_commit_sha === commitSha || mr.squash_commit_sha === commitSha),
  );
  if (merged.length === 0) return { source: 'commit', text: commitMessage || '' };
  return {
    source: merged.map((mr) => `!${mr.iid}`).join(' '),
    text: merged.map((mr) => `${mr.title}\n\n${mr.description || ''}`).join('\n\n'),
  };
}

async function readJson(url, init) {
  const res = await fetch(url, init);
  if (!res.ok) {
    throw new Error(`${init && init.method ? init.method : 'GET'} ${url} -> ${res.status} ${await res.text()}`);
  }
  return res.json();
}

async function main() {
  const need = (name) => {
    const v = process.env[name];
    if (!v) throw new Error(`${name} is not set`);
    return v;
  };
  const apiUrl = need('CI_API_V4_URL');
  const projectId = need('CI_PROJECT_ID');
  const commitSha = need('CI_COMMIT_SHA');
  const gitlabToken = need('GITLAB_MR_TOKEN');
  const githubToken = need('GITHUB_ISSUES_TOKEN');
  const repo = process.env.GITHUB_REPO || 'tomlawesome/mikroview';

  const mergeRequests = await readJson(
    `${apiUrl}/projects/${projectId}/repository/commits/${commitSha}/merge_requests`,
    { headers: { 'PRIVATE-TOKEN': gitlabToken } },
  );
  const { source, text } = closingText({
    mergeRequests, commitSha, commitMessage: process.env.CI_COMMIT_MESSAGE,
  });
  const numbers = extractClosedIssueNumbers(text);
  console.log(`Read ${source}: ${numbers.size === 0 ? 'no closing keywords -- nothing to do.' : `closes ${[...numbers].map((n) => `#${n}`).join(', ')}`}`);

  const gh = (p, init = {}) => readJson(`https://api.github.com/repos/${repo}/issues/${p}`, {
    ...init,
    headers: {
      Authorization: `Bearer ${githubToken}`,
      Accept: 'application/vnd.github+json',
      'X-GitHub-Api-Version': '2022-11-28',
      'Content-Type': 'application/json',
      ...(init.headers || {}),
    },
  });

  let failures = 0;
  for (const n of numbers) {
    try {
      const issue = await gh(n);
      if (issue.pull_request) { console.log(`#${n} is a pull request -- skipping`); continue; }
      if (issue.state === 'closed') { console.log(`#${n} already closed -- skipping`); continue; }
      await gh(`${n}/comments`, {
        method: 'POST',
        body: JSON.stringify({
          body: `Closed by ${source}, merged to \`dev\` on GitLab.\n\n`
            + 'Merged is not released — promotion to `preview` and then `main` '
            + 'is a separate, deliberate step.',
        }),
      });
      await gh(n, { method: 'PATCH', body: JSON.stringify({ state: 'closed', state_reason: 'completed' }) });
      console.log(`#${n}: closed`);
    } catch (e) {
      failures += 1;
      console.error(`#${n}: ${e.message}`);
    }
  }
  if (failures > 0) throw new Error(`${failures} issue(s) could not be closed`);
}

module.exports = { closingText };

if (require.main === module) {
  main().catch((e) => { console.error(e.message); process.exit(1); });
}
