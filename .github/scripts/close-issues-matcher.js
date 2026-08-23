'use strict';

// Matching logic for close-issues-on-dev.yml, pulled out of the workflow so
// it can be unit tested without a live GitHub Actions run. See
// close-issues-matcher.test.js and fixtures/ for the real PR bodies that
// exposed the two defects this module fixes (issue #503):
//
//   1. Negation was ignored: "Not fixed: #371" and "Does not close #363"
//      both matched the bare keyword regex, because nothing before the
//      keyword was considered.
//   2. Markdown code context was ignored: the regex ran over raw text, so a
//      keyword quoted inside a code span (e.g. `` `Closes #442` `` used to
//      explain that the trailer had been removed) counted as a real one.
//      GitHub's own closing-keyword parser skips code spans; this module
//      now does too.

// Negation words/contractions that, immediately before a closing keyword,
// mean the sentence is saying the opposite ("not fixed", "doesn't close").
// Keep this list to common, unambiguous negators -- the goal is catching
// the shape GitHub's own prose does, not building a general negation parser.
const NEGATORS = [
  'not', 'never', 'no',
  "doesn't", "isn't", "won't", "can't", 'cannot',
  "didn't", "wasn't", "weren't", "aren't",
  "hasn't", "haven't", "wouldn't", "shouldn't",
];

const NEGATION_LOOKBEHIND = `(?<!\\b(?:${NEGATORS.join('|')})\\s)`;

// Same keyword set as GitHub's own closing-keyword list, applied only after
// negation and markdown code have been stripped out of consideration.
const CLOSING_KEYWORD_PATTERN = new RegExp(
  `${NEGATION_LOOKBEHIND}\\b(close[sd]?|fix(e[sd])?|resolve[sd]?)\\s*:?\\s+#(\\d+)`,
  'gi',
);

/**
 * Strip markdown code so keywords quoted for discussion (not meant as
 * closing trailers) never reach the matcher. Fenced blocks go first so a
 * fence's own backticks don't get parsed as inline spans afterwards.
 */
function stripMarkdownCode(text) {
  let out = text.replace(/```[\s\S]*?```/g, '');
  out = out.replace(/`[^`\n]*`/g, '');
  return out;
}

/**
 * Return the set of issue numbers a PR title+body legitimately closes.
 */
function extractClosedIssueNumbers(text) {
  const cleaned = stripMarkdownCode(text);
  const numbers = new Set();
  for (const match of cleaned.matchAll(CLOSING_KEYWORD_PATTERN)) {
    numbers.add(Number(match[3]));
  }
  return numbers;
}

// The original, unfixed pattern from close-issues-on-dev.yml before #503,
// kept here only so the test suite can demonstrate it failing against the
// real PR bodies that broke it. Not used by the workflow.
const LEGACY_PATTERN = /\b(close[sd]?|fix(e[sd])?|resolve[sd]?)\s*:?\s+#(\d+)/gi;

function extractClosedIssueNumbersLegacy(text) {
  const numbers = new Set();
  for (const match of text.matchAll(LEGACY_PATTERN)) {
    numbers.add(Number(match[3]));
  }
  return numbers;
}

module.exports = {
  extractClosedIssueNumbers,
  extractClosedIssueNumbersLegacy,
  stripMarkdownCode,
};
