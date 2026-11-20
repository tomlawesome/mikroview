# Release record template

Copy this file to `docs/releases/vX.Y.Z.md` at step 4 of
[docs/releasing.md](../releasing.md), replace every `<...>`, and commit it on
the `release/vX.Y.Z` branch with
`git commit --trailer "Signed-off-by: <your name>"` (a name only, no email;
`git commit -s` would add one). See "The signed release record" in the guide
for what each line must say, and keep one `step N:` line per numbered step of
"What you do, in short", in the guide's order. A step says one of:

- `done: <link, commit or job id>`
- `not-needed: <the owner decision that says so>`
- `after-merge` (the step comes after this promotion)

An empty value or a `<...>` placeholder fails the check.

---

version: vX.Y.Z
guide-blob: <git rev-parse HEAD:docs/releasing.md>
guide-commit: <git log -1 --format=%H -- docs/releasing.md>
promote-issue: <#N>

step 1: done: <the promote issue comment checking the milestone>
step 2: done: <the merge request that bumped VERSION and refreshed the surfaces>
step 3: done: <the audit record and the closed review issue>
step 4: done: <the commit that cut the changelog>
step 5: after-merge
step 6: after-merge
step 7: after-merge
step 8: after-merge
step 9: after-merge
step 10: after-merge
step 11: after-merge
step 12: after-merge

Signed-off-by: <your name, as git config user.name>
